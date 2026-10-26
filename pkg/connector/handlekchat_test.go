// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func TestConvertPostToMatrix_TextOnly(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	post := &model.Post{
		Id:        "post1",
		Message:   "Hello world",
		ChannelId: "ch1",
		UserId:    "user1",
	}

	msg := client.convertPostToMatrix(post)

	if len(msg.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(msg.Parts))
	}
	part := msg.Parts[0]
	if part.Type != event.EventMessage {
		t.Errorf("part type: got %v, want EventMessage", part.Type)
	}
	if part.Content.MsgType != event.MsgText {
		t.Errorf("msg type: got %v, want MsgText", part.Content.MsgType)
	}
	if part.Content.Body != "Hello world" {
		t.Errorf("body: got %q, want %q", part.Content.Body, "Hello world")
	}
	if msg.ReplyTo != nil {
		t.Error("ReplyTo should be nil for non-reply")
	}
}

func TestConvertPostToMatrix_WithReply(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	post := &model.Post{
		Id:        "post2",
		Message:   "Reply text",
		ChannelId: "ch1",
		UserId:    "user1",
		RootId:    "parentpost",
	}

	msg := client.convertPostToMatrix(post)

	if msg.ReplyTo == nil {
		t.Fatal("ReplyTo should not be nil for reply")
	}
	if string(msg.ReplyTo.MessageID) != "parentpost" {
		t.Errorf("ReplyTo MessageID: got %q, want %q", msg.ReplyTo.MessageID, "parentpost")
	}
}

func TestConvertPostToMatrix_EmptyMessage(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	post := &model.Post{
		Id:        "post3",
		Message:   "",
		ChannelId: "ch1",
		UserId:    "user1",
	}

	msg := client.convertPostToMatrix(post)

	if len(msg.Parts) != 0 {
		t.Errorf("expected 0 parts for empty message, got %d", len(msg.Parts))
	}
}

func TestConvertPostToMatrix_WithFormatting(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	post := &model.Post{
		Id:        "post4",
		Message:   "**bold** text",
		ChannelId: "ch1",
		UserId:    "user1",
	}

	msg := client.convertPostToMatrix(post)

	if len(msg.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(msg.Parts))
	}
	part := msg.Parts[0]
	if part.Content.Format != event.FormatHTML {
		t.Errorf("format: got %q, want FormatHTML", part.Content.Format)
	}
	if part.Content.FormattedBody == "" {
		t.Error("formatted body should not be empty for markdown content")
	}
}

func TestConvertPostToMatrix_PartIDs(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	post := &model.Post{
		Id:        "post5",
		Message:   "text with file",
		ChannelId: "ch1",
		UserId:    "user1",
	}

	msg := client.convertPostToMatrix(post)

	if len(msg.Parts) < 1 {
		t.Fatal("expected at least 1 part")
	}
	if string(msg.Parts[0].ID) != "" {
		t.Errorf("text part ID: got %q, want empty (index 0)", msg.Parts[0].ID)
	}
}

func TestConvertEditToMatrix(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	post := &model.Post{
		Id:      "post6",
		Message: "edited content",
	}
	existing := []*database.Message{
		{ID: "post6"},
	}

	edit := client.convertEditToMatrix(post, existing)

	if len(edit.ModifiedParts) != 1 {
		t.Fatalf("expected 1 modified part, got %d", len(edit.ModifiedParts))
	}
	part := edit.ModifiedParts[0]
	if part.Part != existing[0] {
		t.Error("Part should reference the existing message")
	}
	if part.Content.Body != "edited content" {
		t.Errorf("body: got %q, want %q", part.Content.Body, "edited content")
	}
}

func TestConvertEditToMatrix_NoExisting(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	post := &model.Post{
		Id:      "post7",
		Message: "edited",
	}

	edit := client.convertEditToMatrix(post, nil)

	if len(edit.ModifiedParts) != 1 {
		t.Fatalf("expected 1 modified part, got %d", len(edit.ModifiedParts))
	}
	if edit.ModifiedParts[0].Part != nil {
		t.Error("Part should be nil when no existing messages")
	}
}

func TestReactionToEmoji_KnownEmojis(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want string
	}{
		{"+1", "\U0001f44d"},
		{"-1", "\U0001f44e"},
		{"heart", "\u2764\ufe0f"},
		{"smile", "\U0001f604"},
		{"fire", "\U0001f525"},
		{"rocket", "\U0001f680"},
		{"eyes", "\U0001f440"},
		{"tada", "\U0001f389"},
		{"100", "\U0001f4af"},
		{"white_check_mark", "\u2705"},
		{"x", "\u274c"},
		{"thumbsup", "\U0001f44d"},
		{"thumbsdown", "\U0001f44e"},
		{"star", "\u2b50"},
		{"pray", "\U0001f64f"},
		{"thinking", "\U0001f914"},
		{"wave", "\U0001f44b"},
		{"clap", "\U0001f44f"},
		{"laughing", "\U0001f606"},
		{"warning", "\u26a0\ufe0f"},
	}

	for _, tt := range tests {
		got := reactionToEmoji(tt.name)
		if got != tt.want {
			t.Errorf("reactionToEmoji(%q): got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestReactionToEmoji_Custom(t *testing.T) {
	t.Parallel()
	got := reactionToEmoji("custom_emoji")
	if got != ":custom_emoji:" {
		t.Errorf("reactionToEmoji(custom): got %q, want %q", got, ":custom_emoji:")
	}
}

func TestEmojiToReaction_KnownEmojis(t *testing.T) {
	t.Parallel()
	tests := []struct {
		emoji string
		want  string
	}{
		{"\U0001f44d", "+1"},
		{"\U0001f44e", "-1"},
		{"\u2764\ufe0f", "heart"},
		{"\U0001f604", "smile"},
		{"\U0001f525", "fire"},
		{"\U0001f680", "rocket"},
		{"\U0001f440", "eyes"},
		{"\U0001f389", "tada"},
		{"\U0001f4af", "100"},
		{"\u2705", "white_check_mark"},
		{"\u274c", "x"},
		{"\u2b50", "star"},
		{"\U0001f64f", "pray"},
	}

	for _, tt := range tests {
		got := emojiToReaction(tt.emoji)
		if got != tt.want {
			t.Errorf("emojiToReaction(%q): got %q, want %q", tt.emoji, got, tt.want)
		}
	}
}

func TestEmojiToReaction_CustomColonFormat(t *testing.T) {
	t.Parallel()
	got := emojiToReaction(":my_custom:")
	if got != "my_custom" {
		t.Errorf("emojiToReaction(:my_custom:): got %q, want %q", got, "my_custom")
	}
}

func TestEmojiToReaction_UnknownPassthrough(t *testing.T) {
	t.Parallel()
	got := emojiToReaction("unknown_char")
	if got != "unknown_char" {
		t.Errorf("emojiToReaction passthrough: got %q, want %q", got, "unknown_char")
	}
}

func TestEmojiToReaction_SingleColon(t *testing.T) {
	t.Parallel()
	got := emojiToReaction(":")
	if got != ":" {
		t.Errorf("emojiToReaction single colon: got %q, want %q", got, ":")
	}
}

func TestEmojiToReaction_EmptyColons(t *testing.T) {
	t.Parallel()
	got := emojiToReaction("::")
	if got != "::" {
		t.Errorf("emojiToReaction empty colons: got %q, want %q", got, "::")
	}
}

func TestHttpToWS(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"https://mm.example.com", "wss://mm.example.com"},
		{"http://localhost:8065", "ws://localhost:8065"},
		{"wss://already.ws.com", "wss://already.ws.com"},
		{"ws://already.ws.com", "ws://already.ws.com"},
		{"ftp://other.com", "ftp://other.com"},
		{"", ""},
	}

	for _, tt := range tests {
		got := httpToWS(tt.input)
		if got != tt.want {
			t.Errorf("httpToWS(%q): got %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsLoggedIn(t *testing.T) {
	t.Parallel()
	client := &KChatClient{userLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{ID: "test-login"}}}
	if client.IsLoggedIn() {
		t.Error("should not be logged in with nil client")
	}

	client.client = model.NewAPIv4Client("http://localhost")
	if client.IsLoggedIn() {
		t.Error("should not be logged in with empty token")
	}

	client.client.SetToken("test-token")
	if !client.IsLoggedIn() {
		t.Error("should be logged in with client and token")
	}
}

func TestIsThisUser(t *testing.T) {
	t.Parallel()
	client := &KChatClient{userLogin: &bridgev2.UserLogin{UserLogin: &database.UserLogin{ID: "test-login"}}, userID: "user123"}

	if !client.IsThisUser(context.TODO(), MakeUserID("user123")) {
		t.Error("should match own user ID")
	}
	if client.IsThisUser(context.TODO(), MakeUserID("otheruser")) {
		t.Error("should not match different user ID")
	}
}

func TestConvertedMessage_PartsAreConvertedMessagePart(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	post := &model.Post{
		Id:      "post8",
		Message: "test",
	}

	msg := client.convertPostToMatrix(post)

	for _, part := range msg.Parts {
		// Verify the part has expected structure.
		if part.Content == nil {
			t.Error("part Content should not be nil")
		}
		var _ = part
	}
}

// ---------------------------------------------------------------------------
// handleEvent dispatch tests
// ---------------------------------------------------------------------------

func TestHandleEvent_Dispatch(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)

	// Build events that should be echoed (own user) so they return early.
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "my-user-id", ChannelId: "ch1", Message: "hello",
	})
	reactionJSON, _ := json.Marshal(&model.Reaction{
		UserId: "my-user-id", PostId: "p1", EmojiName: "+1",
	})

	tests := []struct {
		name      string
		eventType model.WebsocketEventType
		data      map[string]any
	}{
		{"posted", model.WebsocketEventPosted, map[string]any{"post": string(postJSON)}},
		{"post_edited", model.WebsocketEventPostEdited, map[string]any{"post": string(postJSON)}},
		{"post_deleted", model.WebsocketEventPostDeleted, map[string]any{"post": string(postJSON)}},
		{"reaction_added", model.WebsocketEventReactionAdded, map[string]any{"reaction": string(reactionJSON)}},
		{"reaction_removed", model.WebsocketEventReactionRemoved, map[string]any{"reaction": string(reactionJSON)}},
		{"typing", model.WebsocketEventTyping, map[string]any{"user_id": "my-user-id"}},
		{"channel_viewed", kchatEventChannelViewed, map[string]any{}},
		{"unknown_type", "unknown_custom_event", map[string]any{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock.Reset()
			evt := newWebSocketEvent(tt.eventType, "ch1", tt.data)
			mc.handleEvent(evt)
			// Own-user events are relayed now; only typing is echo-filtered.
			// channel_viewed/unknown_type queue nothing in this scenario.
			expected := 1
			if tt.name == "typing" || tt.name == "channel_viewed" || tt.name == "unknown_type" {
				expected = 0
			}
			if len(mock.Events()) != expected {
				t.Errorf("expected %d events, got %d", expected, len(mock.Events()))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// handlePosted echo prevention tests
// ---------------------------------------------------------------------------

func TestHandlePosted_EchoPrevention_OwnPost(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "my-user-id", ChannelId: "ch1", Message: "hello",
	})
	evt := newWebSocketEvent(model.WebsocketEventPosted, "ch1", map[string]any{
		"post":        string(postJSON),
		"sender_name": "@myuser",
	})

	mc.handlePosted(evt)

	if len(mock.Events()) != 1 {
		t.Errorf("expected 1 event (own posts from kChat are relayed), got %d", len(mock.Events()))
	}
}

func TestHandlePosted_EchoPrevention_BridgeSent(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "my-user-id", ChannelId: "ch1", Message: "hello",
	})
	evt := newWebSocketEvent(model.WebsocketEventPosted, "ch1", map[string]any{
		"post":        string(postJSON),
		"sender_name": "@myuser",
	})
	mc.rememberEcho(echoKindPost, "p1")

	mc.handlePosted(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (bridge-sent echo dropped), got %d", len(mock.Events()))
	}
}

func TestHandlePosted_EchoPrevention_SystemMsg(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "other-user", ChannelId: "ch1",
		Message: "joined", Type: "system_join_channel",
	})
	evt := newWebSocketEvent(model.WebsocketEventPosted, "ch1", map[string]any{
		"post":        string(postJSON),
		"sender_name": "@someuser",
	})

	mc.handlePosted(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (system msg filtered), got %d", len(mock.Events()))
	}
}

func TestHandlePosted_EchoPrevention_PuppetUser(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)

	mc.connector.Puppets[id.UserID("@puppet:example.com")] = &PuppetClient{
		MXID:   id.UserID("@puppet:example.com"),
		UserID: "puppet-mm-id",
	}

	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "puppet-mm-id", ChannelId: "ch1", Message: "from puppet",
	})
	evt := newWebSocketEvent(model.WebsocketEventPosted, "ch1", map[string]any{
		"post":        string(postJSON),
		"sender_name": "@puppetuser",
	})

	mc.handlePosted(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (puppet filtered), got %d", len(mock.Events()))
	}
}

func TestHandlePosted_EchoPrevention_BridgeUsername(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "other-user", ChannelId: "ch1",
		Message: "hello", Type: model.PostTypeDefault,
	})
	evt := newWebSocketEvent(model.WebsocketEventPosted, "ch1", map[string]any{
		"post":        string(postJSON),
		"sender_name": "@kchat_ghost",
	})

	mc.handlePosted(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (bridge username filtered), got %d", len(mock.Events()))
	}
}

func TestHandlePosted_MissingData(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	evt := newWebSocketEvent(model.WebsocketEventPosted, "ch1", map[string]any{})

	mc.handlePosted(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (missing data), got %d", len(mock.Events()))
	}
}

func TestHandlePosted_InvalidJSON(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	evt := newWebSocketEvent(model.WebsocketEventPosted, "ch1", map[string]any{
		"post": "this is not valid json{{{",
	})

	mc.handlePosted(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (invalid JSON), got %d", len(mock.Events()))
	}
}

func TestHandlePosted_PassesEchoChecks(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "other-user", ChannelId: "ch1",
		Message: "hello", Type: model.PostTypeDefault,
	})
	evt := newWebSocketEvent(model.WebsocketEventPosted, "ch1", map[string]any{
		"post":        string(postJSON),
		"sender_name": "@normaluser",
	})

	mc.handlePosted(evt)

	events := mock.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event queued, got %d", len(events))
	}
	if events[0].GetType() != bridgev2.RemoteEventMessage {
		t.Errorf("event type: got %v, want RemoteEventMessage", events[0].GetType())
	}
}

// ---------------------------------------------------------------------------
// handlePostEdited tests
// ---------------------------------------------------------------------------

func TestHandlePostEdited_EchoPrevention(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "my-user-id", ChannelId: "ch1", Message: "edited",
	})
	evt := newWebSocketEvent(model.WebsocketEventPostEdited, "ch1", map[string]any{
		"post": string(postJSON),
	})

	mc.handlePostEdited(evt)

	if len(mock.Events()) != 1 {
		t.Errorf("expected 1 event (own edits from kChat are relayed), got %d", len(mock.Events()))
	}
}

func TestHandlePostEdited_EchoPrevention_BridgeSent(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "my-user-id", ChannelId: "ch1", Message: "edited",
	})
	evt := newWebSocketEvent(model.WebsocketEventPostEdited, "ch1", map[string]any{
		"post": string(postJSON),
	})
	mc.rememberEcho(echoKindEdit, "p1")

	mc.handlePostEdited(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (bridge-sent edit echo dropped), got %d", len(mock.Events()))
	}
}

func TestHandlePostEdited_EchoPrevention_Puppet(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)

	mc.connector.Puppets[id.UserID("@puppet:example.com")] = &PuppetClient{
		MXID:   id.UserID("@puppet:example.com"),
		UserID: "puppet-mm-id",
	}

	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "puppet-mm-id", ChannelId: "ch1", Message: "edited by puppet",
	})
	evt := newWebSocketEvent(model.WebsocketEventPostEdited, "ch1", map[string]any{
		"post": string(postJSON),
	})

	mc.handlePostEdited(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (puppet echo prevention), got %d", len(mock.Events()))
	}
}

func TestHandlePostEdited_EchoPrevention_BridgeUsername(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "other-user", ChannelId: "ch1", Message: "edited",
	})
	evt := newWebSocketEvent(model.WebsocketEventPostEdited, "ch1", map[string]any{
		"post":        string(postJSON),
		"sender_name": "@kchat_ghost",
	})

	mc.handlePostEdited(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (bridge username echo prevention), got %d", len(mock.Events()))
	}
}

func TestHandlePostEdited_MissingData(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	evt := newWebSocketEvent(model.WebsocketEventPostEdited, "ch1", map[string]any{})

	mc.handlePostEdited(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (missing data), got %d", len(mock.Events()))
	}
}

func TestHandlePostEdited_PassesEchoChecks(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "other-user", ChannelId: "ch1", Message: "edited",
	})
	evt := newWebSocketEvent(model.WebsocketEventPostEdited, "ch1", map[string]any{
		"post":        string(postJSON),
		"sender_name": "@normaluser",
	})

	mc.handlePostEdited(evt)

	events := mock.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event queued, got %d", len(events))
	}
	if events[0].GetType() != bridgev2.RemoteEventEdit {
		t.Errorf("event type: got %v, want RemoteEventEdit", events[0].GetType())
	}
}

// ---------------------------------------------------------------------------
// handlePostDeleted tests
// ---------------------------------------------------------------------------

func TestHandlePostDeleted_EchoPrevention(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "my-user-id", ChannelId: "ch1", Message: "deleted",
	})
	evt := newWebSocketEvent(model.WebsocketEventPostDeleted, "ch1", map[string]any{
		"post": string(postJSON),
	})

	mc.handlePostDeleted(evt)

	if len(mock.Events()) != 1 {
		t.Errorf("expected 1 event (own deletes from kChat are relayed), got %d", len(mock.Events()))
	}
}

func TestHandlePostDeleted_EchoPrevention_BridgeSent(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "my-user-id", ChannelId: "ch1", Message: "deleted",
	})
	evt := newWebSocketEvent(model.WebsocketEventPostDeleted, "ch1", map[string]any{
		"post": string(postJSON),
	})
	mc.rememberEcho(echoKindDelete, "p1")

	mc.handlePostDeleted(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (bridge-sent delete echo dropped), got %d", len(mock.Events()))
	}
}

func TestHandlePostDeleted_EchoPrevention_Puppet(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)

	mc.connector.Puppets[id.UserID("@puppet:example.com")] = &PuppetClient{
		MXID:   id.UserID("@puppet:example.com"),
		UserID: "puppet-mm-id",
	}

	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "puppet-mm-id", ChannelId: "ch1", Message: "deleted by puppet",
	})
	evt := newWebSocketEvent(model.WebsocketEventPostDeleted, "ch1", map[string]any{
		"post": string(postJSON),
	})

	mc.handlePostDeleted(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (puppet echo prevention), got %d", len(mock.Events()))
	}
}

func TestHandlePostDeleted_EchoPrevention_BridgeUsername(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "other-user", ChannelId: "ch1", Message: "deleted",
	})
	evt := newWebSocketEvent(model.WebsocketEventPostDeleted, "ch1", map[string]any{
		"post":        string(postJSON),
		"sender_name": "@kchat_ghost",
	})

	mc.handlePostDeleted(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (bridge username echo prevention), got %d", len(mock.Events()))
	}
}

func TestHandlePostDeleted_MissingData(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	evt := newWebSocketEvent(model.WebsocketEventPostDeleted, "ch1", map[string]any{})

	mc.handlePostDeleted(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (missing data), got %d", len(mock.Events()))
	}
}

func TestHandlePostDeleted_PassesEchoChecks(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "other-user", ChannelId: "ch1", Message: "deleted",
	})
	evt := newWebSocketEvent(model.WebsocketEventPostDeleted, "ch1", map[string]any{
		"post":        string(postJSON),
		"sender_name": "@normaluser",
	})

	mc.handlePostDeleted(evt)

	events := mock.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event queued, got %d", len(events))
	}
	if events[0].GetType() != bridgev2.RemoteEventMessageRemove {
		t.Errorf("event type: got %v, want RemoteEventMessageRemove", events[0].GetType())
	}
}

// ---------------------------------------------------------------------------
// handleReactionAdded / handleReactionRemoved tests
// ---------------------------------------------------------------------------

func TestHandleReactionAdded_MissingData(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	evt := newWebSocketEvent(model.WebsocketEventReactionAdded, "ch1", map[string]any{})

	mc.handleReactionAdded(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (missing data), got %d", len(mock.Events()))
	}
}

func TestHandleReactionAdded_InvalidJSON(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	evt := newWebSocketEvent(model.WebsocketEventReactionAdded, "ch1", map[string]any{
		"reaction": "not valid json{{{",
	})

	mc.handleReactionAdded(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (invalid JSON), got %d", len(mock.Events()))
	}
}

func TestHandleReactionRemoved_MissingData(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	evt := newWebSocketEvent(model.WebsocketEventReactionRemoved, "ch1", map[string]any{})

	mc.handleReactionRemoved(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (missing data), got %d", len(mock.Events()))
	}
}

func TestHandleReactionRemoved_InvalidJSON(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	evt := newWebSocketEvent(model.WebsocketEventReactionRemoved, "ch1", map[string]any{
		"reaction": "bad json!!!",
	})

	mc.handleReactionRemoved(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (invalid JSON), got %d", len(mock.Events()))
	}
}

func TestHandlePostEdited_InvalidJSON(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	evt := newWebSocketEvent(model.WebsocketEventPostEdited, "ch1", map[string]any{
		"post": "this is not valid json{{{",
	})

	mc.handlePostEdited(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (invalid JSON), got %d", len(mock.Events()))
	}
}

func TestHandlePostDeleted_InvalidJSON(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	evt := newWebSocketEvent(model.WebsocketEventPostDeleted, "ch1", map[string]any{
		"post": "bad json!!!",
	})

	mc.handlePostDeleted(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (invalid JSON), got %d", len(mock.Events()))
	}
}

func TestHandleReactionAdded_EchoPrevention(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	reactionJSON, _ := json.Marshal(&model.Reaction{
		UserId: "my-user-id", PostId: "p1", EmojiName: "+1",
	})
	mc.rememberEcho(echoKindReaction, "p1:+1:my-user-id")
	evt := newWebSocketEvent(model.WebsocketEventReactionAdded, "ch1", map[string]any{
		"reaction": string(reactionJSON),
	})

	mc.handleReactionAdded(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (bridge echo prevention), got %d", len(mock.Events()))
	}
}

func TestHandleReactionAdded_PassesEchoChecks(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	reactionJSON, _ := json.Marshal(&model.Reaction{
		UserId: "other-user", PostId: "p1", EmojiName: "+1",
	})
	evt := newWebSocketEvent(model.WebsocketEventReactionAdded, "ch1", map[string]any{
		"reaction": string(reactionJSON),
	})

	mc.handleReactionAdded(evt)

	events := mock.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event queued, got %d", len(events))
	}
	if events[0].GetType() != bridgev2.RemoteEventReaction {
		t.Errorf("event type: got %v, want RemoteEventReaction", events[0].GetType())
	}
}

func TestHandleReactionRemoved_EchoPrevention(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	reactionJSON, _ := json.Marshal(&model.Reaction{
		UserId: "my-user-id", PostId: "p1", EmojiName: "heart",
	})
	evt := newWebSocketEvent(model.WebsocketEventReactionRemoved, "ch1", map[string]any{
		"reaction": string(reactionJSON),
	})
	mc.rememberEcho(echoKindReactionRm, "p1:heart:my-user-id")

	mc.handleReactionRemoved(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (bridge echo prevention), got %d", len(mock.Events()))
	}
}

func TestHandleReactionRemoved_PassesEchoChecks(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	reactionJSON, _ := json.Marshal(&model.Reaction{
		UserId: "other-user", PostId: "p1", EmojiName: "heart",
	})
	evt := newWebSocketEvent(model.WebsocketEventReactionRemoved, "ch1", map[string]any{
		"reaction": string(reactionJSON),
	})

	mc.handleReactionRemoved(evt)

	events := mock.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event queued, got %d", len(events))
	}
	if events[0].GetType() != bridgev2.RemoteEventReactionRemove {
		t.Errorf("event type: got %v, want RemoteEventReactionRemove", events[0].GetType())
	}
}

// ---------------------------------------------------------------------------
// handleTyping tests
// ---------------------------------------------------------------------------

func TestHandleTyping_OwnUser(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	evt := newWebSocketEvent(model.WebsocketEventTyping, "ch1", map[string]any{
		"user_id": "my-user-id",
	})

	mc.handleTyping(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (own user), got %d", len(mock.Events()))
	}
}

func TestHandleTyping_MissingUserID(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	evt := newWebSocketEvent(model.WebsocketEventTyping, "ch1", map[string]any{})

	mc.handleTyping(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (missing user_id), got %d", len(mock.Events()))
	}
}

func TestHandleTyping_PassesEchoChecks(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	evt := newWebSocketEvent(model.WebsocketEventTyping, "ch1", map[string]any{
		"user_id": "other-user",
	})

	mc.handleTyping(evt)

	events := mock.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event queued, got %d", len(events))
	}
	if events[0].GetType() != bridgev2.RemoteEventTyping {
		t.Errorf("event type: got %v, want RemoteEventTyping", events[0].GetType())
	}
}

func TestHandleTyping_ConfigTimeout(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mc.connector.Config.TypingTimeout = 15
	mock := testMock(mc)
	evt := newWebSocketEvent(model.WebsocketEventTyping, "ch1", map[string]any{
		"user_id": "other-user",
	})

	mc.handleTyping(evt)

	events := mock.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event queued, got %d", len(events))
	}
	typing, ok := events[0].(*simplevent.Typing)
	if !ok {
		t.Fatalf("expected *simplevent.Typing, got %T", events[0])
	}
	if typing.Timeout != 15*time.Second {
		t.Errorf("timeout: got %v, want %v", typing.Timeout, 15*time.Second)
	}
}

func TestHandleTyping_DefaultTimeout(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	// TypingTimeout is 0 (default zero value), should fall back to 5 seconds.
	mock := testMock(mc)
	evt := newWebSocketEvent(model.WebsocketEventTyping, "ch1", map[string]any{
		"user_id": "other-user",
	})

	mc.handleTyping(evt)

	events := mock.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event queued, got %d", len(events))
	}
	typing, ok := events[0].(*simplevent.Typing)
	if !ok {
		t.Fatalf("expected *simplevent.Typing, got %T", events[0])
	}
	if typing.Timeout != 5*time.Second {
		t.Errorf("timeout: got %v, want %v (default)", typing.Timeout, 5*time.Second)
	}
}

// ---------------------------------------------------------------------------
// handleChannelViewed tests
// ---------------------------------------------------------------------------

func TestHandleChannelViewed_MissingData(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	evt := newWebSocketEvent(kchatEventChannelViewed, "ch1", map[string]any{})

	mc.handleChannelViewed(evt)

	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events (missing channel_id), got %d", len(mock.Events()))
	}
}

func TestHandleChannelViewed_PassesChecks(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	evt := newWebSocketEvent(kchatEventChannelViewed, "ch1", map[string]any{
		"channel_id": "ch1",
	})

	mc.handleChannelViewed(evt)

	events := mock.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event queued, got %d", len(events))
	}
	if events[0].GetType() != bridgev2.RemoteEventReadReceipt {
		t.Errorf("event type: got %v, want RemoteEventReadReceipt", events[0].GetType())
	}
}

// ---------------------------------------------------------------------------
// Parse function unit tests
// ---------------------------------------------------------------------------

func TestParsePostedEvent_Valid(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "other-user", ChannelId: "ch1", Message: "hello",
	})
	evt := newWebSocketEvent(model.WebsocketEventPosted, "ch1", map[string]any{
		"post":        string(postJSON),
		"sender_name": "@normaluser",
	})

	post, err := mc.parsePostedEvent(evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if post == nil {
		t.Fatal("expected non-nil post")
	}
	if post.Id != "p1" {
		t.Errorf("post ID: got %q, want %q", post.Id, "p1")
	}
}

func TestParsePostedEvent_MissingData(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	evt := newWebSocketEvent(model.WebsocketEventPosted, "ch1", map[string]any{})

	post, err := mc.parsePostedEvent(evt)
	if err == nil {
		t.Fatal("expected error for missing data")
	}
	if post != nil {
		t.Error("expected nil post on error")
	}
}

func TestParsePostedEvent_InvalidJSON(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	evt := newWebSocketEvent(model.WebsocketEventPosted, "ch1", map[string]any{
		"post": "bad{json",
	})

	post, err := mc.parsePostedEvent(evt)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if post != nil {
		t.Error("expected nil post on error")
	}
}

func TestParsePostEditedEvent_EchoPrevention_Puppet(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mc.connector.Puppets[id.UserID("@puppet:example.com")] = &PuppetClient{
		MXID:   id.UserID("@puppet:example.com"),
		UserID: "puppet-mm-id",
	}

	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "puppet-mm-id", ChannelId: "ch1", Message: "edited",
	})
	evt := newWebSocketEvent(model.WebsocketEventPostEdited, "ch1", map[string]any{
		"post": string(postJSON),
	})

	post, err := mc.parsePostEditedEvent(evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if post != nil {
		t.Error("expected nil post (puppet echo prevention)")
	}
}

func TestParsePostEditedEvent_EchoPrevention_BridgeUsername(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "other-user", ChannelId: "ch1", Message: "edited",
	})
	evt := newWebSocketEvent(model.WebsocketEventPostEdited, "ch1", map[string]any{
		"post":        string(postJSON),
		"sender_name": "@kchat_ghost",
	})

	post, err := mc.parsePostEditedEvent(evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if post != nil {
		t.Error("expected nil post (bridge username echo prevention)")
	}
}

func TestParsePostDeletedEvent_EchoPrevention_Puppet(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mc.connector.Puppets[id.UserID("@puppet:example.com")] = &PuppetClient{
		MXID:   id.UserID("@puppet:example.com"),
		UserID: "puppet-mm-id",
	}

	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "puppet-mm-id", ChannelId: "ch1", Message: "deleted",
	})
	evt := newWebSocketEvent(model.WebsocketEventPostDeleted, "ch1", map[string]any{
		"post": string(postJSON),
	})

	post, err := mc.parsePostDeletedEvent(evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if post != nil {
		t.Error("expected nil post (puppet echo prevention)")
	}
}

func TestParsePostDeletedEvent_EchoPrevention_BridgeUsername(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "other-user", ChannelId: "ch1", Message: "deleted",
	})
	evt := newWebSocketEvent(model.WebsocketEventPostDeleted, "ch1", map[string]any{
		"post":        string(postJSON),
		"sender_name": "@kchat_ghost",
	})

	post, err := mc.parsePostDeletedEvent(evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if post != nil {
		t.Error("expected nil post (bridge username echo prevention)")
	}
}

func TestParseReactionEvent_Valid(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	reactionJSON, _ := json.Marshal(&model.Reaction{
		UserId: "other-user", PostId: "p1", EmojiName: "+1",
	})
	evt := newWebSocketEvent(model.WebsocketEventReactionAdded, "ch1", map[string]any{
		"reaction": string(reactionJSON),
	})

	reaction, err := mc.parseReactionEvent(evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reaction == nil {
		t.Fatal("expected non-nil reaction")
	}
	if reaction.EmojiName != "+1" {
		t.Errorf("emoji: got %q, want %q", reaction.EmojiName, "+1")
	}
}

func TestParseReactionEvent_EchoPrevention(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mc.rememberEcho(echoKindReaction, "p1:+1:my-user-id")
	reactionJSON, _ := json.Marshal(&model.Reaction{
		UserId: "my-user-id", PostId: "p1", EmojiName: "+1",
	})
	evt := newWebSocketEvent(model.WebsocketEventReactionAdded, "ch1", map[string]any{
		"reaction": string(reactionJSON),
	})

	reaction, err := mc.parseReactionEvent(evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reaction != nil {
		t.Error("expected nil reaction (bridge echo prevention)")
	}
}

func TestParseReactionEvent_MissingData(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	evt := newWebSocketEvent(model.WebsocketEventReactionAdded, "ch1", map[string]any{})

	reaction, err := mc.parseReactionEvent(evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reaction != nil {
		t.Error("expected nil reaction (missing data)")
	}
}

func TestParseTypingEvent_Valid(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	evt := newWebSocketEvent(model.WebsocketEventTyping, "ch1", map[string]any{
		"user_id": "other-user",
	})

	userID, channelID, ok := mc.parseTypingEvent(evt)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if userID != "other-user" {
		t.Errorf("userID: got %q, want %q", userID, "other-user")
	}
	if channelID != "ch1" {
		t.Errorf("channelID: got %q, want %q", channelID, "ch1")
	}
}

func TestParseTypingEvent_OwnUser(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	evt := newWebSocketEvent(model.WebsocketEventTyping, "ch1", map[string]any{
		"user_id": "my-user-id",
	})

	_, _, ok := mc.parseTypingEvent(evt)
	if ok {
		t.Error("expected ok=false for own user")
	}
}

func TestParseTypingEvent_MissingUserID(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	evt := newWebSocketEvent(model.WebsocketEventTyping, "ch1", map[string]any{})

	_, _, ok := mc.parseTypingEvent(evt)
	if ok {
		t.Error("expected ok=false for missing user_id")
	}
}

func TestParseChannelViewedEvent_Valid(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	evt := newWebSocketEvent(kchatEventChannelViewed, "ch1", map[string]any{
		"channel_id": "ch1",
	})

	channelID, ok := mc.parseChannelViewedEvent(evt)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if channelID != "ch1" {
		t.Errorf("channelID: got %q, want %q", channelID, "ch1")
	}
}

func TestParseChannelViewedEvent_MissingData(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	evt := newWebSocketEvent(kchatEventChannelViewed, "ch1", map[string]any{})

	_, ok := mc.parseChannelViewedEvent(evt)
	if ok {
		t.Error("expected ok=false for missing channel_id")
	}
}

// ---------------------------------------------------------------------------
// isBridgeUsername edge cases (complements TestIsBridgeUsername in hotreload_test.go)
// ---------------------------------------------------------------------------

func TestIsBridgeUsername_EdgeCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		username  string
		botPrefix string
		want      bool
	}{
		// Edge cases: partial matches that should NOT trigger.
		{"partial kchat", "mattermos", "", false},
		{"kchat without underscore", "kchat", "", false},
		{"kchat dash but not bridge", "kchat-user", "", false},
		// Empty inputs.
		{"empty username", "", "", false},
		{"empty username with prefix", "", "bridge_", false},
		// Ghost user with only the prefix.
		{"ghost user underscore only", "kchat_", "", true},
		// Custom prefix: the prefix itself as username.
		{"custom prefix only", "bridge_", "bridge_", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isBridgeUsername(tt.username, tt.botPrefix)
			if got != tt.want {
				t.Errorf("isBridgeUsername(%q, %q) = %v, want %v", tt.username, tt.botPrefix, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parsePostedEvent edge cases
// ---------------------------------------------------------------------------

func TestParsePostedEvent_ExplicitDefaultType(t *testing.T) {
	t.Parallel()
	// Posts with Type == PostTypeDefault (empty string equivalent) should pass through.
	mc := newFullTestClient("http://localhost")
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "other-user", ChannelId: "ch1",
		Message: "hello", Type: model.PostTypeDefault,
	})
	evt := newWebSocketEvent(model.WebsocketEventPosted, "ch1", map[string]any{
		"post":        string(postJSON),
		"sender_name": "@normaluser",
	})

	post, err := mc.parsePostedEvent(evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if post == nil {
		t.Fatal("PostTypeDefault posts should pass through, got nil")
	}
}

func TestParsePostedEvent_EmptyTypePassesThrough(t *testing.T) {
	t.Parallel()
	// Posts with Type == "" (zero value) should pass through.
	mc := newFullTestClient("http://localhost")
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "other-user", ChannelId: "ch1",
		Message: "hello",
		// Type is deliberately left empty (zero value)
	})
	evt := newWebSocketEvent(model.WebsocketEventPosted, "ch1", map[string]any{
		"post":        string(postJSON),
		"sender_name": "@normaluser",
	})

	post, err := mc.parsePostedEvent(evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if post == nil {
		t.Fatal("empty-Type posts should pass through, got nil")
	}
}

func TestParsePostedEvent_SenderNameWithoutAtPrefix(t *testing.T) {
	t.Parallel()
	// sender_name without @ prefix should still be checked after TrimPrefix.
	mc := newFullTestClient("http://localhost")
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "other-user", ChannelId: "ch1",
		Message: "hello",
	})
	evt := newWebSocketEvent(model.WebsocketEventPosted, "ch1", map[string]any{
		"post":        string(postJSON),
		"sender_name": "kchat_ghost", // no @ prefix
	})

	post, err := mc.parsePostedEvent(evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if post != nil {
		t.Error("kchat_ghost without @ should still be filtered")
	}
}

func TestParsePostedEvent_EmptySenderName(t *testing.T) {
	t.Parallel()
	// When sender_name is missing/empty, bridge username check should be skipped
	// and post should pass through (other checks still apply).
	mc := newFullTestClient("http://localhost")
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "other-user", ChannelId: "ch1",
		Message: "hello",
	})
	evt := newWebSocketEvent(model.WebsocketEventPosted, "ch1", map[string]any{
		"post": string(postJSON),
		// sender_name deliberately absent
	})

	post, err := mc.parsePostedEvent(evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if post == nil {
		t.Fatal("post with no sender_name should pass through if other checks pass")
	}
}

func TestParsePostedEvent_NonStringPostData(t *testing.T) {
	t.Parallel()
	// If "post" field is not a string (e.g., int, nil), should return error.
	mc := newFullTestClient("http://localhost")

	tests := []struct {
		name string
		data map[string]any
	}{
		{"int value", map[string]any{"post": 42}},
		{"nil value", map[string]any{"post": nil}},
		{"bool value", map[string]any{"post": true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			evt := newWebSocketEvent(model.WebsocketEventPosted, "ch1", tt.data)
			post, err := mc.parsePostedEvent(evt)
			if err == nil {
				t.Error("expected error for non-string post data")
			}
			if post != nil {
				t.Error("expected nil post on error")
			}
		})
	}
}

func TestParsePostedEvent_EmptyUserID(t *testing.T) {
	t.Parallel()
	// A post with empty UserId should pass echo prevention (since "" != m.userID).
	// This documents current behavior — whether this is correct depends on whether
	// kChat ever sends posts with empty user IDs.
	mc := newFullTestClient("http://localhost")
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "", ChannelId: "ch1",
		Message: "hello",
	})
	evt := newWebSocketEvent(model.WebsocketEventPosted, "ch1", map[string]any{
		"post":        string(postJSON),
		"sender_name": "@normaluser",
	})

	post, err := mc.parsePostedEvent(evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Current behavior: empty UserId passes through because "" != "my-user-id".
	// This is acceptable since kChat should always include a user ID.
	if post == nil {
		t.Fatal("empty UserId post passed through echo prevention, which is the current behavior")
	}
}

func TestParsePostedEvent_BotPrefixEchoPrevention(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mc.connector.Config.BotPrefix = "relay_"

	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "other-user", ChannelId: "ch1",
		Message: "hello",
	})
	evt := newWebSocketEvent(model.WebsocketEventPosted, "ch1", map[string]any{
		"post":        string(postJSON),
		"sender_name": "@relay_bot",
	})

	post, err := mc.parsePostedEvent(evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if post != nil {
		t.Error("posts from users matching BotPrefix should be filtered")
	}
}

func TestParsePostedEvent_MultiplePuppets(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mc.connector.Puppets[id.UserID("@puppet1:example.com")] = &PuppetClient{
		MXID: id.UserID("@puppet1:example.com"), UserID: "puppet-id-1",
	}
	mc.connector.Puppets[id.UserID("@puppet2:example.com")] = &PuppetClient{
		MXID: id.UserID("@puppet2:example.com"), UserID: "puppet-id-2",
	}

	// Test each puppet ID is filtered.
	for _, puppetID := range []string{"puppet-id-1", "puppet-id-2"} {
		postJSON, _ := json.Marshal(&model.Post{
			Id: "p1", UserId: puppetID, ChannelId: "ch1", Message: "hello",
		})
		evt := newWebSocketEvent(model.WebsocketEventPosted, "ch1", map[string]any{
			"post": string(postJSON),
		})
		post, err := mc.parsePostedEvent(evt)
		if err != nil {
			t.Fatalf("unexpected error for puppet %s: %v", puppetID, err)
		}
		if post != nil {
			t.Errorf("post from puppet %s should be filtered", puppetID)
		}
	}
}

// ---------------------------------------------------------------------------
// parseReactionEvent edge cases — documents missing echo prevention layers
// ---------------------------------------------------------------------------

func TestParseReactionEvent_BridgeUsername_Filtered(t *testing.T) {
	t.Parallel()
	// Reactions from senders matching bridge username patterns should be
	// filtered (echo prevention layer 5: configurable username prefix check).
	tests := []struct {
		name       string
		senderName string
		botPrefix  string
	}{
		{
			name:       "hardcoded kchat_ghost",
			senderName: "@kchat_ghost",
			botPrefix:  "",
		},
		{
			name:       "hardcoded kchat-bridge",
			senderName: "@kchat-bridge",
			botPrefix:  "",
		},
		{
			name:       "configurable bot prefix",
			senderName: "@relay_bot",
			botPrefix:  "relay_",
		},
		{
			name:       "without @ prefix",
			senderName: "kchat_someone",
			botPrefix:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mc := newFullTestClient("http://localhost")
			mc.connector.Config.BotPrefix = tt.botPrefix

			reactionJSON, _ := json.Marshal(&model.Reaction{
				UserId: "other-user", PostId: "p1", EmojiName: "heart",
			})
			evt := newWebSocketEvent(model.WebsocketEventReactionAdded, "ch1", map[string]any{
				"reaction":    string(reactionJSON),
				"sender_name": tt.senderName,
			})

			reaction, err := mc.parseReactionEvent(evt)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if reaction != nil {
				t.Errorf("expected reaction from bridge username %q to be filtered, but it was not", tt.senderName)
			}
		})
	}
}

func TestParseReactionEvent_PuppetUser_Filtered(t *testing.T) {
	t.Parallel()
	// Reactions from puppet bot users should be filtered (echo prevention layer 1).
	mc := newFullTestClient("http://localhost")
	mc.connector.Puppets[id.UserID("@puppet:example.com")] = &PuppetClient{
		MXID: id.UserID("@puppet:example.com"), UserID: "puppet-mm-id",
	}

	reactionJSON, _ := json.Marshal(&model.Reaction{
		UserId: "puppet-mm-id", PostId: "p1", EmojiName: "+1",
	})
	evt := newWebSocketEvent(model.WebsocketEventReactionAdded, "ch1", map[string]any{
		"reaction": string(reactionJSON),
	})

	reaction, err := mc.parseReactionEvent(evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reaction != nil {
		t.Error("expected puppet reaction to be filtered, but it was not")
	}
}

func TestParseReactionEvent_InvalidJSON(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	evt := newWebSocketEvent(model.WebsocketEventReactionAdded, "ch1", map[string]any{
		"reaction": "not{valid{json",
	})

	reaction, err := mc.parseReactionEvent(evt)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
	if reaction != nil {
		t.Error("expected nil reaction on error")
	}
}

func TestParseReactionEvent_NonStringData(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	evt := newWebSocketEvent(model.WebsocketEventReactionAdded, "ch1", map[string]any{
		"reaction": 42,
	})

	reaction, err := mc.parseReactionEvent(evt)
	if err != nil {
		t.Fatalf("non-string data should return (nil, nil) not error: %v", err)
	}
	if reaction != nil {
		t.Error("expected nil reaction for non-string data")
	}
}

// ---------------------------------------------------------------------------
// parseChannelViewedEvent edge cases
// ---------------------------------------------------------------------------

func TestParseChannelViewedEvent_EmptyChannelID(t *testing.T) {
	t.Parallel()
	// If channel_id is present but empty string, it passes the type assertion.
	// This documents current behavior — an empty channel ID will be queued.
	mc := newFullTestClient("http://localhost")
	evt := newWebSocketEvent(kchatEventChannelViewed, "ch1", map[string]any{
		"channel_id": "",
	})

	channelID, ok := mc.parseChannelViewedEvent(evt)
	if !ok {
		t.Fatal("expected ok=true (empty string passes type assertion)")
	}
	// Current behavior: empty string is accepted. This could cause issues downstream
	// when creating a portal key with empty ID.
	if channelID != "" {
		t.Errorf("expected empty channelID, got %q", channelID)
	}
}

func TestParseChannelViewedEvent_NonStringChannelID(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	evt := newWebSocketEvent(kchatEventChannelViewed, "ch1", map[string]any{
		"channel_id": 12345,
	})

	_, ok := mc.parseChannelViewedEvent(evt)
	if ok {
		t.Error("expected ok=false for non-string channel_id")
	}
}

// ---------------------------------------------------------------------------
// parseTypingEvent edge cases
// ---------------------------------------------------------------------------

func TestParseTypingEvent_NonStringUserID(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	evt := newWebSocketEvent(model.WebsocketEventTyping, "ch1", map[string]any{
		"user_id": 12345,
	})

	_, _, ok := mc.parseTypingEvent(evt)
	if ok {
		t.Error("expected ok=false for non-string user_id")
	}
}

func TestParseTypingEvent_EmptyUserID(t *testing.T) {
	t.Parallel()
	// Empty string user_id passes the type assertion but should still not match
	// own user ID, so it proceeds. This documents the behavior.
	mc := newFullTestClient("http://localhost")
	evt := newWebSocketEvent(model.WebsocketEventTyping, "ch1", map[string]any{
		"user_id": "",
	})

	userID, _, ok := mc.parseTypingEvent(evt)
	if !ok {
		t.Fatal("empty string user_id passes type assertion, so ok should be true")
	}
	if userID != "" {
		t.Errorf("expected empty userID, got %q", userID)
	}
}

// ---------------------------------------------------------------------------
// Asymmetric error handling between parse functions
// ---------------------------------------------------------------------------

func TestParsePostEditedEvent_MissingData_SilentSkip(t *testing.T) {
	t.Parallel()
	// Unlike parsePostedEvent which returns an error for missing data,
	// parsePostEditedEvent returns (nil, nil) — silent skip.
	// This documents the asymmetry.
	mc := newFullTestClient("http://localhost")
	evt := newWebSocketEvent(model.WebsocketEventPostEdited, "ch1", map[string]any{})

	post, err := mc.parsePostEditedEvent(evt)
	if err != nil {
		t.Fatalf("parsePostEditedEvent should silently skip missing data, got error: %v", err)
	}
	if post != nil {
		t.Error("expected nil post for missing data")
	}
}

func TestParsePostDeletedEvent_MissingData_SilentSkip(t *testing.T) {
	t.Parallel()
	// Same asymmetry: parsePostDeletedEvent silently skips missing data.
	mc := newFullTestClient("http://localhost")
	evt := newWebSocketEvent(model.WebsocketEventPostDeleted, "ch1", map[string]any{})

	post, err := mc.parsePostDeletedEvent(evt)
	if err != nil {
		t.Fatalf("parsePostDeletedEvent should silently skip missing data, got error: %v", err)
	}
	if post != nil {
		t.Error("expected nil post for missing data")
	}
}

// ---------------------------------------------------------------------------
// reactionToEmoji edge cases
// ---------------------------------------------------------------------------

func TestReactionToEmoji_EmptyName(t *testing.T) {
	t.Parallel()
	// Empty name produces "::" which is probably not useful.
	// This documents current behavior.
	got := reactionToEmoji("")
	if got != "::" {
		t.Errorf("reactionToEmoji(\"\") = %q, want %q", got, "::")
	}
}

func TestReactionToEmoji_CustomEmoji(t *testing.T) {
	t.Parallel()
	// Unknown emoji names get wrapped in colons.
	got := reactionToEmoji("my_custom_emoji")
	if got != ":my_custom_emoji:" {
		t.Errorf("reactionToEmoji(custom): got %q, want %q", got, ":my_custom_emoji:")
	}
}

// ---------------------------------------------------------------------------
// Handler integration: verify queued event metadata
// ---------------------------------------------------------------------------

func TestHandlePosted_QueuedEventMetadata(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "sender-uid", ChannelId: "target-ch",
		Message: "hello", CreateAt: 1700000000000,
	})
	evt := newWebSocketEvent(model.WebsocketEventPosted, "target-ch", map[string]any{
		"post":        string(postJSON),
		"sender_name": "@normaluser",
	})

	mc.handlePosted(evt)

	events := mock.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	if e.GetType() != bridgev2.RemoteEventMessage {
		t.Errorf("type: got %v, want RemoteEventMessage", e.GetType())
	}
	if string(e.GetPortalKey().ID) != "target-ch" {
		t.Errorf("portal key: got %q, want %q", e.GetPortalKey().ID, "target-ch")
	}
	if string(e.GetSender().Sender) != "sender-uid" {
		t.Errorf("sender: got %q, want %q", e.GetSender().Sender, "sender-uid")
	}
}

func TestHandleReactionAdded_QueuedEventMetadata(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)
	reactionJSON, _ := json.Marshal(&model.Reaction{
		UserId: "reactor-uid", PostId: "target-post", EmojiName: "fire",
	})
	evt := newWebSocketEvent(model.WebsocketEventReactionAdded, "target-ch", map[string]any{
		"reaction": string(reactionJSON),
	})

	mc.handleReactionAdded(evt)

	events := mock.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	if e.GetType() != bridgev2.RemoteEventReaction {
		t.Errorf("type: got %v, want RemoteEventReaction", e.GetType())
	}
	if string(e.GetSender().Sender) != "reactor-uid" {
		t.Errorf("sender: got %q, want %q", e.GetSender().Sender, "reactor-uid")
	}
}

// ---------------------------------------------------------------------------
// convertFileToMatrix tests
// ---------------------------------------------------------------------------

func TestConvertPostToMatrix_WithFiles(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	fake.Files["f1"] = &model.FileInfo{
		Id: "f1", Name: "photo.jpg", MimeType: "image/jpeg", Size: 1024,
	}

	mc := newFullTestClient(fake.Server.URL)
	post := &model.Post{
		Id:        "post-with-file",
		Message:   "Check this out",
		ChannelId: "ch1",
		UserId:    "user1",
		FileIds:   model.StringArray{"f1"},
	}

	msg := mc.convertPostToMatrix(post)

	// Should have 2 parts: text + file.
	if len(msg.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(msg.Parts))
	}
	if msg.Parts[0].Content.MsgType != event.MsgText {
		t.Errorf("part 0 should be text, got %v", msg.Parts[0].Content.MsgType)
	}
	if msg.Parts[1].Content.MsgType != event.MsgImage {
		t.Errorf("part 1 should be image, got %v", msg.Parts[1].Content.MsgType)
	}
}

func TestConvertPostToMatrix_OnlyFiles(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	fake.Files["f2"] = &model.FileInfo{
		Id: "f2", Name: "doc.pdf", MimeType: "application/pdf", Size: 5000,
	}

	mc := newFullTestClient(fake.Server.URL)
	post := &model.Post{
		Id:        "post-file-only",
		Message:   "",
		ChannelId: "ch1",
		UserId:    "user1",
		FileIds:   model.StringArray{"f2"},
	}

	msg := mc.convertPostToMatrix(post)

	// Should have 1 part: file only (no text since message is empty).
	if len(msg.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(msg.Parts))
	}
	if msg.Parts[0].Content.MsgType != event.MsgFile {
		t.Errorf("part should be file, got %v", msg.Parts[0].Content.MsgType)
	}
}

func TestConvertFileToMatrix_Image(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	fake.Files["f1"] = &model.FileInfo{
		Id: "f1", Name: "photo.jpg", MimeType: "image/jpeg", Size: 1024,
	}

	mc := newFullTestClient(fake.Server.URL)
	result := mc.convertFileToMatrix("f1", 1)

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Content.MsgType != event.MsgImage {
		t.Errorf("MsgType: got %v, want MsgImage", result.Content.MsgType)
	}
	if result.Content.Body != "photo.jpg" {
		t.Errorf("Body: got %q, want %q", result.Content.Body, "photo.jpg")
	}
	if result.Content.Info == nil {
		t.Fatal("expected non-nil Info")
	}
	if result.Content.Info.MimeType != "image/jpeg" {
		t.Errorf("MimeType: got %q, want %q", result.Content.Info.MimeType, "image/jpeg")
	}
	if result.Content.Info.Size != 1024 {
		t.Errorf("Size: got %d, want %d", result.Content.Info.Size, 1024)
	}
}

func TestConvertFileToMatrix_Video(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	fake.Files["f2"] = &model.FileInfo{
		Id: "f2", Name: "clip.mp4", MimeType: "video/mp4", Size: 5000,
	}

	mc := newFullTestClient(fake.Server.URL)
	result := mc.convertFileToMatrix("f2", 1)

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Content.MsgType != event.MsgVideo {
		t.Errorf("MsgType: got %v, want MsgVideo", result.Content.MsgType)
	}
}

func TestConvertFileToMatrix_Audio(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	fake.Files["f3"] = &model.FileInfo{
		Id: "f3", Name: "song.mp3", MimeType: "audio/mpeg", Size: 3000,
	}

	mc := newFullTestClient(fake.Server.URL)
	result := mc.convertFileToMatrix("f3", 1)

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Content.MsgType != event.MsgAudio {
		t.Errorf("MsgType: got %v, want MsgAudio", result.Content.MsgType)
	}
}

func TestConvertFileToMatrix_GenericFile(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	fake.Files["f4"] = &model.FileInfo{
		Id: "f4", Name: "document.pdf", MimeType: "application/pdf", Size: 8000,
	}

	mc := newFullTestClient(fake.Server.URL)
	result := mc.convertFileToMatrix("f4", 1)

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Content.MsgType != event.MsgFile {
		t.Errorf("MsgType: got %v, want MsgFile", result.Content.MsgType)
	}
}

func TestConvertFileToMatrix_APIError(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	fake.FailEndpoints["/files/"] = true

	mc := newFullTestClient(fake.Server.URL)
	result := mc.convertFileToMatrix("f1", 1)

	if result != nil {
		t.Errorf("expected nil result on API error, got %+v", result)
	}
}

// TestReactionToEmoji_EmojiToReaction_Roundtrip verifies that known emoji
// names roundtrip through reactionToEmoji -> emojiToReaction.
// Note: Fuzz tests for these functions are in fuzz_test.go.
func TestReactionToEmoji_EmojiToReaction_Roundtrip(t *testing.T) {
	t.Parallel()
	knownNames := []string{
		"+1", "-1", "heart", "smile", "fire", "rocket", "eyes",
		"tada", "100", "white_check_mark", "x", "star", "pray",
		"thinking", "wave", "clap", "laughing", "warning",
	}

	for _, name := range knownNames {
		emoji := reactionToEmoji(name)
		got := emojiToReaction(emoji)
		if got != name {
			t.Errorf("roundtrip failed for %q: reactionToEmoji=%q, emojiToReaction=%q", name, emoji, got)
		}
	}
}

// ---------------------------------------------------------------------------
// senderFor / double puppet SenderLogin tests
// ---------------------------------------------------------------------------

func TestSenderFor_NoDoublePuppet(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")

	sender := mc.senderFor("some-user-id")

	if string(sender.Sender) != "some-user-id" {
		t.Errorf("Sender: got %q, want %q", sender.Sender, "some-user-id")
	}
	if sender.SenderLogin != "" {
		t.Errorf("SenderLogin should be empty, got %q", sender.SenderLogin)
	}
}

func TestSenderFor_WithDoublePuppet(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")

	// Register a double puppet mapping.
	mc.connector.dpLoginsMu.Lock()
	mc.connector.dpLogins["ceo-mm-id"] = MakeUserLoginID("ceo-mm-id")
	mc.connector.dpLoginsMu.Unlock()

	sender := mc.senderFor("ceo-mm-id")

	if string(sender.Sender) != "ceo-mm-id" {
		t.Errorf("Sender: got %q, want %q", sender.Sender, "ceo-mm-id")
	}
	if string(sender.SenderLogin) != "ceo-mm-id" {
		t.Errorf("SenderLogin: got %q, want %q", sender.SenderLogin, "ceo-mm-id")
	}
}

func TestSenderFor_UnregisteredUserNoSenderLogin(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")

	// Register one user but query a different one.
	mc.connector.dpLoginsMu.Lock()
	mc.connector.dpLogins["alice-mm-id"] = MakeUserLoginID("alice-mm-id")
	mc.connector.dpLoginsMu.Unlock()

	sender := mc.senderFor("bob-mm-id")

	if sender.SenderLogin != "" {
		t.Errorf("SenderLogin should be empty for unregistered user, got %q", sender.SenderLogin)
	}
}

func TestHandlePosted_SenderLoginSet(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)

	// Register double puppet for the poster.
	mc.connector.dpLoginsMu.Lock()
	mc.connector.dpLogins["dp-poster"] = MakeUserLoginID("dp-poster")
	mc.connector.dpLoginsMu.Unlock()

	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "dp-poster", ChannelId: "ch1", Message: "hello from CEO",
	})
	evt := newWebSocketEvent(model.WebsocketEventPosted, "ch1", map[string]any{
		"post":        string(postJSON),
		"sender_name": "@ceo",
	})

	mc.handlePosted(evt)

	events := mock.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	sender := events[0].GetSender()
	if string(sender.Sender) != "dp-poster" {
		t.Errorf("Sender: got %q, want %q", sender.Sender, "dp-poster")
	}
	if string(sender.SenderLogin) != "dp-poster" {
		t.Errorf("SenderLogin: got %q, want %q", sender.SenderLogin, "dp-poster")
	}
}

func TestHandlePosted_NoSenderLoginForRegularUser(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)

	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "regular-user", ChannelId: "ch1", Message: "hello",
	})
	evt := newWebSocketEvent(model.WebsocketEventPosted, "ch1", map[string]any{
		"post":        string(postJSON),
		"sender_name": "@regular",
	})

	mc.handlePosted(evt)

	events := mock.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	sender := events[0].GetSender()
	if sender.SenderLogin != "" {
		t.Errorf("SenderLogin should be empty for regular user, got %q", sender.SenderLogin)
	}
}

func TestHandleReactionAdded_SenderLoginSet(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)

	mc.connector.dpLoginsMu.Lock()
	mc.connector.dpLogins["dp-reactor"] = MakeUserLoginID("dp-reactor")
	mc.connector.dpLoginsMu.Unlock()

	reactionJSON, _ := json.Marshal(&model.Reaction{
		UserId: "dp-reactor", PostId: "target-post", EmojiName: "heart",
	})
	evt := newWebSocketEvent(model.WebsocketEventReactionAdded, "ch1", map[string]any{
		"reaction": string(reactionJSON),
	})

	mc.handleReactionAdded(evt)

	events := mock.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if string(events[0].GetSender().SenderLogin) != "dp-reactor" {
		t.Errorf("SenderLogin: got %q, want %q", events[0].GetSender().SenderLogin, "dp-reactor")
	}
}

func TestHandlePostEdited_SenderLoginSet(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)

	mc.connector.dpLoginsMu.Lock()
	mc.connector.dpLogins["dp-editor"] = MakeUserLoginID("dp-editor")
	mc.connector.dpLoginsMu.Unlock()

	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "dp-editor", ChannelId: "ch1", Message: "edited",
		EditAt: time.Now().UnixMilli(),
	})
	evt := newWebSocketEvent(model.WebsocketEventPostEdited, "ch1", map[string]any{
		"post":        string(postJSON),
		"sender_name": "@editor",
	})

	mc.handlePostEdited(evt)

	events := mock.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if string(events[0].GetSender().SenderLogin) != "dp-editor" {
		t.Errorf("SenderLogin: got %q, want %q", events[0].GetSender().SenderLogin, "dp-editor")
	}
}

func TestHandlePostDeleted_SenderLoginSet(t *testing.T) {
	t.Parallel()
	mc := newFullTestClient("http://localhost")
	mock := testMock(mc)

	mc.connector.dpLoginsMu.Lock()
	mc.connector.dpLogins["dp-deleter"] = MakeUserLoginID("dp-deleter")
	mc.connector.dpLoginsMu.Unlock()

	postJSON, _ := json.Marshal(&model.Post{
		Id: "p1", UserId: "dp-deleter", ChannelId: "ch1",
		DeleteAt: time.Now().UnixMilli(),
	})
	evt := newWebSocketEvent(model.WebsocketEventPostDeleted, "ch1", map[string]any{
		"post":        string(postJSON),
		"sender_name": "@deleter",
	})

	mc.handlePostDeleted(evt)

	events := mock.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if string(events[0].GetSender().SenderLogin) != "dp-deleter" {
		t.Errorf("SenderLogin: got %q, want %q", events[0].GetSender().SenderLogin, "dp-deleter")
	}
}

func TestHandleChannelCreated_AutoJoin(t *testing.T) {
	t.Parallel()
	fm := newFakeMM()
	defer fm.Close()
	fm.Channels["ch-new"] = &model.Channel{Id: "ch-new", Name: "created-channel", DisplayName: "Created Channel", Type: model.ChannelTypeOpen}
	mc := newFullTestClient(fm.Server.URL)
	mock := testMock(mc)

	evt := newWebSocketEvent(model.WebsocketEventChannelCreated, "ch-new", map[string]any{
		"channel_id": "ch-new",
	})

	mc.handleChannelCreated(evt)

	if !fm.CalledPath("/api/v4/channels/ch-new/members") {
		t.Error("expected auto-join POST to channel members")
	}
	events := mock.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 ChatResync event, got %d", len(events))
	}
	if events[0].GetPortalKey().ID != "ch-new" {
		t.Errorf("portal key ID: got %q, want %q", events[0].GetPortalKey().ID, "ch-new")
	}
}

func TestHandleChannelCreated_PrivateChannelSkipped(t *testing.T) {
	t.Parallel()
	fm := newFakeMM()
	defer fm.Close()
	fm.Channels["ch-priv"] = &model.Channel{Id: "ch-priv", Name: "private-channel", Type: model.ChannelTypePrivate}
	mc := newFullTestClient(fm.Server.URL)
	mock := testMock(mc)

	evt := newWebSocketEvent(model.WebsocketEventChannelCreated, "ch-priv", map[string]any{
		"channel_id": "ch-priv",
	})

	mc.handleChannelCreated(evt)

	if fm.CalledPath("/api/v4/channels/ch-priv/members") {
		t.Error("private channel must not be joined")
	}
	if len(mock.Events()) != 0 {
		t.Errorf("expected 0 events for private channel, got %d", len(mock.Events()))
	}
}
