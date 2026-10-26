// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func TestEmojiToReaction_KnownUnicode(t *testing.T) {
	tests := []struct {
		emoji string
		want  string
	}{
		{"\U0001f44d", "+1"},
		{"\U0001f44e", "-1"},
		{"\u2764\ufe0f", "heart"},
		{"\U0001f604", "smile"},
		{"\U0001f606", "laughing"},
		{"\U0001f44b", "wave"},
		{"\U0001f44f", "clap"},
		{"\U0001f525", "fire"},
		{"\U0001f4af", "100"},
		{"\U0001f389", "tada"},
		{"\U0001f440", "eyes"},
		{"\U0001f914", "thinking"},
		{"\u2705", "white_check_mark"},
		{"\u274c", "x"},
		{"\u26a0\ufe0f", "warning"},
		{"\U0001f680", "rocket"},
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

func TestEmojiToReaction_CustomColon(t *testing.T) {
	got := emojiToReaction(":custom_emoji:")
	if got != "custom_emoji" {
		t.Errorf("custom colon emoji: got %q, want %q", got, "custom_emoji")
	}
}

func TestEmojiToReaction_UnknownPassthroughMatrix(t *testing.T) {
	got := emojiToReaction("Z")
	if got != "Z" {
		t.Errorf("unknown passthrough: got %q, want %q", got, "Z")
	}
}

func TestEmojiToReaction_EmptyString(t *testing.T) {
	got := emojiToReaction("")
	if got != "" {
		t.Errorf("empty string: got %q, want empty", got)
	}
}

func TestEmojiToReaction_ColonOnly(t *testing.T) {
	got := emojiToReaction(":")
	if got != ":" {
		t.Errorf("single colon: got %q, want %q", got, ":")
	}
}

func TestEmojiToReaction_TwoColons(t *testing.T) {
	// "::" is len 2, not > 2, so it passes through.
	got := emojiToReaction("::")
	if got != "::" {
		t.Errorf("two colons: got %q, want %q", got, "::")
	}
}

func TestEmojiToReaction_RoundTrip(t *testing.T) {
	// Verify known emoji round-trip: emoji -> name -> emoji.
	knownPairs := []struct {
		name  string
		emoji string
	}{
		{"+1", "\U0001f44d"},
		{"heart", "\u2764\ufe0f"},
		{"fire", "\U0001f525"},
		{"rocket", "\U0001f680"},
	}

	for _, tt := range knownPairs {
		// emoji -> name
		name := emojiToReaction(tt.emoji)
		if name != tt.name {
			t.Errorf("emoji->name for %q: got %q, want %q", tt.name, name, tt.name)
		}
		// name -> emoji
		emoji := reactionToEmoji(tt.name)
		if emoji != tt.emoji {
			t.Errorf("name->emoji for %q: got %q, want %q", tt.name, emoji, tt.emoji)
		}
	}
}

// newPuppetTestClient creates a KChatClient with puppets for testing resolvePostClient.
func newPuppetTestClient(puppets map[id.UserID]*PuppetClient) *KChatClient {
	return &KChatClient{
		connector: &KChatConnector{
			Puppets: puppets,
		},
		client: model.NewAPIv4Client("http://fallback"),
		userID: "default-user-id",
		log:    zerolog.Nop(),
	}
}

func TestResolvePostClient_PuppetViaOrigSender(t *testing.T) {
	puppetClient := model.NewAPIv4Client("http://puppet")
	puppets := map[id.UserID]*PuppetClient{
		"@puppet-bot:localhost": {
			MXID:     "@puppet-bot:localhost",
			Client:   puppetClient,
			UserID:   "puppet-mm-id",
			Username: "puppet-bot",
		},
	}
	mc := newPuppetTestClient(puppets)

	client, userID := mc.resolvePostClient(
		&bridgev2.OrigSender{UserID: "@puppet-bot:localhost"},
		nil,
	)

	if client != puppetClient {
		t.Error("expected puppet client, got default")
	}
	if userID != "puppet-mm-id" {
		t.Errorf("expected puppet-mm-id, got %q", userID)
	}
}

func TestResolvePostClient_PuppetViaEventSender(t *testing.T) {
	puppetClient := model.NewAPIv4Client("http://puppet")
	puppets := map[id.UserID]*PuppetClient{
		"@puppet-bot:localhost": {
			MXID:     "@puppet-bot:localhost",
			Client:   puppetClient,
			UserID:   "puppet-mm-id",
			Username: "puppet-bot",
		},
	}
	mc := newPuppetTestClient(puppets)

	evt := &event.Event{Sender: "@puppet-bot:localhost"}
	client, userID := mc.resolvePostClient(nil, evt)

	if client != puppetClient {
		t.Error("expected puppet client, got default")
	}
	if userID != "puppet-mm-id" {
		t.Errorf("expected puppet-mm-id, got %q", userID)
	}
}

func TestResolvePostClient_FallbackToDefault(t *testing.T) {
	mc := newPuppetTestClient(map[id.UserID]*PuppetClient{})

	client, userID := mc.resolvePostClient(
		&bridgev2.OrigSender{UserID: "@unknown:localhost"},
		&event.Event{Sender: "@unknown:localhost"},
	)

	if client != mc.client {
		t.Error("expected default client, got different")
	}
	if userID != "default-user-id" {
		t.Errorf("expected default-user-id, got %q", userID)
	}
}

func TestResolvePostClient_NilInputs(t *testing.T) {
	mc := newPuppetTestClient(map[id.UserID]*PuppetClient{})

	client, userID := mc.resolvePostClient(nil, nil)

	if client != mc.client {
		t.Error("expected default client")
	}
	if userID != "default-user-id" {
		t.Errorf("expected default-user-id, got %q", userID)
	}
}

func TestResolvePostClient_OrigSenderTakesPrecedence(t *testing.T) {
	puppetA := model.NewAPIv4Client("http://puppet-a")
	puppetB := model.NewAPIv4Client("http://puppet-b")
	puppets := map[id.UserID]*PuppetClient{
		"@bot-a:localhost": {
			MXID:   "@bot-a:localhost",
			Client: puppetA,
			UserID: "mm-a",
		},
		"@bot-b:localhost": {
			MXID:   "@bot-b:localhost",
			Client: puppetB,
			UserID: "mm-b",
		},
	}
	mc := newPuppetTestClient(puppets)

	// OrigSender is bot-a, Event.Sender is bot-b — OrigSender wins.
	client, userID := mc.resolvePostClient(
		&bridgev2.OrigSender{UserID: "@bot-a:localhost"},
		&event.Event{Sender: "@bot-b:localhost"},
	)

	if client != puppetA {
		t.Error("expected puppetA (OrigSender), got different")
	}
	if userID != "mm-a" {
		t.Errorf("expected mm-a, got %q", userID)
	}
}

// ---------------------------------------------------------------------------
// HandleMatrixMessage tests
// ---------------------------------------------------------------------------

func TestHandleMatrixMessage_Text(t *testing.T) {
	fm := newFakeMM()
	defer fm.Close()
	mc := newFullTestClient(fm.Server.URL)

	msg := &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Portal:  makeTestPortal("test-channel"),
			Content: &event.MessageEventContent{MsgType: event.MsgText, Body: "Hello"},
		},
	}

	resp, err := mc.HandleMatrixMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if string(resp.DB.ID) != "created-post-id" {
		t.Errorf("expected MessageID 'created-post-id', got %q", resp.DB.ID)
	}
	if string(resp.DB.SenderID) != "my-user-id" {
		t.Errorf("expected SenderID 'my-user-id', got %q", resp.DB.SenderID)
	}
	if !fm.CalledPath("/api/v4/posts") {
		t.Error("expected /api/v4/posts to be called")
	}
}

func TestHandleMatrixMessage_Emote(t *testing.T) {
	fm := newFakeMM()
	defer fm.Close()
	mc := newFullTestClient(fm.Server.URL)

	msg := &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Portal:  makeTestPortal("test-channel"),
			Content: &event.MessageEventContent{MsgType: event.MsgEmote, Body: "waves"},
		},
	}

	resp, err := mc.HandleMatrixMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	// Verify the post body starts with "/me ".
	calls := fm.Calls()
	found := false
	for _, c := range calls {
		if c.Path == "/api/v4/posts" && strings.Contains(c.Body, "/me ") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected post body to contain '/me ' prefix for emote messages")
	}
}

func TestHandleMatrixMessage_Notice(t *testing.T) {
	fm := newFakeMM()
	defer fm.Close()
	mc := newFullTestClient(fm.Server.URL)

	msg := &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Portal:  makeTestPortal("test-channel"),
			Content: &event.MessageEventContent{MsgType: event.MsgNotice, Body: "Notice message"},
		},
	}

	resp, err := mc.HandleMatrixMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestHandleMatrixMessage_WithReply(t *testing.T) {
	fm := newFakeMM()
	defer fm.Close()
	mc := newFullTestClient(fm.Server.URL)

	msg := &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Portal:  makeTestPortal("test-channel"),
			Content: &event.MessageEventContent{MsgType: event.MsgText, Body: "Reply text"},
		},
		ReplyTo: &database.Message{ID: MakeMessageID("parent-post")},
	}

	resp, err := mc.HandleMatrixMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	// Verify the post body includes the root_id for the reply.
	calls := fm.Calls()
	found := false
	for _, c := range calls {
		if c.Path == "/api/v4/posts" && strings.Contains(c.Body, "parent-post") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected post body to contain 'parent-post' as root_id for reply")
	}
}

func TestHandleMatrixMessage_UnsupportedType(t *testing.T) {
	fm := newFakeMM()
	defer fm.Close()
	mc := newFullTestClient(fm.Server.URL)

	msg := &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Portal:  makeTestPortal("test-channel"),
			Content: &event.MessageEventContent{MsgType: event.MessageType("m.custom"), Body: "custom"},
		},
	}

	_, err := mc.HandleMatrixMessage(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error for unsupported message type")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("expected error containing 'unsupported', got: %v", err)
	}
}

func TestHandleMatrixMessage_APIError(t *testing.T) {
	fm := newFakeMM()
	defer fm.Close()

	fm.FailEndpoints["/api/v4/posts"] = true

	mc := newFullTestClient(fm.Server.URL)

	msg := &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Portal:  makeTestPortal("test-channel"),
			Content: &event.MessageEventContent{MsgType: event.MsgText, Body: "Hello"},
		},
	}

	_, err := mc.HandleMatrixMessage(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error when CreatePost fails, got nil")
	}
	if !strings.Contains(err.Error(), "failed to create post") {
		t.Errorf("expected error about failed post, got: %v", err)
	}
}

func TestHandleMatrixMessage_NotLoggedIn(t *testing.T) {
	mc := newNotLoggedInClient()

	msg := &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Portal:  makeTestPortal("test-channel"),
			Content: &event.MessageEventContent{MsgType: event.MsgText, Body: "Hello"},
		},
	}

	_, err := mc.HandleMatrixMessage(context.Background(), msg)
	if !errors.Is(err, bridgev2.ErrNotLoggedIn) {
		t.Errorf("expected ErrNotLoggedIn, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// HandleMatrixEdit tests
// ---------------------------------------------------------------------------

func TestHandleMatrixEdit_Success(t *testing.T) {
	fm := newFakeMM()
	defer fm.Close()
	mc := newFullTestClient(fm.Server.URL)

	msg := &bridgev2.MatrixEdit{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Portal:  makeTestPortal("test-channel"),
			Content: &event.MessageEventContent{MsgType: event.MsgText, Body: "edited"},
		},
		EditTarget: &database.Message{ID: MakeMessageID("post-to-edit")},
	}

	err := mc.HandleMatrixEdit(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fm.CalledPath("/patch") {
		t.Error("expected /patch endpoint to be called")
	}
}

func TestHandleMatrixEdit_APIError(t *testing.T) {
	fm := newFakeMM()
	defer fm.Close()

	fm.FailEndpoints["/patch"] = true

	mc := newFullTestClient(fm.Server.URL)

	msg := &bridgev2.MatrixEdit{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Portal:  makeTestPortal("test-channel"),
			Content: &event.MessageEventContent{MsgType: event.MsgText, Body: "edited"},
		},
		EditTarget: &database.Message{ID: MakeMessageID("post-to-edit")},
	}

	err := mc.HandleMatrixEdit(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error when PatchPost fails, got nil")
	}
	if !strings.Contains(err.Error(), "failed to edit post") {
		t.Errorf("expected error about failed edit, got: %v", err)
	}
}

func TestHandleMatrixEdit_NotLoggedIn(t *testing.T) {
	mc := newNotLoggedInClient()

	msg := &bridgev2.MatrixEdit{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Portal:  makeTestPortal("test-channel"),
			Content: &event.MessageEventContent{MsgType: event.MsgText, Body: "edited"},
		},
		EditTarget: &database.Message{ID: MakeMessageID("post-to-edit")},
	}

	err := mc.HandleMatrixEdit(context.Background(), msg)
	if !errors.Is(err, bridgev2.ErrNotLoggedIn) {
		t.Errorf("expected ErrNotLoggedIn, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// HandleMatrixMessageRemove tests
// ---------------------------------------------------------------------------

func TestHandleMatrixMessageRemove_Success(t *testing.T) {
	fm := newFakeMM()
	defer fm.Close()
	mc := newFullTestClient(fm.Server.URL)

	msg := &bridgev2.MatrixMessageRemove{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.RedactionEventContent]{
			Portal: makeTestPortal("test-channel"),
		},
		TargetMessage: &database.Message{ID: MakeMessageID("post-to-delete")},
	}

	err := mc.HandleMatrixMessageRemove(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fm.CalledPath("/api/v4/posts/post-to-delete") {
		t.Error("expected delete endpoint to be called for 'post-to-delete'")
	}
}

func TestHandleMatrixMessageRemove_APIError(t *testing.T) {
	fm := newFakeMM()
	defer fm.Close()

	fm.FailEndpoints["/api/v4/posts/"] = true

	mc := newFullTestClient(fm.Server.URL)

	msg := &bridgev2.MatrixMessageRemove{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.RedactionEventContent]{
			Portal: makeTestPortal("test-channel"),
		},
		TargetMessage: &database.Message{ID: MakeMessageID("post-to-delete")},
	}

	err := mc.HandleMatrixMessageRemove(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error when DeletePost fails, got nil")
	}
	if !strings.Contains(err.Error(), "failed to delete post") {
		t.Errorf("expected error about failed delete, got: %v", err)
	}
}

func TestHandleMatrixMessageRemove_NotLoggedIn(t *testing.T) {
	mc := newNotLoggedInClient()

	msg := &bridgev2.MatrixMessageRemove{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.RedactionEventContent]{
			Portal: makeTestPortal("test-channel"),
		},
		TargetMessage: &database.Message{ID: MakeMessageID("post-to-delete")},
	}

	err := mc.HandleMatrixMessageRemove(context.Background(), msg)
	if !errors.Is(err, bridgev2.ErrNotLoggedIn) {
		t.Errorf("expected ErrNotLoggedIn, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// PreHandleMatrixReaction tests
// ---------------------------------------------------------------------------

func TestPreHandleMatrixReaction(t *testing.T) {
	fm := newFakeMM()
	defer fm.Close()
	mc := newFullTestClient(fm.Server.URL)

	msg := &bridgev2.MatrixReaction{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.ReactionEventContent]{
			Portal:  makeTestPortal("test-channel"),
			Content: &event.ReactionEventContent{RelatesTo: event.RelatesTo{Key: "\U0001f44d"}},
		},
		TargetMessage: &database.Message{ID: MakeMessageID("target-post")},
	}

	resp, err := mc.PreHandleMatrixReaction(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(resp.EmojiID) != "+1" {
		t.Errorf("expected EmojiID '+1', got %q", resp.EmojiID)
	}
	if string(resp.SenderID) != "my-user-id" {
		t.Errorf("expected SenderID 'my-user-id', got %q", resp.SenderID)
	}
}

// ---------------------------------------------------------------------------
// HandleMatrixReaction tests
// ---------------------------------------------------------------------------

func TestHandleMatrixReaction_Success(t *testing.T) {
	fm := newFakeMM()
	defer fm.Close()
	mc := newFullTestClient(fm.Server.URL)

	msg := &bridgev2.MatrixReaction{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.ReactionEventContent]{
			Portal:  makeTestPortal("test-channel"),
			Content: &event.ReactionEventContent{RelatesTo: event.RelatesTo{Key: "\U0001f44d"}},
		},
		TargetMessage: &database.Message{ID: MakeMessageID("target-post")},
		PreHandleResp: &bridgev2.MatrixReactionPreResponse{
			EmojiID: MakeEmojiID("+1"),
		},
	}

	reaction, err := mc.HandleMatrixReaction(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reaction == nil {
		t.Fatal("expected non-nil reaction")
	}
	if string(reaction.EmojiID) != "+1" {
		t.Errorf("expected EmojiID '+1', got %q", reaction.EmojiID)
	}
	if !fm.CalledPath("/api/v4/reactions") {
		t.Error("expected /api/v4/reactions to be called")
	}
}

func TestHandleMatrixReaction_APIError(t *testing.T) {
	fm := newFakeMM()
	defer fm.Close()

	fm.FailEndpoints["/api/v4/reactions"] = true

	mc := newFullTestClient(fm.Server.URL)

	msg := &bridgev2.MatrixReaction{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.ReactionEventContent]{
			Portal:  makeTestPortal("test-channel"),
			Content: &event.ReactionEventContent{RelatesTo: event.RelatesTo{Key: "\U0001f44d"}},
		},
		TargetMessage: &database.Message{ID: MakeMessageID("target-post")},
		PreHandleResp: &bridgev2.MatrixReactionPreResponse{
			EmojiID: MakeEmojiID("+1"),
		},
	}

	_, err := mc.HandleMatrixReaction(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error when SaveReaction fails, got nil")
	}
	if !strings.Contains(err.Error(), "failed to save reaction") {
		t.Errorf("expected error about failed reaction, got: %v", err)
	}
}

func TestHandleMatrixReaction_NotLoggedIn(t *testing.T) {
	mc := newNotLoggedInClient()

	msg := &bridgev2.MatrixReaction{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.ReactionEventContent]{
			Portal:  makeTestPortal("test-channel"),
			Content: &event.ReactionEventContent{RelatesTo: event.RelatesTo{Key: "\U0001f44d"}},
		},
		TargetMessage: &database.Message{ID: MakeMessageID("target-post")},
		PreHandleResp: &bridgev2.MatrixReactionPreResponse{
			EmojiID: MakeEmojiID("+1"),
		},
	}

	_, err := mc.HandleMatrixReaction(context.Background(), msg)
	if !errors.Is(err, bridgev2.ErrNotLoggedIn) {
		t.Errorf("expected ErrNotLoggedIn, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// HandleMatrixReactionRemove tests
// ---------------------------------------------------------------------------

func TestHandleMatrixReactionRemove_Success(t *testing.T) {
	fm := newFakeMM()
	defer fm.Close()
	mc := newFullTestClient(fm.Server.URL)

	msg := &bridgev2.MatrixReactionRemove{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.RedactionEventContent]{
			Portal: makeTestPortal("test-channel"),
		},
		TargetReaction: &database.Reaction{
			MessageID: MakeMessageID("target-post"),
			EmojiID:   MakeEmojiID("+1"),
		},
	}

	err := mc.HandleMatrixReactionRemove(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fm.CalledPath("/reactions/") {
		t.Error("expected delete reactions endpoint to be called")
	}
}

func TestHandleMatrixReactionRemove_APIError(t *testing.T) {
	fm := newFakeMM()
	defer fm.Close()

	fm.FailEndpoints["/reactions/"] = true

	mc := newFullTestClient(fm.Server.URL)

	msg := &bridgev2.MatrixReactionRemove{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.RedactionEventContent]{
			Portal: makeTestPortal("test-channel"),
		},
		TargetReaction: &database.Reaction{
			MessageID: MakeMessageID("target-post"),
			EmojiID:   MakeEmojiID("+1"),
		},
	}

	err := mc.HandleMatrixReactionRemove(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error when DeleteReaction fails, got nil")
	}
	if !strings.Contains(err.Error(), "failed to remove reaction") {
		t.Errorf("expected error about failed reaction removal, got: %v", err)
	}
}

func TestHandleMatrixReactionRemove_NotLoggedIn(t *testing.T) {
	mc := newNotLoggedInClient()

	msg := &bridgev2.MatrixReactionRemove{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.RedactionEventContent]{
			Portal: makeTestPortal("test-channel"),
		},
		TargetReaction: &database.Reaction{
			MessageID: MakeMessageID("target-post"),
			EmojiID:   MakeEmojiID("+1"),
		},
	}

	err := mc.HandleMatrixReactionRemove(context.Background(), msg)
	if !errors.Is(err, bridgev2.ErrNotLoggedIn) {
		t.Errorf("expected ErrNotLoggedIn, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// HandleMatrixReadReceipt tests
// ---------------------------------------------------------------------------

func TestHandleMatrixReadReceipt_Success(t *testing.T) {
	fm := newFakeMM()
	defer fm.Close()
	mc := newFullTestClient(fm.Server.URL)

	msg := &bridgev2.MatrixReadReceipt{
		Portal: makeTestPortal("test-channel"),
	}

	err := mc.HandleMatrixReadReceipt(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleMatrixReadReceipt_APIError(t *testing.T) {
	fm := newFakeMM()
	defer fm.Close()

	fm.FailEndpoints["/members/"] = true

	mc := newFullTestClient(fm.Server.URL)

	msg := &bridgev2.MatrixReadReceipt{
		Portal: makeTestPortal("test-channel"),
	}

	err := mc.HandleMatrixReadReceipt(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error when ViewChannel fails, got nil")
	}
}

func TestHandleMatrixReadReceipt_NotLoggedIn(t *testing.T) {
	mc := newNotLoggedInClient()

	msg := &bridgev2.MatrixReadReceipt{
		Portal: makeTestPortal("test-channel"),
	}

	err := mc.HandleMatrixReadReceipt(context.Background(), msg)
	if !errors.Is(err, bridgev2.ErrNotLoggedIn) {
		t.Errorf("expected ErrNotLoggedIn, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// HandleMatrixTyping tests
// ---------------------------------------------------------------------------

func TestHandleMatrixTyping_Success(t *testing.T) {
	fm := newFakeMM()
	defer fm.Close()
	mc := newFullTestClient(fm.Server.URL)

	msg := &bridgev2.MatrixTyping{
		Portal:   makeTestPortal("test-channel"),
		IsTyping: true,
	}

	err := mc.HandleMatrixTyping(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleMatrixTyping_APIError(t *testing.T) {
	fm := newFakeMM()
	defer fm.Close()

	fm.FailEndpoints["/typing"] = true

	mc := newFullTestClient(fm.Server.URL)

	msg := &bridgev2.MatrixTyping{
		Portal:   makeTestPortal("test-channel"),
		IsTyping: true,
	}

	// PublishUserTyping fails, but HandleMatrixTyping logs the error and returns nil.
	err := mc.HandleMatrixTyping(context.Background(), msg)
	if err != nil {
		t.Fatalf("expected nil error (error is only logged), got: %v", err)
	}
}

func TestHandleMatrixTyping_NotLoggedIn(t *testing.T) {
	mc := newNotLoggedInClient()

	msg := &bridgev2.MatrixTyping{
		Portal:   makeTestPortal("test-channel"),
		IsTyping: true,
	}

	err := mc.HandleMatrixTyping(context.Background(), msg)
	if !errors.Is(err, bridgev2.ErrNotLoggedIn) {
		t.Errorf("expected ErrNotLoggedIn, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// emojiToReaction edge cases
// ---------------------------------------------------------------------------

func TestEmojiToReaction_EmptyStringMatrix(t *testing.T) {
	got := emojiToReaction("")
	if got != "" {
		t.Errorf("emojiToReaction empty: got %q, want empty", got)
	}
}

func TestEmojiToReaction_ColonWithSingleChar(t *testing.T) {
	// ":a:" has len 3 (> 2), starts and ends with colon -> strips to "a".
	got := emojiToReaction(":a:")
	if got != "a" {
		t.Errorf("emojiToReaction(:a:): got %q, want %q", got, "a")
	}
}

func TestEmojiToReaction_ColonInMiddle(t *testing.T) {
	// "a:b" doesn't start with colon -> passthrough.
	got := emojiToReaction("a:b")
	if got != "a:b" {
		t.Errorf("emojiToReaction(a:b): got %q, want %q", got, "a:b")
	}
}

// ---------------------------------------------------------------------------
// HandleMatrixMessage with puppet (full integration)
// ---------------------------------------------------------------------------

func TestHandleMatrixMessage_WithPuppetSender(t *testing.T) {
	fm := newFakeMM()
	defer fm.Close()
	mc := newFullTestClient(fm.Server.URL)

	// Set up a puppet with a real client pointing at our fake server.
	puppetClient := model.NewAPIv4Client(fm.Server.URL)
	puppetClient.SetToken("puppet-token")
	mc.connector.Puppets[id.UserID("@alice:localhost")] = &PuppetClient{
		MXID:     id.UserID("@alice:localhost"),
		Client:   puppetClient,
		UserID:   "alice-mm-id",
		Username: "alice",
	}

	msg := &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Portal:  makeTestPortal("test-channel"),
			Content: &event.MessageEventContent{MsgType: event.MsgText, Body: "From Alice"},
			Event:   &event.Event{Sender: "@alice:localhost"},
		},
	}

	resp, err := mc.HandleMatrixMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	// Verify the response SenderID is the puppet's MM ID, not the relay user.
	if string(resp.DB.SenderID) != "alice-mm-id" {
		t.Errorf("expected SenderID 'alice-mm-id', got %q", resp.DB.SenderID)
	}
}

func TestHandleMatrixMessage_EmptyBody(t *testing.T) {
	fm := newFakeMM()
	defer fm.Close()
	mc := newFullTestClient(fm.Server.URL)

	msg := &bridgev2.MatrixMessage{
		MatrixEventBase: bridgev2.MatrixEventBase[*event.MessageEventContent]{
			Portal:  makeTestPortal("test-channel"),
			Content: &event.MessageEventContent{MsgType: event.MsgText, Body: ""},
		},
	}

	// Empty body text message should still succeed (kChat allows empty-ish posts).
	resp, err := mc.HandleMatrixMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

// Note: Testing HandleMatrixMessage with file uploads (MsgImage/MsgVideo/MsgAudio/MsgFile)
// requires a full bridge setup for DownloadMedia, which is impractical in unit tests.
// The media upload path is covered by integration tests or manual testing.
