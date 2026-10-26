// Copyright 2024-2026 Remi Philippe
// Copyright 2026 rosch
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/simplevent"

	"github.com/mattermost/mattermost/server/public/model"
)

// resolveIdentifier implements starting chats from Matrix. kChat has no
// /users/username/{name} route, so users are looked up via the search
// endpoint (GET /users?search=...) and matched exactly by username, email
// or nickname.

func (m *KChatClient) ResolveIdentifier(ctx context.Context, identifier string, createChat bool) (*bridgev2.ResolveIdentifierResponse, error) {
	if !m.IsLoggedIn() {
		return nil, bridgev2.ErrNotLoggedIn
	}
	user, err := m.lookupUser(ctx, identifier)
	if err != nil {
		return nil, err
	}
	resp := &bridgev2.ResolveIdentifierResponse{
		UserID:   MakeUserID(user.Id),
		UserInfo: m.userInfoFromUser(user),
	}
	if createChat {
		channel, err := m.createDirectChannel(ctx, user.Id)
		if err != nil {
			return nil, err
		}
		resp.Chat = &bridgev2.CreateChatResponse{
			PortalKey: m.makePortalKey(channel.Id),
		}
	}
	return resp, nil
}

// SearchUsers searches kChat users for the provisioning API contact search.
func (m *KChatClient) SearchUsers(ctx context.Context, query string) ([]*bridgev2.ResolveIdentifierResponse, error) {
	if !m.IsLoggedIn() {
		return nil, bridgev2.ErrNotLoggedIn
	}
	users, err := m.searchUsers(ctx, query)
	if err != nil {
		return nil, err
	}
	responses := make([]*bridgev2.ResolveIdentifierResponse, 0, len(users))
	for _, u := range users {
		responses = append(responses, &bridgev2.ResolveIdentifierResponse{
			UserID:   MakeUserID(u.Id),
			UserInfo: m.userInfoFromUser(u),
		})
	}
	return responses, nil
}

// lookupUser finds exactly one user for an identifier (username, email or nickname).
func (m *KChatClient) lookupUser(ctx context.Context, identifier string) (*model.User, error) {
	identifier = strings.TrimPrefix(identifier, "@")
	if i := strings.Index(identifier, ":"); i >= 0 {
		// Strip Matrix-style suffixes like :beeper.local.
		identifier = identifier[:i]
	}
	users, err := m.searchUsers(ctx, identifier)
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(identifier)
	for _, u := range users {
		if strings.EqualFold(u.Username, needle) ||
			(u.Email != "" && strings.EqualFold(u.Email, needle)) {
			return u, nil
		}
	}
	// Nicknames are not unique: accept a nickname match only when exactly
	// one user carries it, and only after username/email were considered.
	var nickMatch *model.User
	nickCount := 0
	for _, u := range users {
		if u.Nickname != "" && strings.EqualFold(u.Nickname, needle) {
			nickMatch = u
			nickCount++
		}
	}
	if nickCount == 1 {
		return nickMatch, nil
	}
	// No single-candidate fallback: fuzzy prefix hits (e.g. "bob" -> "bobby")
	// must not open DMs with the wrong person. Fuzzy lookup is SearchUsers' job.
	return nil, fmt.Errorf("no exact kChat user match for %q (%d candidates)", identifier, len(users))
}

// searchUsers calls the kChat user search endpoint.
func (m *KChatClient) searchUsers(ctx context.Context, term string) ([]*model.User, error) {
	data, err := rawRequest(ctx, m.client, http.MethodGet, "/users?search="+url.QueryEscape(term), nil)
	if err != nil {
		return nil, fmt.Errorf("user search failed: %w", err)
	}
	var doc []any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse user search response: %w", err)
	}
	for _, item := range doc {
		fixPHPObjectArrays(item)
	}
	cleaned, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	users := []*model.User{}
	if err := json.Unmarshal(cleaned, &users); err != nil {
		return nil, fmt.Errorf("failed to decode user search response: %w", err)
	}
	return users, nil
}

// createDirectChannel creates (or returns) the DM channel with a user.
func (m *KChatClient) createDirectChannel(ctx context.Context, otherUserID string) (*model.Channel, error) {
	payload, err := json.Marshal([]string{m.userID, otherUserID})
	if err != nil {
		return nil, err
	}
	data, err := rawRequest(ctx, m.client, http.MethodPost, "/channels/direct", payload)
	if err != nil {
		return nil, fmt.Errorf("failed to create DM channel: %w", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse DM channel response: %w", err)
	}
	fixPHPObjectArrays(doc)
	cleaned, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	channel := &model.Channel{}
	if err := json.Unmarshal(cleaned, channel); err != nil {
		return nil, fmt.Errorf("failed to decode DM channel response: %w", err)
	}
	if channel.Id == "" {
		return nil, fmt.Errorf("DM channel response has no channel ID: %s", string(cleaned))
	}
	// Register for polling so messages in the DM are received.
	m.registerPolledChannel(context.WithoutCancel(ctx), channel.Id)
	m.SubscribeChannel(channel.Id)
	return channel, nil
}

// userInfoFromUser converts a kChat user to bridgev2 UserInfo.
func (m *KChatClient) userInfoFromUser(user *model.User) *bridgev2.UserInfo {
	return m.mmUserToUserInfo(user)
}

// relayReadReceipts compares member last_viewed_at values with the tracked
// watermark and queues read receipt events for every increase.
func (m *KChatClient) relayReadReceipts(ctx context.Context, channelID string, members model.ChannelMembers) {
	m.readReceiptMu.Lock()
	if m.readReceiptMarks == nil {
		m.readReceiptMarks = make(map[string]map[string]time.Time)
	}
	channelMarks, ok := m.readReceiptMarks[channelID]
	firstObservation := !ok
	if !ok {
		channelMarks = make(map[string]time.Time)
		m.readReceiptMarks[channelID] = channelMarks
	}
	m.readReceiptMu.Unlock()

	for _, member := range members {
		if member.LastViewedAt <= 0 {
			continue
		}
		seen := channelMarks[member.UserId]
		viewed := time.UnixMilli(member.LastViewedAt)
		if !viewed.After(seen) {
			continue
		}
		channelMarks[member.UserId] = viewed
		if firstObservation {
			// First poll only establishes the baseline; emitting receipts for
			// pre-existing read state would spam warnings for unknown targets.
			continue
		}
		m.eventSender.QueueRemoteEvent(m.userLogin, &simplevent.Receipt{
			EventMeta: simplevent.EventMeta{
				Type: bridgev2.RemoteEventReadReceipt,
				LogContext: func(c zerolog.Context) zerolog.Context {
					return c.Str("channel_id", channelID).Str("reader", member.UserId)
				},
				PortalKey: m.makePortalKey(channelID),
				Sender:    m.senderFor(member.UserId),
			},
			ReadUpTo: viewed,
		})
	}
}

var _ bridgev2.IdentifierResolvingNetworkAPI = (*KChatClient)(nil)
var _ bridgev2.UserSearchingNetworkAPI = (*KChatClient)(nil)
