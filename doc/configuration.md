# Configuration

## Config File

The bridge uses a YAML config file for its network-specific settings. The bridgev2 framework handles the overall bridge configuration (database, homeserver connection, appservice registration, logging, etc.) -- the settings below are kChat-specific.

The example config is embedded in the binary at build time from `pkg/connector/example-config.yaml`.

### Fields

```yaml
# URL of the kChat server to connect to.
# This is the default server URL used for puppet connections and auto-login.
# Individual puppets can override this with their own _URL env var.
# Can also be set via the login flow on a per-user basis.
server_url: "https://kchat.example.com"

# Displayname template for kChat ghost users in Matrix.
# Uses Go text/template syntax.
# Available variables: .Username, .Nickname, .FirstName, .LastName
displayname_template: "{{if .Nickname}}{{.Nickname}}{{else}}{{.Username}}{{end}} (MM)"

# Username prefix for echo prevention. Any kChat username starting with
# this prefix is treated as a bridge-managed bot and its posts will not be
# relayed back to Matrix. Leave empty to disable prefix-based filtering.
bot_prefix: ""

# Listen address for the admin HTTP API (serves /api/reload-puppets).
# Set to empty string to disable the admin API.
# Can be overridden via BRIDGE_API_ADDR environment variable.
admin_api_addr: ":29320"

# Enable message backfill to populate channel history on first sync.
backfill_enabled: false

# Maximum number of messages to backfill per channel.
backfill_max_count: 100

# Typing indicator timeout in seconds.
typing_timeout: 5
```

### Display Name Template

The `displayname_template` field uses Go's `text/template` syntax. It controls how kChat users appear in Matrix rooms.

Available variables (from `DisplaynameParams`):

| Variable | Source | Example |
|----------|--------|---------|
| `.Username` | kChat username | `john.doe` |
| `.Nickname` | kChat nickname | `Johnny` |
| `.FirstName` | kChat first name | `John` |
| `.LastName` | kChat last name | `Doe` |

Template examples:

```yaml
# Nickname with username fallback (default)
displayname_template: "{{if .Nickname}}{{.Nickname}}{{else}}{{.Username}}{{end}} (MM)"

# Full name with username fallback
displayname_template: "{{if .FirstName}}{{.FirstName}} {{.LastName}}{{else}}{{.Username}}{{end}}"

# Always use username
displayname_template: "{{.Username}}"

# Username only, no suffix
displayname_template: "{{.Username}}"
```

If the template fails to render (e.g., syntax error), the raw username is used as a fallback.

## Environment Variables

### Auto-Login

These enable the bridge to connect to kChat on first startup without manual bot interaction.

| Variable | Required | Description |
|----------|----------|-------------|
| `KCHAT_AUTO_SERVER_URL` | Yes | kChat server URL for auto-login |
| `KCHAT_AUTO_TOKEN` | Yes | kChat access token for auto-login |
| `KCHAT_AUTO_OWNER_MXID` | Yes | Matrix user ID that "owns" the auto-login |

All three must be set for auto-login to activate. If any existing logins are found in the bridge database, auto-login is skipped.

Auto-login also triggers `autoSetRelay`, which sets the logged-in user as the relay for all bridged rooms. This is required for the puppet system to function -- without relay, the bridgev2 framework rejects messages from non-logged-in users.

### Puppet Configuration

Each puppet requires a pair of environment variables (and one optional):

| Pattern | Required | Description |
|---------|----------|-------------|
| `KCHAT_PUPPET_{SLUG}_MXID` | Yes | Matrix user ID for this puppet |
| `KCHAT_PUPPET_{SLUG}_TOKEN` | Yes | kChat bot access token |
| `KCHAT_PUPPET_{SLUG}_URL` | No | Override server URL (defaults to `server_url`) |

`{SLUG}` is an arbitrary identifier. Convention: uppercase, underscores instead of hyphens.

The bridge scans all environment variables for `KCHAT_PUPPET_*_MXID` patterns at startup.

### Relay Bot

| Variable | Required | Description |
|----------|----------|-------------|
| `KCHAT_PUPPET_RELAY_TOKEN` | No | Token for the relay bot (if managed as a puppet) |

### Admin API

| Variable | Required | Description |
|----------|----------|-------------|
| `BRIDGE_API_ADDR` | No | Override listen address for admin API (default `:29320`) |

The admin API address resolution order:
1. `admin_api_addr` in config file
2. `BRIDGE_API_ADDR` environment variable
3. Default: `:29320`

## Admin API Endpoints

### `POST /api/reload-puppets`

Reloads the puppet map. Two modes:

**Mode 1: Reload from environment variables**

```bash
curl -X POST http://localhost:29320/api/reload-puppets
```

Re-scans `KCHAT_PUPPET_*` environment variables and updates the puppet map.

**Mode 2: Reload from JSON body**

```bash
curl -X POST http://localhost:29320/api/reload-puppets \
  -H 'Content-Type: application/json' \
  -d '[
    {"slug": "ALICE", "mxid": "@alice:example.com", "token": "token-alice"},
    {"slug": "BOB", "mxid": "@bob:example.com", "token": "token-bob"}
  ]'
```

The JSON array represents the **desired state**. Puppets not in the list are removed. Puppets with unchanged tokens are kept without re-authentication.

**Response** (both modes):

```json
{
  "added": 2,
  "removed": 1,
  "total": 5
}
```

| Field | Description |
|-------|-------------|
| `added` | Number of new puppets loaded |
| `removed` | Number of puppets removed |
| `total` | Total puppets now loaded |

### `POST /api/double-puppet`

Registers a double puppet login for a specific user. This is called automatically by the bridge during startup for puppets and auto-login users, but can also be triggered manually.

The bridge uses the AS token from `double_puppet.secrets` to impersonate users via the appservice API. This endpoint is internal to the bridge framework.

## Double Puppet Configuration

The `double_puppet` section in the bridge config controls kChat → Matrix identity mapping:

```yaml
double_puppet:
  servers:
    example.com: https://matrix.example.com
  secrets:
    example.com: "as_token:your_bridge_as_token"
  allow_discovery: false
```

| Field | Description |
|-------|-------------|
| `servers` | Map of homeserver domain → URL. Required for the bridge to reach the homeserver. |
| `secrets` | Map of domain → AS token (prefixed with `as_token:`). The bridge uses this to impersonate users. |
| `allow_discovery` | Whether to allow automatic discovery of homeserver URLs. |

The AS token in `secrets` must match the `as_token` in the bridge's appservice registration. The bridge's registration must also include non-exclusive namespaces for all users that need double puppeting.

### Appservice Registration for Double Puppeting

```yaml
namespaces:
  users:
    - exclusive: true
      regex: '@kchat_.+:example\.com'
    # Non-exclusive: bridge can impersonate for double puppeting
    - exclusive: false
      regex: '@admin:example\.com'
    - exclusive: false
      regex: '@agent-.+:example\.com'
```

## Login Flows

The bridge supports two interactive login methods via the Matrix bot interface:

### Token Login (`token`)

1. User provides kChat server URL
2. User provides personal access token
3. Bridge verifies token and creates login

### Password Login (`password`)

1. User provides kChat server URL
2. User provides username and password
3. Bridge authenticates and creates login using the session token

Both flows create a `UserLogin` with metadata containing `server_url`, `token`, `user_id`, and `team_id`. After login, the bridge connects the WebSocket and begins syncing channels.

## Network Ports

| Port | Service | Description |
|------|---------|-------------|
| 29319 | Appservice | Matrix homeserver pushes events here (bridge framework default) |
| 29320 | Admin API | Puppet hot-reload endpoint |
