// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
)

func newTestClient() *KChatClient {
	cfg := Config{
		DisplaynameTemplate: "{{.Username}} (MM)",
	}
	_ = cfg.PostProcess()
	return &KChatClient{
		connector: &KChatConnector{
			Config:   cfg,
			dpLogins: make(map[string]networkid.UserLoginID),
		},
		userID: "myuserid",
	}
}

func TestChannelToChatInfo_PublicChannel(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	channel := &model.Channel{
		Id:          "ch123",
		Type:        model.ChannelTypeOpen,
		DisplayName: "General",
		Name:        "general",
		Header:      "Welcome to General",
	}
	members := model.ChannelMembers{
		{UserId: "user1", ChannelId: "ch123"},
		{UserId: "user2", ChannelId: "ch123"},
	}

	info := client.channelToChatInfo(channel, members)

	if info.Type == nil {
		t.Fatal("Type should not be nil")
	}
	if *info.Type != database.RoomTypeDefault {
		t.Errorf("Type: got %q, want %q", *info.Type, database.RoomTypeDefault)
	}
	if info.Name == nil || *info.Name != "General" {
		t.Errorf("Name: got %v, want %q", info.Name, "General")
	}
	if info.Topic == nil || *info.Topic != "Welcome to General" {
		t.Errorf("Topic: got %v, want %q", info.Topic, "Welcome to General")
	}
}

func TestChannelToChatInfo_DM(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	channel := &model.Channel{
		Id:   "dm123",
		Type: model.ChannelTypeDirect,
	}
	members := model.ChannelMembers{
		{UserId: "myuserid", ChannelId: "dm123"},
		{UserId: "user2", ChannelId: "dm123"},
	}

	info := client.channelToChatInfo(channel, members)

	if info.Type == nil {
		t.Fatal("Type should not be nil for DM")
	}
	if *info.Type != database.RoomTypeDM {
		t.Errorf("Type: got %q, want %q", *info.Type, database.RoomTypeDM)
	}
	if info.Name != nil {
		t.Error("DM should not have a name")
	}
	if info.Topic != nil {
		t.Error("DM should not have a topic")
	}
	if info.Members.OtherUserID != MakeUserID("user2") {
		t.Errorf("OtherUserID: got %q, want %q", info.Members.OtherUserID, MakeUserID("user2"))
	}
}

func TestChannelToChatInfo_GroupDM(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	channel := &model.Channel{
		Id:          "grp123",
		Type:        model.ChannelTypeGroup,
		DisplayName: "Group Chat",
	}
	members := model.ChannelMembers{
		{UserId: "user1", ChannelId: "grp123"},
		{UserId: "user2", ChannelId: "grp123"},
		{UserId: "user3", ChannelId: "grp123"},
	}

	info := client.channelToChatInfo(channel, members)

	if info.Type == nil {
		t.Fatal("Type should not be nil for group DM")
	}
	if *info.Type != database.RoomTypeGroupDM {
		t.Errorf("Group DM Type: got %q, want %q", *info.Type, database.RoomTypeGroupDM)
	}
	if info.Name == nil || *info.Name != "Group Chat" {
		t.Errorf("Group DM Name: got %v, want %q", info.Name, "Group Chat")
	}
}

func TestChannelToChatInfo_GroupDM_NoDisplayName(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	channel := &model.Channel{
		Id:   "grp456",
		Type: model.ChannelTypeGroup,
	}
	members := model.ChannelMembers{
		{UserId: "user1", ChannelId: "grp456"},
		{UserId: "user2", ChannelId: "grp456"},
	}

	info := client.channelToChatInfo(channel, members)

	if *info.Type != database.RoomTypeGroupDM {
		t.Errorf("Type: got %q, want %q", *info.Type, database.RoomTypeGroupDM)
	}
	if info.Name != nil {
		t.Errorf("Group DM without display name should have nil Name, got %q", *info.Name)
	}
}

func TestChannelToChatInfo_DM_OtherUser(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	channel := &model.Channel{
		Id:   "dm456",
		Type: model.ChannelTypeDirect,
	}
	members := model.ChannelMembers{
		{UserId: "myuserid", ChannelId: "dm456"},
		{UserId: "otheruser", ChannelId: "dm456"},
	}

	info := client.channelToChatInfo(channel, members)

	if info.Type == nil || *info.Type != database.RoomTypeDM {
		t.Fatalf("Type: got %v, want %q", info.Type, database.RoomTypeDM)
	}
	if info.Members.OtherUserID != MakeUserID("otheruser") {
		t.Errorf("OtherUserID: got %q, want %q", info.Members.OtherUserID, MakeUserID("otheruser"))
	}
}

func TestChannelToChatInfo_DM_SelfOnly(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	channel := &model.Channel{
		Id:   "dm789",
		Type: model.ChannelTypeDirect,
	}
	// Edge case: DM with only self as member (e.g., self-DM or partial member list).
	members := model.ChannelMembers{
		{UserId: "myuserid", ChannelId: "dm789"},
	}

	info := client.channelToChatInfo(channel, members)

	if info.Type == nil || *info.Type != database.RoomTypeDM {
		t.Fatalf("Type: got %v, want %q", info.Type, database.RoomTypeDM)
	}
	// OtherUserID should remain zero value since there is no other member.
	if info.Members.OtherUserID != "" {
		t.Errorf("OtherUserID should be empty for self-only DM, got %q", info.Members.OtherUserID)
	}
}

func TestChannelToChatInfo_FallbackName(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	channel := &model.Channel{
		Id:          "ch456",
		Type:        model.ChannelTypeOpen,
		DisplayName: "",
		Name:        "fallback-name",
	}
	members := model.ChannelMembers{}

	info := client.channelToChatInfo(channel, members)

	if info.Name == nil || *info.Name != "fallback-name" {
		t.Errorf("Name fallback: got %v, want %q", info.Name, "fallback-name")
	}
}

func TestChannelToChatInfo_NoHeader(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	channel := &model.Channel{
		Id:          "ch789",
		Type:        model.ChannelTypeOpen,
		DisplayName: "No Topic",
	}
	members := model.ChannelMembers{}

	info := client.channelToChatInfo(channel, members)

	if info.Topic != nil {
		t.Error("Topic should be nil when header is empty")
	}
}

func TestChannelMembersToChatMembers(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	members := model.ChannelMembers{
		{UserId: "user1", ChannelId: "ch1", SchemeAdmin: false},
		{UserId: "user2", ChannelId: "ch1", SchemeAdmin: true},
		{UserId: "user3", ChannelId: "ch1", SchemeAdmin: false},
	}

	result := client.channelMembersToChatMembers(members)

	if !result.IsFull {
		t.Error("IsFull should be true")
	}
	if result.TotalMemberCount != 3 {
		t.Errorf("TotalMemberCount: got %d, want 3", result.TotalMemberCount)
	}
	if len(result.MemberMap) != 3 {
		t.Fatalf("MemberMap length: got %d, want 3", len(result.MemberMap))
	}

	for uid, m := range result.MemberMap {
		if m.Membership != event.MembershipJoin {
			t.Errorf("Member %s Membership: got %q, want %q", uid, m.Membership, event.MembershipJoin)
		}
	}

	// user2 is admin -> power level 50.
	adminMember := result.MemberMap[MakeUserID("user2")]
	if adminMember.PowerLevel == nil {
		t.Fatal("Admin member should have a PowerLevel")
	}
	if *adminMember.PowerLevel != 50 {
		t.Errorf("Admin PowerLevel: got %d, want 50", *adminMember.PowerLevel)
	}

	// Non-admins should have nil PowerLevel.
	if result.MemberMap[MakeUserID("user1")].PowerLevel != nil {
		t.Error("Non-admin member user1 should have nil PowerLevel")
	}
	if result.MemberMap[MakeUserID("user3")].PowerLevel != nil {
		t.Error("Non-admin member user3 should have nil PowerLevel")
	}
}

func TestChannelMembersToChatMembers_Empty(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	result := client.channelMembersToChatMembers(model.ChannelMembers{})

	if !result.IsFull {
		t.Error("IsFull should be true even for empty list")
	}
	if result.TotalMemberCount != 0 {
		t.Errorf("TotalMemberCount: got %d, want 0", result.TotalMemberCount)
	}
	if len(result.MemberMap) != 0 {
		t.Errorf("MemberMap length: got %d, want 0", len(result.MemberMap))
	}
}

func TestMmUserToUserInfo(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	user := &model.User{
		Id:        "user123",
		Username:  "johndoe",
		Nickname:  "",
		FirstName: "John",
		LastName:  "Doe",
	}

	info := client.mmUserToUserInfo(user)

	if info.Name == nil {
		t.Fatal("Name should not be nil")
	}
	if *info.Name != "johndoe (MM)" {
		t.Errorf("Name: got %q, want %q", *info.Name, "johndoe (MM)")
	}
	if len(info.Identifiers) != 1 {
		t.Fatalf("Identifiers length: got %d, want 1", len(info.Identifiers))
	}
	if info.Identifiers[0] != "kchat:user123" {
		t.Errorf("Identifier: got %q, want %q", info.Identifiers[0], "kchat:user123")
	}
}

func TestMmUserToUserInfo_Avatar(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	user := &model.User{
		Id:                "user123",
		Username:          "johndoe",
		LastPictureUpdate: 1700000000000,
	}

	info := client.mmUserToUserInfo(user)

	if info.Avatar == nil {
		t.Fatal("Avatar should not be nil")
	}
	if info.Avatar.ID == "" {
		t.Error("Avatar ID should not be empty")
	}
	if info.Avatar.Get == nil {
		t.Error("Avatar Get function should not be nil")
	}
}

func TestMmUserToUserInfo_AvatarID_Format(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	user := &model.User{
		Id:                "user456",
		Username:          "jane",
		LastPictureUpdate: 1700000000000,
	}

	info := client.mmUserToUserInfo(user)

	expectedID := networkid.AvatarID("user456_1700000000000")
	if info.Avatar.ID != expectedID {
		t.Errorf("Avatar ID: got %q, want %q", info.Avatar.ID, expectedID)
	}
}

func TestMmUserToUserInfo_AvatarCacheInvalidation(t *testing.T) {
	t.Parallel()
	client := newTestClient()

	user1 := &model.User{Id: "user789", Username: "bob", LastPictureUpdate: 1000}
	user2 := &model.User{Id: "user789", Username: "bob", LastPictureUpdate: 2000}

	info1 := client.mmUserToUserInfo(user1)
	info2 := client.mmUserToUserInfo(user2)

	if info1.Avatar.ID == info2.Avatar.ID {
		t.Error("Avatar IDs should differ when LastPictureUpdate changes")
	}
}

// TestChannelMembersToChatMembers_NilMembers verifies that nil ChannelMembers
// (as opposed to empty) does not cause a panic.
func TestChannelMembersToChatMembers_NilMembers(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	result := client.channelMembersToChatMembers(nil)

	if !result.IsFull {
		t.Error("IsFull should be true even for nil members")
	}
	if result.TotalMemberCount != 0 {
		t.Errorf("TotalMemberCount: got %d, want 0", result.TotalMemberCount)
	}
	if len(result.MemberMap) != 0 {
		t.Errorf("MemberMap length: got %d, want 0", len(result.MemberMap))
	}
}

// TestMmUserToUserInfo_ZeroLastPictureUpdate verifies that a user with
// LastPictureUpdate=0 still gets a valid avatar ID (even if the timestamp is zero).
func TestMmUserToUserInfo_ZeroLastPictureUpdate(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	user := &model.User{
		Id:                "user000",
		Username:          "zeroavatar",
		LastPictureUpdate: 0,
	}

	info := client.mmUserToUserInfo(user)

	if info.Avatar == nil {
		t.Fatal("Avatar should not be nil even with zero LastPictureUpdate")
	}
	expectedID := networkid.AvatarID("user000_0")
	if info.Avatar.ID != expectedID {
		t.Errorf("Avatar ID: got %q, want %q", info.Avatar.ID, expectedID)
	}
}

// TestChannelToChatInfo_PrivateChannel verifies that private channels are
// treated like public channels (RoomTypeDefault).
func TestChannelToChatInfo_PrivateChannel(t *testing.T) {
	t.Parallel()
	client := newTestClient()
	channel := &model.Channel{
		Id:          "priv123",
		Type:        model.ChannelTypePrivate,
		DisplayName: "Secret Room",
		Header:      "Private matters",
	}
	members := model.ChannelMembers{
		{UserId: "user1", ChannelId: "priv123"},
	}

	info := client.channelToChatInfo(channel, members)

	if info.Type == nil {
		t.Fatal("Type should not be nil")
	}
	if *info.Type != database.RoomTypeDefault {
		t.Errorf("Type: got %q, want %q (private channels use default)", *info.Type, database.RoomTypeDefault)
	}
	if info.Name == nil || *info.Name != "Secret Room" {
		t.Errorf("Name: got %v, want %q", info.Name, "Secret Room")
	}
	if info.Topic == nil || *info.Topic != "Private matters" {
		t.Errorf("Topic: got %v, want %q", info.Topic, "Private matters")
	}
}
