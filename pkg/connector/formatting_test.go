// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"testing"

	"maunium.net/go/mautrix/event"
)

func TestMatrixfmtParse(t *testing.T) {
	t.Parallel()
	content := &event.MessageEventContent{
		MsgType: event.MsgText,
		Body:    "Hello world",
	}
	result := matrixfmtParse(content)
	if result != "Hello world" {
		t.Errorf("matrixfmtParse plain text: got %q, want %q", result, "Hello world")
	}
}

func TestMatrixfmtParse_Formatted(t *testing.T) {
	t.Parallel()
	content := &event.MessageEventContent{
		MsgType:       event.MsgText,
		Body:          "bold text",
		Format:        event.FormatHTML,
		FormattedBody: "<strong>bold</strong> text",
	}
	result := matrixfmtParse(content)
	if result == "" {
		t.Error("matrixfmtParse should return non-empty for formatted content")
	}
}

func TestKChatFmtParse(t *testing.T) {
	t.Parallel()
	result := kChatFmtParse("**bold** text")
	if result == nil {
		t.Fatal("kChatFmtParse should not return nil")
	}
	if result.Body == "" {
		t.Error("parsed Body should not be empty")
	}
}
