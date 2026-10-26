// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

// Command matrix-kchat is a Matrix-kChat puppeting bridge built
// on the mautrix bridgev2 framework. It translates messages between the two
// platforms and supports per-user puppet identity routing so each Matrix user
// can post to kChat under a dedicated bot account.
package main

import (
	"git.altanis.de/rosch/kchat/pkg/connector"
	"maunium.net/go/mautrix/bridgev2/matrix/mxmain"
)

// These are filled at build time with -ldflags.
var (
	Tag       = "unknown"
	Commit    = "unknown"
	BuildTime = "unknown"
)

var m = mxmain.BridgeMain{
	Name:        "matrix-kchat",
	URL:         "https://git.altanis.de/rosch/kchat",
	Description: "A Matrix-kChat bridge",
	Version:     "0.1.0",

	Connector: &connector.KChatConnector{},
}

func main() {
	m.InitVersion(Tag, Commit, BuildTime)
	m.Run()
}
