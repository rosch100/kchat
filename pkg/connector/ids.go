// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"strconv"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

// MakePortalID creates a networkid.PortalID from a kChat channel ID.
func MakePortalID(channelID string) networkid.PortalID {
	return networkid.PortalID(channelID)
}

// ParsePortalID extracts the kChat channel ID from a PortalID.
func ParsePortalID(portalID networkid.PortalID) string {
	return string(portalID)
}

// MakeUserID creates a networkid.UserID from a kChat user ID.
func MakeUserID(userID string) networkid.UserID {
	return networkid.UserID(userID)
}

// ParseUserID extracts the kChat user ID from a networkid.UserID.
func ParseUserID(userID networkid.UserID) string {
	return string(userID)
}

// MakeMessageID creates a networkid.MessageID from a kChat post ID.
func MakeMessageID(postID string) networkid.MessageID {
	return networkid.MessageID(postID)
}

// ParseMessageID extracts the kChat post ID from a MessageID.
func ParseMessageID(messageID networkid.MessageID) string {
	return string(messageID)
}

// MakeMessagePartID creates a networkid.PartID for message parts (e.g., file attachments).
func MakeMessagePartID(index int) networkid.PartID {
	if index == 0 {
		return ""
	}
	return networkid.PartID(strconv.Itoa(index))
}

// MakeEmojiID creates a networkid.EmojiID from a kChat emoji name.
func MakeEmojiID(emojiName string) networkid.EmojiID {
	return networkid.EmojiID(emojiName)
}

// ParseEmojiID extracts the kChat emoji name from an EmojiID.
func ParseEmojiID(emojiID networkid.EmojiID) string {
	return string(emojiID)
}

// makePortalKey creates a networkid.PortalKey from a kChat channel ID.
// The receiver is always set so split-portals configs work correctly.
func makePortalKey(login *bridgev2.UserLogin, channelID string) networkid.PortalKey {
	return networkid.PortalKey{
		ID:       MakePortalID(channelID),
		Receiver: login.ID,
	}
}
