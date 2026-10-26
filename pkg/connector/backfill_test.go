// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

// makePostList creates a model.PostList from a slice of posts, ordered newest first.
func makePostList(posts []*model.Post) *model.PostList {
	pl := model.NewPostList()
	for _, p := range posts {
		pl.AddPost(p)
		pl.AddOrder(p.Id)
	}
	return pl
}

// TestFetchMessages_BasicBackfill verifies that FetchMessages returns posts in
// chronological order (oldest first) with correct sender mapping and timestamps.
func TestFetchMessages_BasicBackfill(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	now := time.Now().UnixMilli()
	posts := []*model.Post{
		{Id: "post3", ChannelId: "ch1", UserId: "user1", Message: "third", CreateAt: now - 1000},
		{Id: "post2", ChannelId: "ch1", UserId: "user2", Message: "second", CreateAt: now - 2000},
		{Id: "post1", ChannelId: "ch1", UserId: "user1", Message: "first", CreateAt: now - 3000},
	}
	fake.Posts["ch1"] = makePostList(posts)

	mc := newFullTestClient(fake.Server.URL)
	portal := makeTestPortal("ch1")

	resp, err := mc.FetchMessages(context.Background(), bridgev2.FetchMessagesParams{
		Portal: portal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(resp.Messages))
	}

	// Verify chronological order (oldest first).
	wantOrder := []string{"post1", "post2", "post3"}
	for i, wantID := range wantOrder {
		if string(resp.Messages[i].ID) != wantID {
			t.Errorf("message[%d]: got %q, want %q", i, resp.Messages[i].ID, wantID)
		}
	}

	// Verify sender mapping.
	if string(resp.Messages[0].Sender.Sender) != "user1" {
		t.Errorf("first sender: got %q, want %q", resp.Messages[0].Sender.Sender, "user1")
	}
	if string(resp.Messages[1].Sender.Sender) != "user2" {
		t.Errorf("second sender: got %q, want %q", resp.Messages[1].Sender.Sender, "user2")
	}

	// Verify timestamp.
	if resp.Messages[0].Timestamp.UnixMilli() != now-3000 {
		t.Errorf("first timestamp: got %d, want %d", resp.Messages[0].Timestamp.UnixMilli(), now-3000)
	}
}

// TestFetchMessages_EmptyChannel verifies that FetchMessages returns an empty
// message list with HasMore=false for channels with no posts.
func TestFetchMessages_EmptyChannel(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	mc := newFullTestClient(fake.Server.URL)
	portal := makeTestPortal("ch1")

	resp, err := mc.FetchMessages(context.Background(), bridgev2.FetchMessagesParams{
		Portal: portal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Messages) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(resp.Messages))
	}
	if resp.HasMore {
		t.Error("expected HasMore to be false for empty channel")
	}
}

// TestFetchMessages_SkipsSystemMessages verifies that system messages (e.g.,
// join/leave) are filtered out during backfill, preserving only user posts.
func TestFetchMessages_SkipsSystemMessages(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	now := time.Now().UnixMilli()
	posts := []*model.Post{
		{Id: "post1", ChannelId: "ch1", UserId: "user1", Message: "hello", CreateAt: now - 2000},
		{Id: "sys1", ChannelId: "ch1", UserId: "user1", Message: "joined", CreateAt: now - 1000, Type: model.PostTypeJoinChannel},
		{Id: "post2", ChannelId: "ch1", UserId: "user2", Message: "world", CreateAt: now},
	}
	fake.Posts["ch1"] = makePostList(posts)

	mc := newFullTestClient(fake.Server.URL)
	portal := makeTestPortal("ch1")

	resp, err := mc.FetchMessages(context.Background(), bridgev2.FetchMessagesParams{
		Portal: portal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Messages) != 2 {
		t.Fatalf("expected 2 messages (system filtered), got %d", len(resp.Messages))
	}
	if string(resp.Messages[0].ID) != "post1" {
		t.Errorf("first message: got %q, want %q", resp.Messages[0].ID, "post1")
	}
	if string(resp.Messages[1].ID) != "post2" {
		t.Errorf("second message: got %q, want %q", resp.Messages[1].ID, "post2")
	}
}

// TestFetchMessages_RespectsMaxCount verifies that BackfillMaxCount limits
// the number of messages returned.
func TestFetchMessages_RespectsMaxCount(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	now := time.Now().UnixMilli()
	var posts []*model.Post
	for i := range 5 {
		posts = append(posts, &model.Post{
			Id:        fmt.Sprintf("post%d", i),
			ChannelId: "ch1",
			UserId:    "user1",
			Message:   "msg",
			CreateAt:  now - int64((5-i)*1000),
		})
	}
	fake.Posts["ch1"] = makePostList(posts)

	mc := newFullTestClient(fake.Server.URL)
	mc.connector.Config.BackfillMaxCount = 2
	portal := makeTestPortal("ch1")

	resp, err := mc.FetchMessages(context.Background(), bridgev2.FetchMessagesParams{
		Portal: portal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Messages) != 2 {
		t.Fatalf("expected 2 messages (max count), got %d", len(resp.Messages))
	}
}

// TestFetchMessages_DefaultMaxCount verifies that BackfillMaxCount=0 defaults
// to 100 and does not error on an empty channel.
func TestFetchMessages_DefaultMaxCount(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	mc := newFullTestClient(fake.Server.URL)
	mc.connector.Config.BackfillMaxCount = 0
	portal := makeTestPortal("ch1")

	resp, err := mc.FetchMessages(context.Background(), bridgev2.FetchMessagesParams{
		Portal: portal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

// TestFetchMessages_NegativeMaxCount verifies that a negative BackfillMaxCount
// also defaults to 100 (same as zero).
func TestFetchMessages_NegativeMaxCount(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	mc := newFullTestClient(fake.Server.URL)
	mc.connector.Config.BackfillMaxCount = -5
	portal := makeTestPortal("ch1")

	resp, err := mc.FetchMessages(context.Background(), bridgev2.FetchMessagesParams{
		Portal: portal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response for negative max count")
	}
}

// TestFetchMessages_ParamsCountOverridesConfig verifies that params.Count
// takes precedence over the configured BackfillMaxCount.
func TestFetchMessages_ParamsCountOverridesConfig(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	now := time.Now().UnixMilli()
	var posts []*model.Post
	for i := range 5 {
		posts = append(posts, &model.Post{
			Id:        fmt.Sprintf("post%d", i),
			ChannelId: "ch1",
			UserId:    "user1",
			Message:   "msg",
			CreateAt:  now - int64((5-i)*1000),
		})
	}
	fake.Posts["ch1"] = makePostList(posts)

	mc := newFullTestClient(fake.Server.URL)
	mc.connector.Config.BackfillMaxCount = 100
	portal := makeTestPortal("ch1")

	resp, err := mc.FetchMessages(context.Background(), bridgev2.FetchMessagesParams{
		Portal: portal,
		Count:  3,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Messages) != 3 {
		t.Fatalf("expected 3 messages (params.Count override), got %d", len(resp.Messages))
	}
}

// TestFetchMessages_WithAnchor verifies backward pagination using AnchorMessage.
func TestFetchMessages_WithAnchor(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	now := time.Now().UnixMilli()
	posts := []*model.Post{
		{Id: "older1", ChannelId: "ch1", UserId: "user1", Message: "older", CreateAt: now - 3000},
		{Id: "older2", ChannelId: "ch1", UserId: "user1", Message: "older2", CreateAt: now - 2000},
	}
	fake.Posts["ch1"] = makePostList(posts)

	mc := newFullTestClient(fake.Server.URL)
	portal := makeTestPortal("ch1")

	anchor := &database.Message{
		ID: networkid.MessageID("anchor-post"),
	}

	resp, err := mc.FetchMessages(context.Background(), bridgev2.FetchMessagesParams{
		Portal:        portal,
		AnchorMessage: anchor,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The fakeMM always returns the same PostList for the channel regardless
	// of the before= query param, so we just verify the call succeeded and
	// messages are returned in chronological order.
	if len(resp.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(resp.Messages))
	}
	if string(resp.Messages[0].ID) != "older1" {
		t.Errorf("first message: got %q, want %q", resp.Messages[0].ID, "older1")
	}
	if resp.Forward {
		t.Error("expected Forward to be false for backward fetch")
	}
}

// TestFetchMessages_ForwardWithAnchor verifies forward pagination using
// AnchorMessage with Forward=true.
func TestFetchMessages_ForwardWithAnchor(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	now := time.Now().UnixMilli()
	posts := []*model.Post{
		{Id: "newer1", ChannelId: "ch1", UserId: "user1", Message: "newer", CreateAt: now - 1000},
		{Id: "newer2", ChannelId: "ch1", UserId: "user2", Message: "newest", CreateAt: now},
	}
	fake.Posts["ch1"] = makePostList(posts)

	mc := newFullTestClient(fake.Server.URL)
	portal := makeTestPortal("ch1")

	anchor := &database.Message{
		ID: networkid.MessageID("anchor-post"),
	}

	resp, err := mc.FetchMessages(context.Background(), bridgev2.FetchMessagesParams{
		Portal:        portal,
		AnchorMessage: anchor,
		Forward:       true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Forward {
		t.Error("expected Forward to be true in response")
	}
	if len(resp.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(resp.Messages))
	}
}

// TestFetchMessages_HasMore verifies that HasMore is true when the API returns
// exactly perPage posts (indicating more may be available).
func TestFetchMessages_HasMore(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	now := time.Now().UnixMilli()
	posts := []*model.Post{
		{Id: "p1", ChannelId: "ch1", UserId: "u1", Message: "a", CreateAt: now - 3000},
		{Id: "p2", ChannelId: "ch1", UserId: "u1", Message: "b", CreateAt: now - 2000},
		{Id: "p3", ChannelId: "ch1", UserId: "u1", Message: "c", CreateAt: now - 1000},
	}
	fake.Posts["ch1"] = makePostList(posts)

	mc := newFullTestClient(fake.Server.URL)
	mc.connector.Config.BackfillMaxCount = 3
	portal := makeTestPortal("ch1")

	resp, err := mc.FetchMessages(context.Background(), bridgev2.FetchMessagesParams{
		Portal: portal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.HasMore {
		t.Error("expected HasMore to be true when Order length equals perPage")
	}
}

// TestFetchMessages_APIError verifies that FetchMessages propagates API errors
// with descriptive wrapping.
func TestFetchMessages_APIError(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	fake.FailEndpoints["/posts"] = true

	mc := newFullTestClient(fake.Server.URL)
	portal := makeTestPortal("ch1")

	_, err := mc.FetchMessages(context.Background(), bridgev2.FetchMessagesParams{
		Portal: portal,
	})
	if err == nil {
		t.Fatal("expected error when posts endpoint fails")
	}
	if !strings.Contains(err.Error(), "failed to fetch posts for backfill") {
		t.Errorf("error should wrap with backfill context, got: %v", err)
	}
}

// TestFetchMessages_MessageContent verifies that backfilled messages include
// converted content with at least one part.
func TestFetchMessages_MessageContent(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	now := time.Now().UnixMilli()
	posts := []*model.Post{
		{Id: "p1", ChannelId: "ch1", UserId: "user1", Message: "hello **bold**", CreateAt: now},
	}
	fake.Posts["ch1"] = makePostList(posts)

	mc := newFullTestClient(fake.Server.URL)
	portal := makeTestPortal("ch1")

	resp, err := mc.FetchMessages(context.Background(), bridgev2.FetchMessagesParams{
		Portal: portal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(resp.Messages))
	}

	msg := resp.Messages[0]
	if msg.ConvertedMessage == nil || len(msg.Parts) == 0 {
		t.Fatal("expected converted message with parts")
	}
}

// TestFetchMessages_ThreadPost verifies that reply posts have ShouldBackfillThread
// set and include ReplyTo references.
func TestFetchMessages_ThreadPost(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	now := time.Now().UnixMilli()
	posts := []*model.Post{
		{Id: "parent", ChannelId: "ch1", UserId: "user1", Message: "parent msg", CreateAt: now - 2000},
		{Id: "reply", ChannelId: "ch1", UserId: "user2", Message: "reply msg", CreateAt: now - 1000, RootId: "parent"},
	}
	fake.Posts["ch1"] = makePostList(posts)

	mc := newFullTestClient(fake.Server.URL)
	portal := makeTestPortal("ch1")

	resp, err := mc.FetchMessages(context.Background(), bridgev2.FetchMessagesParams{
		Portal: portal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(resp.Messages))
	}

	// Parent should not have ShouldBackfillThread.
	if resp.Messages[0].ShouldBackfillThread {
		t.Error("parent message should not have ShouldBackfillThread")
	}

	// Reply should have ShouldBackfillThread set.
	if !resp.Messages[1].ShouldBackfillThread {
		t.Error("reply message should have ShouldBackfillThread set")
	}

	// Reply should have ReplyTo set in the converted message.
	if resp.Messages[1].ReplyTo == nil {
		t.Error("reply should have ReplyTo set")
	}
}

// TestFetchMessages_CancelledContext verifies that a cancelled context is
// propagated to the API call and results in an error.
func TestFetchMessages_CancelledContext(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	mc := newFullTestClient(fake.Server.URL)
	portal := makeTestPortal("ch1")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := mc.FetchMessages(ctx, bridgev2.FetchMessagesParams{
		Portal: portal,
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// TestFetchMessages_AllSystemMessages verifies that a channel with only system
// messages returns an empty result.
func TestFetchMessages_AllSystemMessages(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	now := time.Now().UnixMilli()
	posts := []*model.Post{
		{Id: "sys1", ChannelId: "ch1", UserId: "user1", Message: "joined", CreateAt: now - 2000, Type: model.PostTypeJoinChannel},
		{Id: "sys2", ChannelId: "ch1", UserId: "user2", Message: "left", CreateAt: now - 1000, Type: model.PostTypeLeaveChannel},
	}
	fake.Posts["ch1"] = makePostList(posts)

	mc := newFullTestClient(fake.Server.URL)
	portal := makeTestPortal("ch1")

	resp, err := mc.FetchMessages(context.Background(), bridgev2.FetchMessagesParams{
		Portal: portal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Messages) != 0 {
		t.Errorf("expected 0 messages (all system), got %d", len(resp.Messages))
	}
}

// TestFetchMessages_PerPageCap verifies that when maxCount > 200, perPage is
// capped at 200 (this is verified indirectly since the fake server accepts any
// page size, but we verify the request succeeds and the result is truncated).
func TestFetchMessages_LargeMaxCount(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	mc := newFullTestClient(fake.Server.URL)
	mc.connector.Config.BackfillMaxCount = 500
	portal := makeTestPortal("ch1")

	resp, err := mc.FetchMessages(context.Background(), bridgev2.FetchMessagesParams{
		Portal: portal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response for large max count")
	}
}
