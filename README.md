# matrix-kchat

[![CI](https://github.com/rosch100/kchat/actions/workflows/ci.yml/badge.svg)](https://github.com/rosch100/kchat/actions/workflows/ci.yml)
[![License: MPL 2.0](https://img.shields.io/badge/License-MPL_2.0-brightgreen.svg)](LICENSE)

A Matrix bridge for [Infomaniak kChat](https://www.infomaniak.com/en/kchat), built on the [mautrix bridgev2](https://github.com/mautrix/go) framework.

Derived from [aiku/mautrix-mattermost](https://github.com/aiku/mautrix-mattermost) (MPL-2.0) — kChat speaks the Mattermost v4 REST/WebSocket API, so the connector reuses the official [Mattermost Go SDK](https://github.com/mattermost/mattermost-server) client.

## Features

- Bidirectional messaging: text, images, videos, audio, files
- Reactions, edits, deletes (redactions), replies/threads
- Typing indicators (kChat → Matrix via realtime socket)
- Read receipts (kChat → Matrix via `last_viewed_at` polling; kChat has no
  mark-read API, so receipts are one-way)
- Rich formatting: Matrix HTML ↔ kChat markdown
- Direct messages and channel portals; start DMs from Beeper via the
  `pm <kChat-username>` command or the provisioning API
- Message transport: per-5 s channel post polling (kChat's server-side
  broadcast does not reach third-party sockets) plus realtime socket for
  typing/presence
- Message backfill on first sync
- Login via kChat personal access token (created in the Infomaniak Manager
  with the kChat scope; kChat has no password login route)
- bridgev2 native: appservice websockets, E2EE support, double puppeting

## Requirements

- Go 1.25+ (build only)
- libolm for E2EE (`brew install libolm` on macOS)
- A kChat access token: create one in the [Infomaniak Manager](https://manager.infomaniak.com/v3/ng/accounts/token/list) or via your kChat account settings

## Setup with Beeper (bridge-manager)

1. Clone this repository and build and install [bbctl](https://github.com/beeper/bridge-manager):

   ```bash
   git clone https://github.com/beeper/bridge-manager.git
   cd bridge-manager && ./build.sh
   ```

2. Generate the Beeper-flavoured config (network section comes from this bridge):

   ```bash
   bbctl config --type bridgev2 sh-kchat > config.yaml
   ```

3. Fill in the `network` section (`server_url` etc. — see `config.yaml.example`) and start the bridge:

   ```bash
   ./matrix-kchat -c config.yaml
   ```

## Setup with a self-hosted Matrix server

```bash
make build
./matrix-kchat -e -c config.yaml     # generate example config, edit it
./matrix-kchat -g -c config.yaml -r registration.yaml
# Register registration.yaml with your homeserver (app_service_config_files in Synapse)
./matrix-kchat -c config.yaml
```

## Login

1. Start a chat with the bridge bot (`@matrix-kchatbot:your.server` or `@sh-kchatbot:beeper.local`).
2. Send `login`, then enter your kChat server URL (`https://your-team.kchat.infomaniak.com`)
   and a personal access token (kChat has no password login route; tokens are created
   in kChat under Account Settings -> Security -> Personal Access Tokens).

## kChat specifics

- The API base URL is always `https://<team>.kchat.infomaniak.com/api/v4` (Mattermost-compatible).
- kChat API rate limit: 60 requests/minute — the bridge batches syncs accordingly.
- Bots/bridges need `read_channel`/`create_post` permissions on the channels they bridge.

## License

MPL-2.0. This project is a derivative of [aiku/mautrix-mattermost](https://github.com/aiku/mautrix-mattermost) by Remi Philippe; see `THIRD_PARTY_NOTICES`.
