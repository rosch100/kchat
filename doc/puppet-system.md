# Puppet System

## Problem

In a standard Matrix-kChat bridge using relay mode, all messages from Matrix users appear under a single "relay bot" account in kChat. The message content might include the original sender's name in the text, but the kChat post metadata (author, avatar, username) all point to the same relay bot.

This is unacceptable for deployments where multiple Matrix users each need a distinct kChat identity -- for example, when separate bot accounts represent different services or team members.

## Solution

The puppet system maps each Matrix user (identified by MXID) to a dedicated kChat bot account. When a Matrix user sends a message, the bridge looks up their MXID in the puppet map and posts the message using that bot's API token instead of the relay bot.

From kChat's perspective, each puppet appears as a separate user with its own username, display name, and avatar.

## Configuration

### Environment Variables

Each puppet requires two environment variables (and one optional):

```
KCHAT_PUPPET_{SLUG}_MXID   = @bot-name:example.com
KCHAT_PUPPET_{SLUG}_TOKEN  = <kchat bot access token>
KCHAT_PUPPET_{SLUG}_URL    = http://kchat:8065  (optional)
```

Where `{SLUG}` is an arbitrary uppercase identifier with underscores. The bridge scans all environment variables matching `KCHAT_PUPPET_*_MXID` at startup.

**Naming convention**: Convert the puppet's identifier to uppercase and replace hyphens with underscores. For example, a puppet for `@my-bot:example.com` might use slug `MY_BOT`.

The `_URL` variable is optional and defaults to the bridge's configured `server_url`. Use it when a puppet needs to connect to a different kChat server.

### Examples

```bash
# A service bot
KCHAT_PUPPET_SERVICE_BOT_MXID=@service-bot:example.com
KCHAT_PUPPET_SERVICE_BOT_TOKEN=abc123token

# An integration
KCHAT_PUPPET_GITHUB_MXID=@github-bridge:example.com
KCHAT_PUPPET_GITHUB_TOKEN=def456token
```

## Resolution Algorithm

When a Matrix message arrives, `resolvePostClient()` in `handlematrix.go` performs a 3-path lookup to determine which kChat client to use for posting:

### Path 1: origSender (relay metadata)

```go
if origSender != nil {
    if puppet, ok := m.connector.Puppets[origSender.UserID]; ok {
        return puppet.Client, puppet.UserID
    }
}
```

The bridgev2 framework sets `origSender` on relayed messages -- messages from Matrix users who are not directly logged into the bridge. This is the primary path for puppet resolution. When a non-logged-in Matrix user sends a message in a bridged room, the framework wraps it as a relay event and populates `origSender` with the original Matrix user's identity.

### Path 2: evt.Sender (direct event sender)

```go
if evt != nil && evt.Sender != "" {
    if puppet, ok := m.connector.Puppets[evt.Sender]; ok {
        return puppet.Client, puppet.UserID
    }
}
```

This covers cases where the Matrix event sender is not wrapped in relay metadata. This can happen with certain event types or when the bridge framework processes events differently. Without this check, directly-sent events from a puppet user would fall through to the relay bot.

### Path 3: Relay fallback

```go
return m.client, m.userID
```

If neither path matches a puppet, the message is posted using the bridge's relay bot account. This is the normal behavior for Matrix users who don't have a puppet configured.

## Hot-Reload

Puppets can be added, removed, or updated without restarting the bridge.

### Via Admin API

```bash
# Reload from environment variables
curl -X POST http://localhost:29320/api/reload-puppets

# Reload from explicit JSON
curl -X POST http://localhost:29320/api/reload-puppets \
  -H 'Content-Type: application/json' \
  -d '[
    {"slug": "BOT_A", "mxid": "@bot-a:example.com", "token": "token-a"},
    {"slug": "BOT_B", "mxid": "@bot-b:example.com", "token": "token-b"}
  ]'
```

Response:

```json
{"added": 2, "removed": 0, "total": 5}
```

When using JSON body, the provided list becomes the **desired state**. Puppets in the current map but absent from the JSON are removed. Puppets with unchanged tokens are kept as-is (no re-authentication).

When the body is empty, the bridge re-scans environment variables.

### Reload Behavior

1. Build desired puppet set from input (JSON body or env vars)
2. Acquire write lock on puppet map
3. Remove puppets not in desired set
4. For each desired puppet:
   - If already loaded with same token: skip (no-op)
   - Otherwise: create new kChat API client, verify token via `GetMe()`, add to map
5. Release lock
6. Log summary: added, removed, total

Failed authentications during reload are logged and skipped -- they do not prevent other puppets from loading.

## WatchNewPortals

The bridgev2 framework requires a relay login to be set on each portal (bridged room) before it will call `HandleMatrixMessage`. Without relay, messages from non-logged-in users are rejected with "not logged in" before the puppet system is reached.

`WatchNewPortals` solves this for rooms created after startup:

1. Runs as a background goroutine with a configurable interval (default 60 seconds)
2. Each tick: fetches all portals with an MXID, checks if relay is set
3. For portals without relay: finds the first available user login and sets it as relay
4. Logs how many portals were updated

This is necessary because:
- The initial `autoSetRelay` only runs 3 times after boot
- New kChat channels can be created at any time
- When a new channel is bridged, the resulting portal needs relay before puppet routing works

## Thread Safety

The puppet map (`KChatConnector.Puppets`) is accessed from multiple goroutines:

- **Read path**: `resolvePostClient` and `IsPuppetUserID` during message handling
- **Write path**: `ReloadPuppetsFromEntries` during hot-reload

Access is protected by `sync.RWMutex` (`puppetMu`). Read operations use `RLock`/`RUnlock`, write operations use `Lock`/`Unlock`. This allows concurrent message handling while serializing reload operations.

Note that the initial `loadPuppets` call during `Start()` does not lock because it runs before any goroutines are spawned.

## Double Puppeting (kChat → Matrix)

The puppet system described above handles Matrix → kChat direction. **Double puppeting** handles the reverse: when a kChat user posts, the corresponding Matrix event appears under their real MXID rather than a ghost (`@kchat_<id>:server`).

### Configuration

Add `double_puppet` to the bridge config:

```yaml
double_puppet:
  servers:
    example.com: https://matrix.example.com
  secrets:
    example.com: "as_token:your_bridge_as_token"
  allow_discovery: false
```

The `secrets` map uses the bridge's own AS token (prefixed with `as_token:`) to impersonate users via the appservice API.

### Appservice Registration

The bridge's appservice registration must include **non-exclusive namespaces** for users that need double puppeting:

```yaml
namespaces:
  users:
    - exclusive: true
      regex: '@kchat_.+:example\.com'
    # Non-exclusive: bridge AS can impersonate these for double puppeting
    - exclusive: false
      regex: '@admin:example\.com'
    - exclusive: false
      regex: '@agent-.+:example\.com'
```

Without these namespace entries, the homeserver will reject the bridge's attempt to send events on behalf of these users.

### How It Works

1. During `loadPuppets()`, the bridge calls `setupUserDoublePuppet()` for each puppet entry, registering a `UserLogin` via the bridgev2 `LoginDoublePuppet()` method.
2. During `autoLogin()`, the bridge calls `setupUserDoublePuppet()` for the auto-login user (with a legacy password-based fallback).
3. The `dpLogins` map tracks `MM user ID → UserLoginID`.
4. When a kChat event arrives, `senderFor()` in `handlekchat.go` checks `dpLogins`. If the MM poster has a DP login, the event is routed through that login's intent, so the Matrix event appears under the real MXID.

### Interaction with Puppet Routing

The two systems are complementary:
- **Puppet routing** (Matrix → MM): ensures messages from `@alice:server` post to MM as `alice-bot`
- **Double puppeting** (MM → Matrix): ensures messages from `alice` in MM appear as `@alice:server` in Matrix

Together, they create a seamless bidirectional identity mapping.

## Scaling

The puppet system has been tested with 30+ concurrent puppets. Memory overhead per puppet is minimal:

- One `*model.Client4` (kChat HTTP client)
- Stored MXID, user ID, and username strings

There is no per-puppet goroutine or WebSocket connection. All puppets share the bridge's single WebSocket connection for receiving events and use the main HTTP client pool for posting. The cost scales linearly with the number of puppets at approximately one small struct per puppet.

## Setup Walkthrough

1. **Create kChat bot accounts** for each Matrix user that needs a puppet identity. Use the kChat admin panel or API to create bot accounts and generate access tokens.

2. **Set environment variables** before starting the bridge:
   ```bash
   export KCHAT_PUPPET_ALICE_MXID="@alice:example.com"
   export KCHAT_PUPPET_ALICE_TOKEN="<alice-bot-token>"
   export KCHAT_PUPPET_BOB_MXID="@bob:example.com"
   export KCHAT_PUPPET_BOB_TOKEN="<bob-bot-token>"
   ```

3. **Start the bridge**. On startup, the bridge scans for `KCHAT_PUPPET_*_MXID` variables, verifies each token, and logs loaded puppets:
   ```
   Loaded puppet client  puppet=ALICE mxid=@alice:example.com mm_username=alice-bot
   Loaded puppet client  puppet=BOB mxid=@bob:example.com mm_username=bob-bot
   ```

4. **Verify puppet routing**. Send a message from `@alice:example.com` in a bridged Matrix room. It should appear in kChat under `alice-bot`, not the relay bot.

5. **Add more puppets at runtime** using the admin API:
   ```bash
   curl -X POST http://localhost:29320/api/reload-puppets \
     -H 'Content-Type: application/json' \
     -d '[{"slug":"CAROL","mxid":"@carol:example.com","token":"<carol-token>"}]'
   ```
   Note: when using JSON reload, include ALL desired puppets in the request, not just new ones.
