# Puppet Identity System — Deployment Guide

How to deploy matrix-kchat with puppet identity routing so that multiple
agents or users post to kChat under their own bot accounts instead of a
single relay user.

> **Quick demo**: See [example/multi-agent-puppeting/](../example/multi-agent-puppeting/)
> for a self-contained Docker Compose demo with 3 AI agents. Run
> `cd example/multi-agent-puppeting && docker compose up --build` and open
> http://localhost:18065 to watch agents post under distinct identities.

---

## Overview

The puppet system maps Matrix user IDs (MXIDs) to kChat bot tokens.
When a bridged message arrives from `@alice:example.com`, the bridge posts it
to kChat using Alice's dedicated bot — preserving her username, display
name, and avatar.

```
Matrix side                       kChat side
─────────────────                 ─────────────────
@alice:example.com  ──puppet──▶   alice-bot (Bot A token)
@bob:example.com    ──puppet──▶   bob-bot   (Bot B token)
@unknown:example.com ──relay──▶   relay-bot (fallback)
```

---

## 1. Prerequisites

| Component | Purpose |
|-----------|---------|
| kChat server | Target chat platform |
| Matrix homeserver (Synapse recommended) | Source chat platform |
| matrix-kchat bridge | Bridges the two |
| Admin access to kChat | To create bots and tokens |
| Admin access to Synapse | To register Matrix users |

---

## 2. Create kChat Bot Accounts

Each puppet needs a kChat bot account with a personal access token.

### Enable bot accounts and tokens

In your kChat System Console (or via environment variables):

```
MM_SERVICESETTINGS_ENABLEBOTACCOUNTCREATION=true
MM_SERVICESETTINGS_ENABLEUSERACCESSTOKENS=true
```

### Create a bot via the API

```bash
MM_URL="http://kchat:8065"
KCHAT_TOKEN="<admin-token>"

# Create the bot
curl -s -X POST "${MM_URL}/api/v4/bots" \
  -H "Authorization: Bearer ${KCHAT_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "alice-bot",
    "display_name": "Alice",
    "description": "Puppet bot for Alice"
  }'
```

### Get the bot's user ID

```bash
BOT_USER_ID=$(curl -s "${MM_URL}/api/v4/users/username/alice-bot" \
  -H "Authorization: Bearer ${KCHAT_TOKEN}" | jq -r '.id')
```

### Generate a personal access token

```bash
BOT_TOKEN=$(curl -s -X POST "${MM_URL}/api/v4/users/${BOT_USER_ID}/tokens" \
  -H "Authorization: Bearer ${KCHAT_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"description": "alice-bot puppet token"}' | jq -r '.token')
```

### Add the bot to a team and channels

```bash
TEAM_ID="<your-team-id>"

# Add to team
curl -s -X POST "${MM_URL}/api/v4/teams/${TEAM_ID}/members" \
  -H "Authorization: Bearer ${KCHAT_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"team_id\": \"${TEAM_ID}\", \"user_id\": \"${BOT_USER_ID}\"}"

# Add to a channel
CHANNEL_ID="<your-channel-id>"
curl -s -X POST "${MM_URL}/api/v4/channels/${CHANNEL_ID}/members" \
  -H "Authorization: Bearer ${KCHAT_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"user_id\": \"${BOT_USER_ID}\"}"
```

> **Important**: Bots must be added to `town-square` (or whatever your default
> channel is) for team membership to take effect.

Repeat for each puppet identity you need.

---

## 3. Register Corresponding Matrix Users

Each puppet bot needs a Matrix user that the bridge's appservice manages. If
you're running Synapse with shared-secret registration:

```bash
SYNAPSE_URL="http://synapse:8008"
SHARED_SECRET="your-shared-secret"

# Get a nonce
NONCE=$(curl -s "${SYNAPSE_URL}/_synapse/admin/v1/register" | jq -r '.nonce')

# Generate HMAC (nonce\0username\0password\0notadmin)
MAC=$(printf '%s\0%s\0%s\0%s' "$NONCE" "alice-bot" "a-password" "notadmin" \
  | openssl dgst -sha1 -hmac "$SHARED_SECRET" | awk '{print $NF}')

# Register
curl -s -X POST "${SYNAPSE_URL}/_synapse/admin/v1/register" \
  -H "Content-Type: application/json" \
  -d "{
    \"nonce\": \"${NONCE}\",
    \"username\": \"alice-bot\",
    \"password\": \"a-password\",
    \"admin\": false,
    \"mac\": \"${MAC}\"
  }"
```

The resulting MXID will be `@alice-bot:example.com` (where `example.com` is
your homeserver's `server_name`).

---

## 4. Configure Puppet Environment Variables

The bridge discovers puppets via environment variables at startup:

```bash
# Pattern:
#   KCHAT_PUPPET_<SLUG>_MXID = Matrix user ID
#   KCHAT_PUPPET_<SLUG>_TOKEN = kChat bot token
#
# SLUG rules: uppercase, hyphens become underscores

export KCHAT_PUPPET_ALICE_MXID="@alice-bot:example.com"
export KCHAT_PUPPET_ALICE_TOKEN="<bot-token-for-alice>"

export KCHAT_PUPPET_BOB_SMITH_MXID="@bob-smith-bot:example.com"
export KCHAT_PUPPET_BOB_SMITH_TOKEN="<bot-token-for-bob>"
```

The bridge calls `loadPuppets()` on startup, which scans all environment
variables matching the `KCHAT_PUPPET_*_MXID` / `*_TOKEN` pattern.

---

## 5. Bridge Configuration

Key sections in `config.yaml`:

### Network settings

```yaml
network:
  server_url: http://kchat:8065
  displayname_template: "{{if .Nickname}}{{.Nickname}}{{else}}{{.Username}}{{end}}"

  # Echo prevention: any MM username starting with this prefix is treated
  # as a bridge-managed bot and its posts won't loop back to Matrix.
  bot_prefix: ""

  # Admin API for hot-reload (port separate from appservice)
  admin_api_addr: ":29320"
```

### Relay configuration

```yaml
bridge:
  relay:
    enabled: true
    admin_only: false
    # CRITICAL: Without message_formats, relay silently drops ALL messages.
    # When using puppets, you may not want the sender name prepended
    # (the puppet bot identity already conveys who sent it).
    message_formats:
      m.text: "{{ .Message }}"
      m.notice: "{{ .Message }}"
      m.emote: "* {{ .Message }}"
      m.file: "sent a file{{ if .Caption }}: {{ .Caption }}{{ end }}"
      m.image: "sent an image{{ if .Caption }}: {{ .Caption }}{{ end }}"
      m.audio: "sent an audio file{{ if .Caption }}: {{ .Caption }}{{ end }}"
      m.video: "sent a video{{ if .Caption }}: {{ .Caption }}{{ end }}"
  permissions:
    "*": relay
    "example.com": user
    "@admin:example.com": admin
```

---

## 6. Deployment Options

### Option A: Docker Compose

```yaml
services:
  matrix-kchat:
    image: ghcr.io/rosch/matrix-kchat:latest
    ports:
      - "29319:29319"   # Appservice API
      - "29320:29320"   # Admin API (reload-puppets)
    volumes:
      - ./config.yaml:/data/config.yaml:ro
    environment:
      KCHAT_AUTO_SERVER_URL: http://kchat:8065
      KCHAT_AUTO_OWNER_MXID: "@admin:example.com"
      KCHAT_AUTO_TOKEN: "<bridge-bot-token>"

      # Puppet mappings
      KCHAT_PUPPET_ALICE_MXID: "@alice-bot:example.com"
      KCHAT_PUPPET_ALICE_TOKEN: "<alice-bot-token>"
      KCHAT_PUPPET_BOB_MXID: "@bob-bot:example.com"
      KCHAT_PUPPET_BOB_TOKEN: "<bob-bot-token>"
```

### Option B: Kubernetes

Split puppet config into a ConfigMap (MXIDs) and a Secret (tokens) for
proper secret management:

**ConfigMap** — puppet MXIDs:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: kchat-puppet-config
data:
  KCHAT_PUPPET_ALICE_MXID: "@alice-bot:example.com"
  KCHAT_PUPPET_BOB_MXID: "@bob-bot:example.com"
```

**Secret** — puppet tokens:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: kchat-puppet-tokens
type: Opaque
stringData:
  KCHAT_PUPPET_ALICE_TOKEN: "<alice-bot-token>"
  KCHAT_PUPPET_BOB_TOKEN: "<bob-bot-token>"
```

**Deployment** — inject both via `envFrom`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: matrix-kchat
spec:
  replicas: 1
  template:
    spec:
      containers:
        - name: matrix-kchat
          image: ghcr.io/rosch/matrix-kchat:latest
          args: ["-c", "/data/config.yaml"]
          ports:
            - containerPort: 29319
              name: appservice
            - containerPort: 29320
              name: admin-api
          env:
            - name: KCHAT_AUTO_SERVER_URL
              value: "http://kchat:8065"
            - name: KCHAT_AUTO_OWNER_MXID
              value: "@admin:example.com"
            - name: KCHAT_AUTO_TOKEN
              valueFrom:
                secretKeyRef:
                  name: kchat-bridge-token
                  key: token
          envFrom:
            - configMapRef:
                name: kchat-puppet-config
                optional: true
            - secretRef:
                name: kchat-puppet-tokens
                optional: true
          readinessProbe:
            tcpSocket:
              port: 29319
            initialDelaySeconds: 5
            periodSeconds: 5
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 250m
              memory: 256Mi
```

> **Gotcha**: The bridge's `/_matrix/app/v1/ping` endpoint only accepts POST,
> so use TCP socket probes instead of HTTP GET probes.

**Service** — expose both ports:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: matrix-kchat
spec:
  ports:
    - port: 29319
      name: appservice
    - port: 29320
      name: admin-api
  selector:
    app: matrix-kchat
```

---

## 7. Automating Puppet Bootstrap at Scale

For deployments with many puppets (10+), use an init job to create all bots
and generate the ConfigMap/Secret automatically.

### Define agents in a config file

```
# agents.conf — one agent per line
# Format: slug:display_name:description
alice:Alice:Product manager
bob:Bob:Engineer
carol:Carol:Designer
```

### Bootstrap script pattern

```bash
#!/usr/bin/env bash
set -uo pipefail

MM_URL="${MM_URL:-http://kchat:8065}"
AGENTS_CONF="${AGENTS_CONF:-/config/agents.conf}"
NAMESPACE="${NAMESPACE:-default}"
DOMAIN="${DOMAIN:-example.com}"

# Accumulate puppet env vars
declare -A PUPPET_MXIDS
declare -A PUPPET_TOKENS

while IFS=: read -r slug display_name description || [ -n "$slug" ]; do
  # Skip comments and blank lines
  [[ "$slug" =~ ^[[:space:]]*# ]] && continue
  [[ -z "$slug" ]] && continue

  slug=$(echo "$slug" | xargs)
  display_name=$(echo "$display_name" | xargs)
  description=$(echo "$description" | xargs)

  echo "Creating bot: ${slug}"

  # Create MM bot (idempotent — ignores "already exists")
  curl -s -X POST "${MM_URL}/api/v4/bots" \
    -H "Authorization: Bearer ${KCHAT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{\"username\":\"${slug}\",\"display_name\":\"${display_name}\",\"description\":\"${description}\"}" \
    >/dev/null 2>&1 || true

  # Get bot user ID
  BOT_USER_ID=$(curl -s "${MM_URL}/api/v4/users/username/${slug}" \
    -H "Authorization: Bearer ${KCHAT_TOKEN}" | jq -r '.id // empty')

  [ -z "$BOT_USER_ID" ] && { echo "  WARN: could not find ${slug}"; continue; }

  # Generate personal access token
  BOT_TOKEN=$(curl -s -X POST "${MM_URL}/api/v4/users/${BOT_USER_ID}/tokens" \
    -H "Authorization: Bearer ${KCHAT_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{\"description\":\"${slug} puppet token\"}" | jq -r '.token // empty')

  [ -z "$BOT_TOKEN" ] && { echo "  WARN: token gen failed for ${slug}"; continue; }

  # Add to team + channels (omitted for brevity — see Section 2)

  # Accumulate for ConfigMap/Secret
  ENV_KEY=$(echo "$slug" | tr '[:lower:]-' '[:upper:]_')
  PUPPET_MXIDS["$ENV_KEY"]="@${slug}:${DOMAIN}"
  PUPPET_TOKENS["$ENV_KEY"]="$BOT_TOKEN"

  echo "  Bot ${slug} ready"
done < "$AGENTS_CONF"

# Build ConfigMap
CM_ARGS=""
for key in "${!PUPPET_MXIDS[@]}"; do
  CM_ARGS="${CM_ARGS} --from-literal=KCHAT_PUPPET_${key}_MXID=${PUPPET_MXIDS[$key]}"
done

SECRET_ARGS=""
for key in "${!PUPPET_TOKENS[@]}"; do
  SECRET_ARGS="${SECRET_ARGS} --from-literal=KCHAT_PUPPET_${key}_TOKEN=${PUPPET_TOKENS[$key]}"
done

eval kubectl create configmap kchat-puppet-config \
  --namespace="$NAMESPACE" $CM_ARGS \
  --dry-run=client -o yaml | kubectl apply -f -

eval kubectl create secret generic kchat-puppet-tokens \
  --namespace="$NAMESPACE" $SECRET_ARGS \
  --dry-run=client -o yaml | kubectl apply -f -

echo "Puppet ConfigMap: ${#PUPPET_MXIDS[@]} entries"
echo "Puppet Secret: ${#PUPPET_TOKENS[@]} entries"
```

Run this as a Kubernetes Job with appropriate RBAC (create/update on
secrets and configmaps).

---

## 8. Hot-Reload: Add Puppets Without Restart

The bridge exposes `POST /api/reload-puppets` on the admin API port
(default 29320).

### Reload from environment variables

If you've updated the ConfigMap/Secret (or env vars), trigger an env reload:

```bash
curl -X POST http://matrix-kchat:29320/api/reload-puppets
```

The bridge re-scans all `KCHAT_PUPPET_*` env vars and reconciles:
new puppets are added, removed ones are cleaned up, unchanged ones are kept.

### Reload with explicit entries

For direct API-driven provisioning (no env var update needed):

```bash
curl -X POST http://matrix-kchat:29320/api/reload-puppets \
  -H "Content-Type: application/json" \
  -d '[
    {"slug": "ALICE", "mxid": "@alice-bot:example.com", "token": "<token>"},
    {"slug": "BOB", "mxid": "@bob-bot:example.com", "token": "<token>"}
  ]'
```

> **Warning**: The JSON body **replaces** the entire puppet set — include all
> puppets you want active, not just the new one.

### Programmatic provisioning

For runtime agent creation (e.g., from a Python service), the flow is:

1. Create MM bot + generate token via kChat API
2. Register Matrix user via Synapse admin API
3. POST to `/api/reload-puppets` (empty body for env-based reload)

```python
import httpx

BRIDGE_API = "http://matrix-kchat:29320"

# After creating the MM bot and updating k8s Secret/ConfigMap...
resp = httpx.post(f"{BRIDGE_API}/api/reload-puppets")
data = resp.json()
print(f"Added: {data['added']}, Removed: {data['removed']}, Total: {data['total']}")
```

---

## 9. Double Puppeting Setup

Double puppeting makes incoming kChat messages appear in Matrix under the real user's MXID instead of a ghost account. This is configured alongside the puppet system.

### Step 1: Configure `double_puppet` in bridge config

```yaml
double_puppet:
  servers:
    example.com: https://matrix.example.com
  secrets:
    example.com: "as_token:your_bridge_as_token"
  allow_discovery: false
```

The `as_token:` prefix tells the bridge to use its appservice token (from `appservice.as_token`) to impersonate users.

### Step 2: Add non-exclusive namespaces to appservice registration

The bridge can only impersonate users that are in its registered namespaces. Add non-exclusive entries for each user or user pattern that needs double puppeting:

```yaml
# In your bridge appservice registration file
namespaces:
  users:
    - exclusive: true
      regex: '@kchat_.+:example\.com'
    # Double puppeting: bridge impersonates these users
    - exclusive: false
      regex: '@admin:example\.com'
    - exclusive: false
      regex: '@agent-.+:example\.com'
```

> **Important**: If another appservice has exclusive access to a namespace (e.g., an agent appservice owns `@agent-.+`), the bridge's non-exclusive entry still allows it to send events on behalf of those users.

### Step 3: Restart Synapse and bridge

Synapse reads appservice registrations on startup, so it needs a restart after changing the registration file.

### How it activates

- For **puppet entries**: `loadPuppets()` automatically calls `setupUserDoublePuppet()` for each puppet.
- For **auto-login users**: `autoLogin()` calls `setupUserDoublePuppet()` using the AS token config, with a fallback to the legacy password-based method (`SYNAPSE_DOUBLE_PUPPET_PASSWORD`).
- No manual API calls are needed -- double puppeting is set up automatically.

### Verification

Check the bridge logs for:
```
Double puppet enabled for auto-login user  mm_user_id=... mxid=@admin:example.com
```
or:
```
Puppet double-puppet login  puppet=ALICE mm_user_id=... mxid=@alice:example.com
```

---

## 10. Echo Prevention

The bridge has multiple layers of echo prevention to stop infinite message loops.
When configuring puppets, be aware:

1. **Bridge bot user ID check** — the main bridge bot (also the relay bot) is
   always filtered
2. **System message filtering** — MM system messages (join/leave/etc.) are
   dropped
3. **Puppet bot user ID check** — messages from known puppet MM user IDs are
   filtered
4. **Configurable username prefix** — set `bot_prefix` in config to filter any
   MM username starting with that prefix (e.g., `"myorg-"`), plus hardcoded
   patterns like `kchat-bridge` and `kchat_*`

> **Never simplify or remove these layers.** They are all required to prevent
> loops in different edge cases.

---

## 11. Troubleshooting

| Problem | Cause | Fix |
|---------|-------|-----|
| Relay silently drops messages | Missing `message_formats` in relay config | Add the full `message_formats` block (Section 5) |
| Bot can't post to channel | Bot not added as channel member | Add via API: `POST /channels/{id}/members` |
| Puppet not picked up | MXID mismatch between env var and appservice | Ensure MXID exactly matches what the bridge sees |
| Health probe fails | Using HTTP GET on `/_matrix/app/v1/ping` | Switch to TCP socket probe (Section 6) |
| New rooms don't have relay | `autoSetRelay()` ran before room was created | Manually set relay or restart bridge |
| Hot-reload doesn't pick up new puppets | ConfigMap/Secret updated but not reloaded | POST to `/api/reload-puppets` or restart pod |
| Token expired or invalid | Personal access tokens were revoked | Regenerate via `POST /users/{id}/tokens` |
| Double puppet not working | Bridge AS token can't impersonate user | Add non-exclusive namespace to bridge registration (Section 9) |
| Ghost instead of real MXID | `double_puppet.secrets` missing or wrong AS token | Ensure `as_token:` prefix and token matches registration |

---

## 12. Architecture Diagram

```
┌────────────────────────────────────────────────────────────────┐
│                        Your Cluster                            │
│                                                                │
│  ┌─── Init Job (runs once) ─────────────────────────────────┐ │
│  │  1. Create MM admin, team, channels                      │ │
│  │  2. Register Synapse users                               │ │
│  │  3. Create MM bots + tokens for each puppet              │ │
│  │  4. Write ConfigMap (MXIDs) + Secret (tokens)            │ │
│  └──────────────────────────────────────────────────────────┘ │
│                              │                                 │
│                    ConfigMap + Secret                           │
│                              │                                 │
│  ┌──────────────────────────▼──────────────────────────────┐  │
│  │  matrix-kchat Pod                                 │  │
│  │                                                         │  │
│  │  envFrom:                                               │  │
│  │    - kchat-puppet-config   (MXIDs)                 │  │
│  │    - kchat-puppet-tokens   (tokens)                │  │
│  │                                                         │  │
│  │  :29319  Appservice API  ◀──── Matrix homeserver        │  │
│  │  :29320  Admin API       ◀──── reload-puppets calls     │  │
│  │                                                         │  │
│  │  ┌─────────── Puppet Router ──────────────────┐         │  │
│  │  │  @alice:example.com → alice-bot token ──▶ MM         │  │
│  │  │  @bob:example.com   → bob-bot token   ──▶ MM         │  │
│  │  │  (no match)         → relay-bot       ──▶ MM         │  │
│  │  └────────────────────────────────────────────┘         │  │
│  │                                                         │  │
│  │  ◀──── MM WebSocket ──── kChat server              │  │
│  │  (echo prevention filters puppet/bridge/relay posts)    │  │
│  └─────────────────────────────────────────────────────────┘  │
│                                                                │
│  ┌──────────────┐    ┌──────────────┐    ┌────────────────┐   │
│  │  kChat  │    │   Synapse     │    │  PostgreSQL    │   │
│  │  :8065       │    │   :8008       │    │  :5432         │   │
│  └──────────────┘    └──────────────┘    └────────────────┘   │
└────────────────────────────────────────────────────────────────┘
```
