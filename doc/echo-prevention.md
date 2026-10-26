# Echo Prevention

## Problem

A bridge creates a feedback loop by design: it copies messages between two platforms. Without echo prevention, this happens:

1. User sends message in Matrix
2. Bridge relays message to kChat (posted by puppet or relay bot)
3. Bridge receives the kChat post via WebSocket
4. Bridge relays message back to Matrix (as a new message)
5. Step 1 repeats indefinitely

This produces infinite duplicate messages on both sides.

## Solution

The bridge uses 5 layers of echo prevention in the kChat-to-Matrix direction (`handlePosted` in `handlekchat.go`). Each layer catches a different category of bridge-generated messages.

### Layer 1: Bridge Bot User ID Check

```go
if post.UserId == m.userID {
    return
}
```

Filters out posts created by the bridge's own logged-in kChat account (the relay bot). This is the most basic check -- the bridge should never relay its own posts back.

**What it catches**: All messages posted by the bridge's primary relay account.

### Layer 2: System Message Filtering

```go
if post.Type != "" && post.Type != model.PostTypeDefault {
    return
}
```

Filters out kChat system messages (join/leave notifications, header changes, channel purpose updates, etc.). These have post types like `system_join_channel`, `system_header_change`, and similar.

**What it catches**: Non-user-generated channel events that should not be bridged as chat messages.

### Layer 3: Puppet Bot User ID Check

```go
if m.connector.IsPuppetUserID(post.UserId) {
    return
}
```

Filters out posts from any loaded puppet bot account. When the bridge posts a message via a puppet, the WebSocket delivers that post event back. Without this check, every puppet message would be relayed back to Matrix as a duplicate.

`IsPuppetUserID()` iterates the puppet map under a read lock and compares kChat user IDs.

**What it catches**: Messages posted by the bridge via the puppet system.

### Layer 4: Relay Bot User ID Check

This is implicitly handled by Layer 1 since the relay bot IS the bridge's logged-in user. If the relay bot were a separate account, it would need its own check.

In the current architecture, the relay bot is the auto-login user whose credentials are provided via `KCHAT_AUTO_TOKEN`. Layer 1 covers it.

### Layer 5: Bridge Username Prefix Check

```go
senderName, _ := evt.GetData()["sender_name"].(string)
senderName = strings.TrimPrefix(senderName, "@")
if senderName != "" && isBridgeUsername(senderName, m.connector.Config.BotPrefix) {
    return
}
```

Filters out posts from usernames that match known bridge patterns:

1. **Exact match**: `kchat-bridge` (the conventional bridge bot name)
2. **Ghost prefix**: `kchat_` (ghost users created by the bridge's `username_template: kchat_{{.}}`)
3. **Configurable prefix**: Any username starting with the value of `bot_prefix` in config

**What it catches**: Bridge ghost users, the bridge bot under its canonical name, and any custom bot accounts that share a configurable naming convention.

## Why Each Layer Exists

It is tempting to simplify to fewer layers, but each catches a distinct failure mode:

| Layer | Catches | Without it... |
|-------|---------|--------------|
| Bridge bot user ID | Relay bot's own posts | Every relayed message loops back |
| System messages | Join/leave/header events | Channel admin actions create chat spam |
| Puppet user IDs | Puppet-posted messages | Every puppet message duplicates |
| Username `kchat-bridge` | Bridge bot under canonical name | Bridge bot's own posts relay if user ID check fails (e.g., reconnection with new session) |
| Username prefix `kchat_` | Ghost users from bridge template | Bridge-created ghost users echo |
| Configurable `bot_prefix` | Deployment-specific bots | Custom puppet bots with non-standard names echo |

### Why simplifying is dangerous

- **Layers 1+3 are not redundant**: Layer 1 checks the relay bot. Layer 3 checks puppets. They are different sets of user IDs.
- **User ID checks can miss edge cases**: If the bridge reconnects with a new session or the user ID is stale, username-based filtering (Layer 5) provides a safety net.
- **Username checks alone are not sufficient**: A kChat user could change their username. User ID checks are authoritative; username checks are a fallback.
- **System message filtering is orthogonal**: It prevents a different class of noise (channel events vs. chat echoes).

## Reactions

Reactions use all applicable echo prevention layers but skip Layer 2 (system message filtering) because reactions don't have a post `Type` field. The applicable layers are:

1. **Bridge bot user ID** — skip own reactions (`reaction.UserId == m.userID`)
3. **Puppet user IDs** — skip reactions from puppet bots (`IsPuppetUserID`)
5. **Bridge username prefix** — skip reactions from bridge-patterned usernames

This is by design, not a gap — Layer 2 is structurally N/A for reactions.

## Configuration

### `bot_prefix` in config.yaml

```yaml
# Username prefix for echo prevention
bot_prefix: "mybridge-"
```

When set, any kChat username starting with this prefix is treated as a bridge-managed bot and its posts are not relayed. This is useful when puppet bot accounts share a naming convention (e.g., `mybridge-alice`, `mybridge-bob`).

Leave empty to disable prefix-based filtering. The other layers still provide echo prevention.

### Implementation

The `isBridgeUsername` function in `handlekchat.go`:

```go
func isBridgeUsername(username, botPrefix string) bool {
    switch {
    case username == "kchat-bridge":
        return true
    case strings.HasPrefix(username, "kchat_"):
        return true
    case botPrefix != "" && strings.HasPrefix(username, botPrefix):
        return true
    default:
        return false
    }
}
```
