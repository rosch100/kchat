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

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/id"
)

// Shared (person-independent) deployment helpers: auto-joining public team
// channels for a dedicated kChat bot account and inviting configured Matrix
// users into the shared portal rooms.

// rawGetChannel fetches a single channel with PHP quirks fixed.
func (m *KChatClient) rawGetChannel(ctx context.Context, channelID string) (*model.Channel, error) {
	data, err := rawRequest(ctx, m.client, http.MethodGet, "/channels/"+channelID, nil)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse channel response: %w", err)
	}
	fixPHPObjectArrays(doc)
	cleaned, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	channel := &model.Channel{}
	if err := json.Unmarshal(cleaned, channel); err != nil {
		return nil, fmt.Errorf("failed to decode channel response: %w", err)
	}
	return channel, nil
}

// joinChannel adds the logged-in kChat account to a channel. Already-being-a-
// member responses are treated as success.
func (m *KChatClient) joinChannel(ctx context.Context, channelID string) error {
	payload, err := json.Marshal(map[string]any{"user_id": m.userID})
	if err != nil {
		return err
	}
	_, err = rawRequest(ctx, m.client, http.MethodPost, "/channels/"+channelID+"/members", payload)
	return err
}

// queueChannelResync queues a ChatResync for a channel so bridgev2 creates
// or updates the shared portal room.
func (m *KChatClient) queueChannelResync(ctx context.Context, channel *model.Channel) {
	members, _, err := m.client.GetChannelMembers(ctx, channel.Id, 0, 200, "")
	if err != nil {
		m.log.Warn().Err(err).Str("channel_id", channel.Id).Msg("Failed to get channel members for resync")
		return
	}
	m.eventSender.QueueRemoteEvent(m.userLogin, &simplevent.ChatResync{
		EventMeta: simplevent.EventMeta{
			Type:      bridgev2.RemoteEventChatResync,
			PortalKey: m.makePortalKey(channel.Id),
			LogContext: func(c zerolog.Context) zerolog.Context {
				return c.Str("channel_id", channel.Id).Str("channel_name", channel.Name)
			},
			CreatePortal: true,
		},
		ChatInfo: m.channelToChatInfo(channel, members),
	})
}

// inviteRelayUsers invites the configured Matrix users into every shared
// portal room. Intended for split_portals: false team deployments; the bot
// runs it after the channel sync and periodically from the poller.
func (m *KChatClient) inviteRelayUsers(ctx context.Context) {
	if m.connector == nil || len(m.connector.Config.RelayInviteUsers) == 0 {
		return
	}
	if m.connector.Bridge == nil || m.connector.Bridge.DB == nil || m.connector.Bridge.Bot == nil {
		return
	}
	portals, err := m.connector.Bridge.DB.Portal.GetAllWithoutReceiver(ctx)
	if err != nil {
		m.log.Warn().Err(err).Msg("Failed to list shared portals for invites")
		return
	}
	for _, portal := range portals {
		if portal.MXID == "" {
			continue
		}
		for _, userMXID := range m.connector.Config.RelayInviteUsers {
			if err := m.connector.Bridge.Bot.EnsureInvited(ctx, portal.MXID, id.UserID(userMXID)); err != nil {
				m.log.Warn().Err(err).
					Str("room_id", string(portal.MXID)).
					Str("user", userMXID).
					Msg("Failed to invite user to shared portal")
			}
		}
	}
}
