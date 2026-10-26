// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Package connector implements a Matrix-kChat bridge using the mautrix
// bridgev2 framework.
//
// The key differentiator of this bridge is puppet identity routing: each
// Matrix user can post to kChat under a dedicated bot account rather
// than a shared relay identity. Puppets are configured via environment
// variables (KCHAT_PUPPET_*) or the hot-reload HTTP API at
// POST /api/reload-puppets.
//
// # Core Types
//
// [KChatConnector] implements [bridgev2.NetworkConnector] and manages the
// bridge lifecycle, puppet registry, admin API, and portal relay watcher.
//
// [KChatClient] represents an authenticated kChat user session. It
// maintains a WebSocket connection for real-time events and performs REST API
// calls for channel sync, message sending, and backfill.
//
// [PuppetClient] maps a Matrix user (MXID) to a kChat bot/user client,
// allowing that user's messages to appear under the bot's identity.
//
// # Echo Prevention
//
// The bridge uses a multi-layer echo prevention system to avoid infinite
// message loops between the two platforms. Layers include puppet user ID
// checks, bridge bot ID checks, relay bot ID checks, configurable username
// prefix matching, and system message filtering. These layers must not be
// simplified or removed.
//
// # Sub-packages
//
//   - matrixfmt converts Matrix HTML to kChat markdown.
//   - kchatfmt converts kChat markdown to Matrix HTML.
package connector
