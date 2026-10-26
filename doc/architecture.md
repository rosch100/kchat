# Architecture

## Overview

matrix-kchat is a Matrix-kChat bridge built on the [mautrix bridgev2](https://github.com/mautrix/go) framework. It enables bidirectional message synchronization between Matrix rooms and kChat channels with per-user identity preservation through a puppet routing system.

## Component Diagram

```
┌─────────────────┐     ┌──────────────────────┐     ┌──────────────────┐
│   Matrix         │     │  matrix-kchat   │     │   kChat     │
│   Homeserver     │     │                        │     │   Server         │
│   (Synapse)      │◄───►│  ┌──────────────────┐  │◄───►│                  │
│                  │     │  │ Puppet Router     │  │     │  Bot accounts    │
│  Appservice API  │────►│  │ (resolvePostClient)  │──│────►│  per Matrix user │
│                  │     │  └──────────────────┘  │     │                  │
│                  │     │  ┌──────────────────┐  │     │                  │
│                  │◄────│  │ WebSocket Listener│◄─│─────│  Real-time events│
│                  │     │  └──────────────────┘  │     │                  │
│                  │     │  ┌──────────────────┐  │     │                  │
│                  │     │  │ Admin API (:29320)│  │     │                  │
│                  │     │  └──────────────────┘  │     │                  │
└─────────────────┘     └──────────────────────┘     └──────────────────┘
```

## Message Flow

### Matrix to kChat

1. Matrix user sends message in a bridged room
2. Synapse pushes event to bridge via Appservice API
3. Bridge extracts sender MXID from event
4. `resolvePostClient()` performs 3-path puppet lookup:
   a. Check `origSender` (relay metadata) against puppet map
   b. Check `evt.Sender` (direct event sender) against puppet map
   c. Fall back to relay bot client
5. Message posted to kChat using the resolved bot's API token
6. Message appears under the puppet bot's identity in kChat

### kChat to Matrix

1. kChat event arrives via WebSocket
2. Echo prevention filters (5 layers) check if this is a bridge-generated message
3. If real user message: convert to Matrix event format
4. Send to corresponding Matrix room via bridge bot
5. Message appears in Matrix room with kChat user attribution

## Key Components

| Component | File | Responsibility |
|-----------|------|---------------|
| Connector | `pkg/connector/connector.go` | Core state, puppet loading, relay management, admin API |
| Client | `pkg/connector/client.go` | MM API + WebSocket, channel sync, connection lifecycle |
| Matrix Handler | `pkg/connector/handlematrix.go` | Matrix to MM message conversion, puppet routing |
| MM Handler | `pkg/connector/handlekchat.go` | MM to Matrix event conversion, echo prevention |
| Chat Info | `pkg/connector/chatinfo.go` | Channel/user metadata, member list conversion |
| IDs | `pkg/connector/ids.go` | Network ID type mapping (portal, user, message, emoji) |
| Login | `pkg/connector/login.go` | Token and password authentication flows |
| Config | `pkg/connector/config.go` | Bridge configuration, displayname template |
| Formatting Glue | `pkg/connector/formatting.go` | Delegates to formatter packages |
| Matrix Formatter | `pkg/connector/matrixfmt/` | HTML to Markdown |
| MM Formatter | `pkg/connector/kchatfmt/` | Markdown to HTML |
| Entry Point | `cmd/matrix-kchat/main.go` | Bridge binary, wires connector to mxmain |

## Threading Model

- **Main goroutine**: Bridge framework HTTP server (appservice on port 29319)
- **WebSocket goroutine**: kChat real-time event listener (`listenWebSocket`)
- **WatchNewPortals goroutine**: Periodic portal relay checker (default 60s interval)
- **Admin API goroutine**: HTTP server on port 29320 for puppet hot-reload
- **autoLogin goroutine**: Deferred auto-login after bridge framework init
- **autoSetRelay goroutine**: Retries relay setup across new portals (3 attempts)
- **syncChannels goroutine**: Fetches all team channels after WebSocket connects

Puppet map access is protected by `sync.RWMutex` for thread safety. The `Puppets` map is read-locked during message routing (`resolvePostClient` via `IsPuppetUserID`) and write-locked during reload operations (`ReloadPuppetsFromEntries`).

## Double Puppeting (MM → Matrix)

Double puppeting makes incoming kChat messages appear in Matrix under the real MXID of the corresponding Matrix user, rather than a ghost account (`@kchat_<id>:server`).

### How It Works

1. The bridge config declares `double_puppet.secrets` with the bridge's own AS token:
   ```yaml
   double_puppet:
     secrets:
       example.com: "as_token:bridge_as_token_here"
   ```
2. On startup, `loadPuppets()` calls `setupUserDoublePuppet()` for each puppet, which registers a `UserLogin` via `LoginDoublePuppet()` using the AS token.
3. `autoLogin()` also calls `setupUserDoublePuppet()` for the auto-login user, with a fallback to the legacy password-based `setupDoublePuppet()`.
4. The `dpLogins` map tracks MM user ID → `UserLoginID`. When a kChat event arrives, `senderFor()` checks this map and routes the event through the double puppet login if available.
5. The bridge sends the Matrix event using the AS token with `?user_id=@realuser:server`, so it appears as the real user.

### Requirements

- The bridge's appservice registration must include **non-exclusive namespaces** for all users that need double puppeting. The bridge AS token can only impersonate users in its registered namespaces.
- The `double_puppet.servers` config must point to the homeserver URL for each domain.

### Code Path

```
loadPuppets() / autoLogin()
  → setupUserDoublePuppet(ctx, mmUserID, ownerMXID)
    → bridge.LoginDoublePuppet(ctx, "appservice-config")
      → dpLogins[mmUserID] = userLoginID
        → senderFor() uses dpLogin for incoming MM events
```

## Relay System

The bridgev2 framework requires a "relay login" to be set on each portal room before it will deliver Matrix messages through `HandleMatrixMessage`. Without a relay, the framework rejects messages from non-logged-in users with "not logged in" before the puppet system is ever reached.

The relay is set through two mechanisms:
1. **autoSetRelay**: Runs after auto-login, retries 3 times with 30s delays to catch portals created during initial channel sync
2. **WatchNewPortals**: Continuous 60s polling loop that catches portals created after startup (e.g., when new channels are bridged)
