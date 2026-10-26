// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"testing"

	"maunium.net/go/mautrix/bridgev2/networkid"
)

func TestMakePortalID(t *testing.T) {
	t.Parallel()
	id := MakePortalID("ch123")
	if id != networkid.PortalID("ch123") {
		t.Errorf("MakePortalID: got %q, want %q", id, "ch123")
	}
}

func TestParsePortalID(t *testing.T) {
	t.Parallel()
	got := ParsePortalID(networkid.PortalID("ch456"))
	if got != "ch456" {
		t.Errorf("ParsePortalID: got %q, want %q", got, "ch456")
	}
}

func TestPortalIDRoundTrip(t *testing.T) {
	t.Parallel()
	original := "channel-abc-123"
	got := ParsePortalID(MakePortalID(original))
	if got != original {
		t.Errorf("PortalID round trip: got %q, want %q", got, original)
	}
}

func TestMakeUserID(t *testing.T) {
	t.Parallel()
	id := MakeUserID("user42")
	if id != networkid.UserID("user42") {
		t.Errorf("MakeUserID: got %q, want %q", id, "user42")
	}
}

func TestParseUserID(t *testing.T) {
	t.Parallel()
	got := ParseUserID(networkid.UserID("user99"))
	if got != "user99" {
		t.Errorf("ParseUserID: got %q, want %q", got, "user99")
	}
}

func TestUserIDRoundTrip(t *testing.T) {
	t.Parallel()
	original := "abc123def456"
	got := ParseUserID(MakeUserID(original))
	if got != original {
		t.Errorf("UserID round trip: got %q, want %q", got, original)
	}
}

func TestMakeMessageID(t *testing.T) {
	t.Parallel()
	id := MakeMessageID("post789")
	if id != networkid.MessageID("post789") {
		t.Errorf("MakeMessageID: got %q, want %q", id, "post789")
	}
}

func TestParseMessageID(t *testing.T) {
	t.Parallel()
	got := ParseMessageID(networkid.MessageID("post111"))
	if got != "post111" {
		t.Errorf("ParseMessageID: got %q, want %q", got, "post111")
	}
}

func TestMessageIDRoundTrip(t *testing.T) {
	t.Parallel()
	original := "post-xyz-789"
	got := ParseMessageID(MakeMessageID(original))
	if got != original {
		t.Errorf("MessageID round trip: got %q, want %q", got, original)
	}
}

func TestMakeMessagePartID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		index int
		want  networkid.PartID
	}{
		{0, ""},
		{1, networkid.PartID("1")},
		{2, networkid.PartID("2")},
		{5, networkid.PartID("5")},
	}
	for _, tt := range tests {
		got := MakeMessagePartID(tt.index)
		if got != tt.want {
			t.Errorf("MakeMessagePartID(%d): got %q, want %q", tt.index, got, tt.want)
		}
	}
}

func TestMakeMessagePartID_LargeIndex(t *testing.T) {
	t.Parallel()
	part9 := MakeMessagePartID(9)
	if string(part9) != "9" {
		t.Errorf("MakeMessagePartID(9) = %q, want %q", part9, "9")
	}

	// Previously index >= 10 produced wrong characters (e.g., ':' for 10).
	// Fixed to use strconv.Itoa.
	part10 := MakeMessagePartID(10)
	if string(part10) != "10" {
		t.Errorf("MakeMessagePartID(10) = %q, want %q", part10, "10")
	}

	part99 := MakeMessagePartID(99)
	if string(part99) != "99" {
		t.Errorf("MakeMessagePartID(99) = %q, want %q", part99, "99")
	}
}

func TestMakeEmojiID(t *testing.T) {
	t.Parallel()
	id := MakeEmojiID("thumbsup")
	if id != networkid.EmojiID("thumbsup") {
		t.Errorf("MakeEmojiID: got %q, want %q", id, "thumbsup")
	}
}

func TestParseEmojiID(t *testing.T) {
	t.Parallel()
	got := ParseEmojiID(networkid.EmojiID("heart"))
	if got != "heart" {
		t.Errorf("ParseEmojiID: got %q, want %q", got, "heart")
	}
}

func TestEmojiIDRoundTrip(t *testing.T) {
	t.Parallel()
	original := "custom_emoji_name"
	got := ParseEmojiID(MakeEmojiID(original))
	if got != original {
		t.Errorf("EmojiID round trip: got %q, want %q", got, original)
	}
}

func TestMakePortalKey(t *testing.T) {
	t.Parallel()
	key := makePortalKey("ch-test")
	if string(key.ID) != "ch-test" {
		t.Errorf("makePortalKey ID: got %q, want %q", key.ID, "ch-test")
	}
	if key.Receiver != "" {
		t.Errorf("makePortalKey Receiver: got %q, want empty", key.Receiver)
	}
}

func TestMakeUserLoginID(t *testing.T) {
	t.Parallel()
	id := MakeUserLoginID("login-user-1")
	if id != networkid.UserLoginID("login-user-1") {
		t.Errorf("MakeUserLoginID: got %q, want %q", id, "login-user-1")
	}
}

func TestParseUserLoginID(t *testing.T) {
	t.Parallel()
	got := ParseUserLoginID(networkid.UserLoginID("login-user-2"))
	if got != "login-user-2" {
		t.Errorf("ParseUserLoginID: got %q, want %q", got, "login-user-2")
	}
}

func TestUserLoginIDRoundTrip(t *testing.T) {
	t.Parallel()
	original := "mm-user-abc123"
	got := ParseUserLoginID(MakeUserLoginID(original))
	if got != original {
		t.Errorf("UserLoginID round trip: got %q, want %q", got, original)
	}
}
