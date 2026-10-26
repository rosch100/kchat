// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mattermost/mattermost/server/public/model"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
)

// channelToChatInfo converts a kChat channel and its members to a bridgev2.ChatInfo.
func (m *KChatClient) channelToChatInfo(channel *model.Channel, members model.ChannelMembers) *bridgev2.ChatInfo {
	memberList := m.channelMembersToChatMembers(members)

	chatInfo := &bridgev2.ChatInfo{
		Members: memberList,
	}

	switch channel.Type {
	case model.ChannelTypeDirect:
		dmType := database.RoomTypeDM
		chatInfo.Type = &dmType
		// Set OtherUserID for 1:1 DMs.
		for _, member := range members {
			if member.UserId != m.userID {
				memberList.OtherUserID = MakeUserID(member.UserId)
				break
			}
		}
	case model.ChannelTypeGroup:
		groupType := database.RoomTypeGroupDM
		chatInfo.Type = &groupType
		if channel.DisplayName != "" {
			name := channel.DisplayName
			chatInfo.Name = &name
		}
	default:
		roomType := database.RoomTypeDefault
		chatInfo.Type = &roomType
		name := channel.DisplayName
		if name == "" {
			name = channel.Name
		}
		chatInfo.Name = &name
		if channel.Header != "" {
			chatInfo.Topic = &channel.Header
		}
	}

	return chatInfo
}

// channelMembersToChatMembers converts kChat channel members to bridgev2 member list.
func (m *KChatClient) channelMembersToChatMembers(members model.ChannelMembers) *bridgev2.ChatMemberList {
	memberMap := make(map[networkid.UserID]bridgev2.ChatMember, len(members))

	for _, member := range members {
		chatMember := bridgev2.ChatMember{
			EventSender: bridgev2.EventSender{
				Sender: MakeUserID(member.UserId),
			},
			Membership: event.MembershipJoin,
		}
		// Mark channel admins as moderators.
		if member.SchemeAdmin {
			chatMember.PowerLevel = func() *int {
				pl := 50
				return &pl
			}()
		}
		memberMap[MakeUserID(member.UserId)] = chatMember
	}

	return &bridgev2.ChatMemberList{
		IsFull:           true,
		TotalMemberCount: len(members),
		MemberMap:        memberMap,
	}
}

// mmUserToUserInfo converts a kChat user to a bridgev2.UserInfo.
func (m *KChatClient) mmUserToUserInfo(user *model.User) *bridgev2.UserInfo {
	name := m.connector.Config.FormatDisplayname(DisplaynameParams{
		Username:  user.Username,
		Nickname:  user.Nickname,
		FirstName: user.FirstName,
		LastName:  user.LastName,
	})

	info := &bridgev2.UserInfo{
		Identifiers: []string{
			fmt.Sprintf("kchat:%s", user.Id),
		},
		Name: &name,
	}

	avatarID := networkid.AvatarID(user.Id + "_" + strconv.FormatInt(user.LastPictureUpdate, 10))
	info.Avatar = &bridgev2.Avatar{
		ID: avatarID,
		Get: func(ctx context.Context) ([]byte, error) {
			data, _, err := m.client.GetProfileImage(ctx, user.Id, "")
			return data, err
		},
	}

	return info
}
