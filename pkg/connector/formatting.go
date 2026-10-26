// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"github.com/rosch100/kchat/pkg/connector/kchatfmt"
	"github.com/rosch100/kchat/pkg/connector/matrixfmt"
	"maunium.net/go/mautrix/event"
)

// kChatFmtParse converts kChat markdown to Matrix HTML message content.
func kChatFmtParse(text string) *kchatfmt.ParsedMessage {
	return kchatfmt.Parse(text)
}

// matrixfmtParse converts Matrix message content to kChat markdown.
func matrixfmtParse(content *event.MessageEventContent) string {
	return matrixfmt.Parse(content)
}
