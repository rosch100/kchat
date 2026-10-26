// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"context"
	"testing"

	"maunium.net/go/mautrix/event"
)

func TestGetCapabilities_Formatting(t *testing.T) {
	t.Parallel()
	client := &KChatClient{}
	caps := client.GetCapabilities(context.Background(), nil)

	expectedFormats := []event.FormattingFeature{
		event.FmtBold,
		event.FmtItalic,
		event.FmtStrikethrough,
		event.FmtInlineCode,
		event.FmtCodeBlock,
		event.FmtBlockquote,
		event.FmtInlineLink,
		event.FmtUserLink,
		event.FmtUnorderedList,
		event.FmtOrderedList,
		event.FmtHeaders,
	}

	for _, fmt := range expectedFormats {
		level, ok := caps.Formatting[fmt]
		if !ok {
			t.Errorf("Formatting missing %v", fmt)
			continue
		}
		if level != event.CapLevelFullySupported {
			t.Errorf("Formatting %v: got %v, want FullySupported", fmt, level)
		}
	}
}

func TestGetCapabilities_Files(t *testing.T) {
	t.Parallel()
	client := &KChatClient{}
	caps := client.GetCapabilities(context.Background(), nil)

	fileTypes := []event.MessageType{
		event.MsgImage,
		event.MsgVideo,
		event.MsgAudio,
		event.MsgFile,
	}

	for _, ft := range fileTypes {
		fc, ok := caps.File[ft]
		if !ok {
			t.Errorf("File support missing for %v", ft)
			continue
		}
		if fc.MaxSize != 100*1024*1024 {
			t.Errorf("File %v MaxSize: got %d, want %d", ft, fc.MaxSize, 100*1024*1024)
		}
	}
}

func TestGetCapabilities_ImageCaption(t *testing.T) {
	t.Parallel()
	client := &KChatClient{}
	caps := client.GetCapabilities(context.Background(), nil)

	imageFC := caps.File[event.MsgImage]
	if imageFC.Caption != event.CapLevelFullySupported {
		t.Errorf("Image caption: got %v, want FullySupported", imageFC.Caption)
	}

	videoFC := caps.File[event.MsgVideo]
	if videoFC.Caption != event.CapLevelFullySupported {
		t.Errorf("Video caption: got %v, want FullySupported", videoFC.Caption)
	}
}

func TestGetCapabilities_Features(t *testing.T) {
	t.Parallel()
	client := &KChatClient{}
	caps := client.GetCapabilities(context.Background(), nil)

	if caps.MaxTextLength != 16383 {
		t.Errorf("MaxTextLength: got %d, want 16383", caps.MaxTextLength)
	}
	if caps.Reply != event.CapLevelFullySupported {
		t.Errorf("Reply: got %v, want FullySupported", caps.Reply)
	}
	if caps.Edit != event.CapLevelFullySupported {
		t.Errorf("Edit: got %v, want FullySupported", caps.Edit)
	}
	if caps.Delete != event.CapLevelFullySupported {
		t.Errorf("Delete: got %v, want FullySupported", caps.Delete)
	}
	if caps.Reaction != event.CapLevelFullySupported {
		t.Errorf("Reaction: got %v, want FullySupported", caps.Reaction)
	}
	if !caps.ReadReceipts {
		t.Error("ReadReceipts should be true")
	}
	if !caps.TypingNotifications {
		t.Error("TypingNotifications should be true")
	}
}
