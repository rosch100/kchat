// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/id"
)

// TestDisconnect_ClosesStopChan verifies that Disconnect closes the stopChan.
func TestDisconnect_ClosesStopChan(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	mc := newFullTestClient(fake.Server.URL)
	mc.Disconnect()

	select {
	case <-mc.stopChan:
		// expected: channel is closed
	default:
		t.Fatal("stopChan was not closed after Disconnect")
	}
}

// TestDisconnect_DoubleSafe verifies calling Disconnect twice does not panic.
func TestDisconnect_DoubleSafe(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	mc := newFullTestClient(fake.Server.URL)
	mc.Disconnect()
	mc.Disconnect() // second call should not panic
}

// TestDisconnect_NilWsClient verifies Disconnect handles nil wsClient gracefully.
func TestDisconnect_NilWsClient(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	mc := newFullTestClient(fake.Server.URL)
	mc.wsClient = nil
	mc.Disconnect() // should not panic with nil wsClient
}

// TestLogoutRemote_CallsLogout verifies that LogoutRemote calls the logout endpoint.
func TestLogoutRemote_CallsLogout(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	fake.Users["my-user-id"] = &model.User{Id: "my-user-id", Username: "testuser"}
	fake.TokenToUser["test-token"] = "my-user-id"

	mc := newFullTestClient(fake.Server.URL)
	mc.LogoutRemote(context.Background())

	if !fake.CalledPath("/users/logout") {
		t.Fatal("expected /users/logout to be called")
	}
}

// TestLogoutRemote_NilClient verifies LogoutRemote does not panic with nil client.
func TestLogoutRemote_NilClient(t *testing.T) {
	t.Parallel()
	mc := newNotLoggedInClient()
	mc.LogoutRemote(context.Background()) // should not panic with nil client
}

// TestGetChatInfo_Success verifies GetChatInfo returns correct channel info.
func TestGetChatInfo_Success(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	fake.Channels["ch1"] = &model.Channel{
		Id:          "ch1",
		Name:        "test-channel",
		DisplayName: "Test Channel",
		Type:        model.ChannelTypeOpen,
	}
	fake.ChannelMembers["ch1"] = model.ChannelMembers{
		{ChannelId: "ch1", UserId: "u1"},
		{ChannelId: "ch1", UserId: "u2"},
	}

	mc := newFullTestClient(fake.Server.URL)
	mc.connector.Config.DisplaynameTemplate = "{{.Username}}"
	_ = mc.connector.Config.PostProcess()

	portal := makeTestPortal("ch1")
	info, err := mc.GetChatInfo(context.Background(), portal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil ChatInfo")
	}
	if info.Name == nil || *info.Name != "Test Channel" {
		t.Fatalf("expected name 'Test Channel', got %v", info.Name)
	}
	if info.Members == nil || len(info.Members.MemberMap) != 2 {
		t.Fatalf("expected 2 members, got %v", info.Members)
	}
}

// TestGetChatInfo_ChannelError verifies GetChatInfo returns a descriptive error
// when the channel endpoint fails.
func TestGetChatInfo_ChannelError(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	fake.FailEndpoints["/channels/"] = true

	mc := newFullTestClient(fake.Server.URL)

	portal := makeTestPortal("ch1")
	_, err := mc.GetChatInfo(context.Background(), portal)
	if err == nil {
		t.Fatal("expected error when channel endpoint fails")
	}
	if !strings.Contains(err.Error(), "failed to get channel info") {
		t.Errorf("error should mention channel info, got: %v", err)
	}
}

// TestGetUserInfo_Success verifies GetUserInfo returns correct user info.
func TestGetUserInfo_Success(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	fake.Users["uid1"] = &model.User{Id: "uid1", Username: "testuser"}

	mc := newFullTestClient(fake.Server.URL)
	mc.connector.Config.DisplaynameTemplate = "{{.Username}}"
	_ = mc.connector.Config.PostProcess()

	ghost := &bridgev2.Ghost{Ghost: &database.Ghost{ID: MakeUserID("uid1")}}
	info, err := mc.GetUserInfo(context.Background(), ghost)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil UserInfo")
	}
	if info.Name == nil || *info.Name != "testuser" {
		t.Fatalf("expected name 'testuser', got %v", info.Name)
	}
}

// TestGetUserInfo_Error verifies GetUserInfo returns a descriptive error
// when the user endpoint fails.
func TestGetUserInfo_Error(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	fake.FailEndpoints["/users/"] = true

	mc := newFullTestClient(fake.Server.URL)

	ghost := &bridgev2.Ghost{Ghost: &database.Ghost{ID: MakeUserID("uid1")}}
	_, err := mc.GetUserInfo(context.Background(), ghost)
	if err == nil {
		t.Fatal("expected error when user endpoint fails")
	}
	if !strings.Contains(err.Error(), "failed to get user info") {
		t.Errorf("error should mention user info, got: %v", err)
	}
}

// TestSyncChannels_FetchError verifies that syncChannels handles channel fetch
// errors gracefully (logs, does not panic).
func TestSyncChannels_FetchError(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	fake.FailEndpoints["/channels"] = true

	mc := newFullTestClient(fake.Server.URL)
	mc.connector.Config.DisplaynameTemplate = "{{.Username}}"
	_ = mc.connector.Config.PostProcess()

	// Should return without panic -- the error is logged, not propagated.
	mc.syncChannels(context.Background())
}

// TestSyncChannels_MemberFetchError verifies that a member fetch error skips
// the channel but does not panic.
func TestSyncChannels_MemberFetchError(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	fake.ChannelsForTeamUser["my-team-id:my-user-id"] = []*model.Channel{
		{Id: "pub1", Name: "public-channel", DisplayName: "Public", Type: model.ChannelTypeOpen},
	}
	fake.FailEndpoints["/members"] = true

	mc := newFullTestClient(fake.Server.URL)
	mc.connector.Config.DisplaynameTemplate = "{{.Username}}"
	_ = mc.connector.Config.PostProcess()

	// GetChannelMembers fails, channel is skipped but no panic.
	mc.syncChannels(context.Background())
}

// TestSyncChannels_IncludesDMs verifies that DMs and group DMs are included
// in the channel sync.
func TestSyncChannels_IncludesDMs(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	fake.ChannelsForUser["my-user-id"] = []*model.Channel{
		{Id: "dm1", Name: "dm-channel", Type: model.ChannelTypeDirect},
		{Id: "gm1", Name: "group-channel", DisplayName: "Group Chat", Type: model.ChannelTypeGroup},
	}
	fake.ChannelMembers["dm1"] = model.ChannelMembers{
		{ChannelId: "dm1", UserId: "my-user-id"},
		{ChannelId: "dm1", UserId: "other-user"},
	}
	fake.ChannelMembers["gm1"] = model.ChannelMembers{
		{ChannelId: "gm1", UserId: "my-user-id"},
		{ChannelId: "gm1", UserId: "user2"},
		{ChannelId: "gm1", UserId: "user3"},
	}

	mc := newFullTestClient(fake.Server.URL)
	mc.connector.Config.DisplaynameTemplate = "{{.Username}}"
	_ = mc.connector.Config.PostProcess()
	mock := testMock(mc)

	mc.syncChannels(context.Background())

	events := mock.Events()
	if len(events) != 2 {
		t.Fatalf("expected 2 events (DMs should be included), got %d", len(events))
	}
}

// TestSyncChannels_NonDMChannelReachesQueue verifies that public channels
// produce ChatResync events.
func TestSyncChannels_NonDMChannelReachesQueue(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	fake.ChannelsForTeamUser["my-team-id:my-user-id"] = []*model.Channel{
		{Id: "pub1", Name: "public-channel", DisplayName: "Public", Type: model.ChannelTypeOpen},
	}
	fake.ChannelsForUser["my-user-id"] = []*model.Channel{
		{Id: "pub1", Name: "public-channel", DisplayName: "Public", Type: model.ChannelTypeOpen},
	}
	fake.ChannelMembers["pub1"] = model.ChannelMembers{
		{ChannelId: "pub1", UserId: "my-user-id"},
	}

	mc := newFullTestClient(fake.Server.URL)
	mc.connector.Config.DisplaynameTemplate = "{{.Username}}"
	_ = mc.connector.Config.PostProcess()
	mock := testMock(mc)

	mc.syncChannels(context.Background())

	events := mock.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event queued (ChatResync), got %d", len(events))
	}
	if events[0].GetType() != bridgev2.RemoteEventChatResync {
		t.Errorf("event type: got %v, want RemoteEventChatResync", events[0].GetType())
	}
}

// TestSyncChannels_NoTeamStillFetchesDMs verifies that DMs are synced even
// when teamID is empty.
func TestSyncChannels_NoTeamStillFetchesDMs(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	fake.ChannelsForUser["my-user-id"] = []*model.Channel{
		{Id: "dm1", Name: "dm-channel", Type: model.ChannelTypeDirect},
	}
	fake.ChannelMembers["dm1"] = model.ChannelMembers{
		{ChannelId: "dm1", UserId: "my-user-id"},
		{ChannelId: "dm1", UserId: "other-user"},
	}

	mc := newFullTestClient(fake.Server.URL)
	mc.teamID = ""
	mc.connector.Config.DisplaynameTemplate = "{{.Username}}"
	_ = mc.connector.Config.PostProcess()
	mock := testMock(mc)

	mc.syncChannels(context.Background())

	events := mock.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event (DM synced without team), got %d", len(events))
	}
}

// TestSyncChannels_DeduplicatesChannels verifies that channels appearing in
// both team and all-user lists are only synced once.
func TestSyncChannels_DeduplicatesChannels(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	// Same channel appears in both team channels and all-user channels.
	pub := &model.Channel{Id: "pub1", Name: "public-channel", DisplayName: "Public", Type: model.ChannelTypeOpen}
	fake.ChannelsForTeamUser["my-team-id:my-user-id"] = []*model.Channel{pub}
	fake.ChannelsForUser["my-user-id"] = []*model.Channel{pub}
	fake.ChannelMembers["pub1"] = model.ChannelMembers{
		{ChannelId: "pub1", UserId: "my-user-id"},
	}

	mc := newFullTestClient(fake.Server.URL)
	mc.connector.Config.DisplaynameTemplate = "{{.Username}}"
	_ = mc.connector.Config.PostProcess()
	mock := testMock(mc)

	mc.syncChannels(context.Background())

	events := mock.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event (deduplicated), got %d", len(events))
	}
}

// TestNewKChatClient_WithMetadata verifies that NewKChatClient
// correctly initializes all fields from login metadata.
func TestNewKChatClient_WithMetadata(t *testing.T) {
	t.Parallel()
	log := zerolog.Nop()
	connector := &KChatConnector{
		Bridge:  &bridgev2.Bridge{},
		Puppets: make(map[id.UserID]*PuppetClient),
	}

	dbLogin := &database.UserLogin{
		Metadata: &UserLoginMetadata{
			ServerURL: "http://mm.test:8065",
			Token:     "test-token",
			UserID:    "user-123",
			TeamID:    "team-456",
		},
	}
	login := &bridgev2.UserLogin{
		UserLogin: dbLogin,
		Log:       log,
	}

	mc := NewKChatClient(login, connector)

	if mc.serverURL != "http://mm.test:8065" {
		t.Errorf("serverURL: got %q, want %q", mc.serverURL, "http://mm.test:8065")
	}
	if mc.userID != "user-123" {
		t.Errorf("userID: got %q, want %q", mc.userID, "user-123")
	}
	if mc.teamID != "team-456" {
		t.Errorf("teamID: got %q, want %q", mc.teamID, "team-456")
	}
	if mc.client == nil {
		t.Fatal("client should not be nil when metadata has token")
	}
	if mc.connector != connector {
		t.Error("connector should be set")
	}
	if !mc.IsLoggedIn() {
		t.Error("should be logged in with valid token")
	}
	if mc.eventSender == nil {
		t.Error("eventSender should not be nil")
	}
}

// TestNewKChatClient_EmptyMetadata verifies that NewKChatClient
// handles empty token by not creating a REST client.
func TestNewKChatClient_EmptyMetadata(t *testing.T) {
	t.Parallel()
	log := zerolog.Nop()
	connector := &KChatConnector{
		Bridge:  &bridgev2.Bridge{},
		Puppets: make(map[id.UserID]*PuppetClient),
	}

	dbLogin := &database.UserLogin{
		Metadata: &UserLoginMetadata{}, // empty token
	}
	login := &bridgev2.UserLogin{
		UserLogin: dbLogin,
		Log:       log,
	}

	mc := NewKChatClient(login, connector)

	if mc.client != nil {
		t.Error("client should be nil when metadata has no token")
	}
	if mc.IsLoggedIn() {
		t.Error("should not be logged in with empty metadata")
	}
}

// TestLoadUserLogin verifies that LoadUserLogin creates a properly initialized
// KChatClient and attaches it to the UserLogin.
func TestLoadUserLogin(t *testing.T) {
	t.Parallel()
	log := zerolog.Nop()
	connector := &KChatConnector{
		Bridge:  &bridgev2.Bridge{},
		Puppets: make(map[id.UserID]*PuppetClient),
	}

	dbLogin := &database.UserLogin{
		Metadata: &UserLoginMetadata{
			ServerURL: "http://mm.test:8065",
			Token:     "test-token",
			UserID:    "user-123",
			TeamID:    "team-456",
		},
	}
	login := &bridgev2.UserLogin{
		UserLogin: dbLogin,
		Log:       log,
	}

	err := connector.LoadUserLogin(context.Background(), login)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mc, ok := login.Client.(*KChatClient)
	if !ok {
		t.Fatalf("expected *KChatClient, got %T", login.Client)
	}
	if mc.serverURL != "http://mm.test:8065" {
		t.Errorf("serverURL: got %q, want %q", mc.serverURL, "http://mm.test:8065")
	}
	if !mc.IsLoggedIn() {
		t.Error("client should be logged in after LoadUserLogin")
	}
}

// TestDisconnect_ConcurrentSafe verifies that concurrent Disconnect calls
// do not race or panic.
func TestDisconnect_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	fake := newFakeMM()
	t.Cleanup(fake.Close)

	mc := newFullTestClient(fake.Server.URL)

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()
			mc.Disconnect()
		}()
	}

	wg.Wait()

	// After all goroutines finish, stopChan must be closed.
	select {
	case <-mc.stopChan:
		// expected: channel is closed
	default:
		t.Fatal("stopChan was not closed after concurrent Disconnect calls")
	}
}
