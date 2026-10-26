// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/bridgev2/status"
	"maunium.net/go/mautrix/event"
)

// remoteEventSender is an interface for queuing remote events. This allows
// tests to inject a mock instead of requiring a full bridgev2.Bridge.
type remoteEventSender interface {
	QueueRemoteEvent(login *bridgev2.UserLogin, evt bridgev2.RemoteEvent)
}

// bridgeEventSender is the production implementation that delegates to the bridge.
type bridgeEventSender struct {
	bridge *bridgev2.Bridge
}

func (b *bridgeEventSender) QueueRemoteEvent(login *bridgev2.UserLogin, evt bridgev2.RemoteEvent) {
	b.bridge.QueueRemoteEvent(login, evt)
}

// KChatClient represents a single authenticated kChat user connection.
type KChatClient struct {
	connector   *KChatConnector
	userLogin   *bridgev2.UserLogin
	eventSender remoteEventSender

	client    *model.Client4
	wsSocket  *pusherSocket
	userID    string
	teamID    string
	serverURL string

	stopOnce sync.Once
	stopChan chan struct{}
	log      zerolog.Logger
}

var (
	_ bridgev2.NetworkAPI                    = (*KChatClient)(nil)
	_ bridgev2.EditHandlingNetworkAPI        = (*KChatClient)(nil)
	_ bridgev2.ReactionHandlingNetworkAPI    = (*KChatClient)(nil)
	_ bridgev2.RedactionHandlingNetworkAPI   = (*KChatClient)(nil)
	_ bridgev2.ReadReceiptHandlingNetworkAPI = (*KChatClient)(nil)
	_ bridgev2.TypingHandlingNetworkAPI      = (*KChatClient)(nil)
)

// NewKChatClient creates a new client from an existing user login.
func NewKChatClient(login *bridgev2.UserLogin, connector *KChatConnector) *KChatClient {
	log := login.Log.With().Str("component", "mm_client").Logger()
	mc := &KChatClient{
		connector:   connector,
		userLogin:   login,
		eventSender: &bridgeEventSender{bridge: connector.Bridge},
		stopChan:    make(chan struct{}),
		log:         log,
	}
	meta := login.Metadata.(*UserLoginMetadata)
	if meta == nil {
		return mc
	}
	// Always restore the MM user ID so IsThisUser() works for all login
	// types, including lightweight double-puppet-only logins.
	mc.userID = meta.UserID
	mc.teamID = meta.TeamID
	mc.serverURL = meta.ServerURL
	if meta.Token != "" && !meta.DoublePuppetOnly {
		mc.client = model.NewAPIv4Client(meta.ServerURL)
		mc.client.SetToken(meta.Token)
	}
	return mc
}

// Connect implements bridgev2.NetworkAPI. It does not return an error;
// connection errors are reported via BridgeState.
func (m *KChatClient) Connect(ctx context.Context) {
	// Double-puppet-only logins have no MM client and don't need a connection.
	// Re-register in the dpLogins map so the mapping survives restarts.
	meta, _ := m.userLogin.Metadata.(*UserLoginMetadata)
	if meta != nil && meta.DoublePuppetOnly {
		m.connector.dpLoginsMu.Lock()
		m.connector.dpLogins[meta.UserID] = m.userLogin.ID
		m.connector.dpLoginsMu.Unlock()
		m.log.Info().
			Str("mm_user_id", meta.UserID).
			Str("matrix_mxid", string(m.userLogin.UserMXID)).
			Msg("Restored double-puppet-only login")
		return
	}

	if m.client == nil {
		m.log.Warn().Msg("Client not initialized, login first")
		m.userLogin.BridgeState.Send(status.BridgeState{
			StateEvent: status.StateBadCredentials,
			Error:      "mm-not-logged-in",
			Message:    "Not logged in to kChat",
		})
		return
	}

	m.log.Info().Str("server_url", m.serverURL).Msg("Connecting to kChat")

	me, _, err := m.client.GetMe(ctx, "")
	if err != nil {
		m.log.Error().Err(err).Msg("Failed to verify kChat session")
		m.userLogin.BridgeState.Send(status.BridgeState{
			StateEvent: status.StateBadCredentials,
			Error:      "mm-token-invalid",
			Message:    "kChat authentication token is invalid",
		})
		return
	}
	m.userID = me.Id
	m.log.Info().Str("user_id", me.Id).Str("username", me.Username).Msg("Authenticated")

	if m.teamID == "" {
		teams, _, err := m.client.GetTeamsForUser(ctx, m.userID, "")
		if err != nil {
			m.log.Error().Err(err).Msg("Failed to get teams")
			m.userLogin.BridgeState.Send(status.BridgeState{
				StateEvent: status.StateUnknownError,
				Error:      "mm-teams-failed",
				Message:    "Failed to get teams",
			})
			return
		}
		if len(teams) > 0 {
			m.teamID = teams[0].Id
		}
	}

	if err := m.connectWebSocket(); err != nil {
		m.log.Error().Err(err).Msg("WebSocket connection failed")
		m.userLogin.BridgeState.Send(status.BridgeState{
			StateEvent: status.StateTransientDisconnect,
			Error:      "mm-ws-failed",
			Message:    "WebSocket connection failed",
		})
		return
	}

	m.userLogin.BridgeState.Send(status.BridgeState{
		StateEvent: status.StateConnected,
	})

	// Sync existing channels to create portal rooms in Matrix.
	go m.syncChannels(ctx)
}

func (m *KChatClient) connectWebSocket() error {
	if m.teamID == "" {
		return fmt.Errorf("cannot subscribe without a team ID")
	}
	socket, err := newPusherSocket(m)
	if err != nil {
		return fmt.Errorf("failed to create websocket client: %w", err)
	}
	socket.setPendingChannels(m.teamID, m.userID)
	m.wsSocket = socket
	go func() {
		if err := socket.Listen(); err != nil {
			m.log.Error().Err(err).Msg("WebSocket connection failed")
			m.handleWebSocketDisconnect()
		}
	}()

	return nil
}

// httpToWS converts an HTTP(S) URL to a WS(S) URL.
func httpToWS(url string) string {
	if strings.HasPrefix(url, "https://") {
		return "wss://" + strings.TrimPrefix(url, "https://")
	}
	if strings.HasPrefix(url, "http://") {
		return "ws://" + strings.TrimPrefix(url, "http://")
	}
	return url
}

func (m *KChatClient) listenWebSocket() {
	for {
		select {
		case <-m.stopChan:
			return
		case event, ok := <-m.wsSocket.EventChannel:
			if !ok {
				m.log.Warn().Msg("WebSocket event channel closed, reconnecting")
				m.handleWebSocketDisconnect()
				return
			}
			if event == nil {
				continue
			}
			m.handleEvent(event)
		}
	}
}

func (m *KChatClient) handleWebSocketDisconnect() {
	m.userLogin.BridgeState.Send(status.BridgeState{
		StateEvent: status.StateTransientDisconnect,
		Error:      "mm-ws-disconnected",
		Message:    "WebSocket disconnected, reconnecting",
	})

	if err := m.connectWebSocket(); err != nil {
		m.log.Error().Err(err).Msg("Failed to reconnect WebSocket")
		m.userLogin.BridgeState.Send(status.BridgeState{
			StateEvent: status.StateUnknownError,
			Error:      "mm-ws-reconnect-failed",
			Message:    "Failed to reconnect WebSocket",
		})
	} else {
		m.userLogin.BridgeState.Send(status.BridgeState{
			StateEvent: status.StateConnected,
		})
	}
}

// syncChannels fetches all kChat channels the user is a member of
// (including DMs and group DMs) and queues ChatResync events so the bridge
// creates portal rooms in Matrix.
func (m *KChatClient) syncChannels(ctx context.Context) {
	channelMap := make(map[string]*model.Channel)

	// Fetch team channels if we have a team ID.
	if m.teamID != "" {
		channels, _, err := m.client.GetChannelsForTeamForUser(ctx, m.teamID, m.userID, false, "")
		if err != nil {
			m.log.Error().Err(err).Msg("Failed to fetch team channels for sync")
		} else {
			for _, ch := range channels {
				channelMap[ch.Id] = ch
			}
		}
	}

	// Fetch all channels including DMs/group DMs (cross-team).
	allChannels, _, err := m.client.GetChannelsForUserWithLastDeleteAt(ctx, m.userID, 0)
	if err != nil {
		m.log.Error().Err(err).Msg("Failed to fetch all user channels for sync")
		if len(channelMap) == 0 {
			return
		}
	} else {
		for _, ch := range allChannels {
			channelMap[ch.Id] = ch
		}
	}

	m.log.Info().Int("count", len(channelMap)).Msg("Syncing channels")

	for _, ch := range channelMap {
		m.log.Debug().
			Str("channel_id", ch.Id).
			Str("channel_name", ch.Name).
			Str("channel_type", string(ch.Type)).
			Msg("Syncing channel")

		members, _, err := m.client.GetChannelMembers(ctx, ch.Id, 0, 200, "")
		if err != nil {
			m.log.Warn().Err(err).Str("channel_id", ch.Id).Msg("Failed to get channel members")
			continue
		}

		chatInfo := m.channelToChatInfo(ch, members)

		var checkBackfill func(ctx context.Context, latestMessage *database.Message) (bool, error)
		var latestMessageTS time.Time
		if m.connector.Config.BackfillEnabled && ch.LastPostAt > 0 {
			lastPostAt := ch.LastPostAt
			latestMessageTS = time.UnixMilli(lastPostAt)
			checkBackfill = func(_ context.Context, latestMessage *database.Message) (bool, error) {
				if latestMessage == nil {
					return true, nil
				}
				return latestMessage.Timestamp.Before(time.UnixMilli(lastPostAt)), nil
			}
		}

		m.eventSender.QueueRemoteEvent(m.userLogin, &simplevent.ChatResync{
			EventMeta: simplevent.EventMeta{
				Type:      bridgev2.RemoteEventChatResync,
				PortalKey: makePortalKey(m.userLogin, ch.Id),
				LogContext: func(c zerolog.Context) zerolog.Context {
					return c.Str("channel_id", ch.Id).Str("channel_name", ch.Name)
				},
				CreatePortal: true,
			},
			ChatInfo:               chatInfo,
			LatestMessageTS:        latestMessageTS,
			CheckNeedsBackfillFunc: checkBackfill,
		})
	}

	m.log.Info().Msg("Channel sync complete")
}

// Disconnect closes the WebSocket connection and stops the client's event loop.
func (m *KChatClient) Disconnect() {
	m.stopOnce.Do(func() {
		close(m.stopChan)
	})
	if m.wsSocket != nil {
		_ = m.wsSocket.conn.Close()
		m.wsSocket = nil
	}
}

// IsLoggedIn reports whether the client holds a valid authentication token.
func (m *KChatClient) IsLoggedIn() bool {
	return m.client != nil && m.client.AuthToken != ""
}

func (m *KChatClient) LogoutRemote(ctx context.Context) {
	if m.client != nil {
		_, _ = m.client.Logout(ctx)
	}
	m.Disconnect()
}

// IsThisUser reports whether the given network user ID matches this client's kChat user.
func (m *KChatClient) IsThisUser(_ context.Context, userID networkid.UserID) bool {
	return string(userID) == m.userID
}

func (m *KChatClient) GetChatInfo(ctx context.Context, portal *bridgev2.Portal) (*bridgev2.ChatInfo, error) {
	channelID := ParsePortalID(portal.ID)
	channel, _, err := m.client.GetChannel(ctx, channelID, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get channel info: %w", err)
	}

	members, _, err := m.client.GetChannelMembers(ctx, channelID, 0, 200, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get channel members: %w", err)
	}

	return m.channelToChatInfo(channel, members), nil
}

func (m *KChatClient) GetUserInfo(ctx context.Context, ghost *bridgev2.Ghost) (*bridgev2.UserInfo, error) {
	mmUserID := ParseUserID(ghost.ID)
	user, err := rawGetUser(ctx, m.client, mmUserID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}
	return m.mmUserToUserInfo(user), nil
}

func (m *KChatClient) GetCapabilities(_ context.Context, _ *bridgev2.Portal) *event.RoomFeatures {
	return &event.RoomFeatures{
		Formatting: event.FormattingFeatureMap{
			event.FmtBold:          event.CapLevelFullySupported,
			event.FmtItalic:        event.CapLevelFullySupported,
			event.FmtStrikethrough: event.CapLevelFullySupported,
			event.FmtInlineCode:    event.CapLevelFullySupported,
			event.FmtCodeBlock:     event.CapLevelFullySupported,
			event.FmtBlockquote:    event.CapLevelFullySupported,
			event.FmtInlineLink:    event.CapLevelFullySupported,
			event.FmtUserLink:      event.CapLevelFullySupported,
			event.FmtUnorderedList: event.CapLevelFullySupported,
			event.FmtOrderedList:   event.CapLevelFullySupported,
			event.FmtHeaders:       event.CapLevelFullySupported,
		},
		File: event.FileFeatureMap{
			event.MsgImage: {
				MimeTypes: map[string]event.CapabilitySupportLevel{
					"image/*": event.CapLevelFullySupported,
				},
				MaxSize: 100 * 1024 * 1024,
				Caption: event.CapLevelFullySupported,
			},
			event.MsgVideo: {
				MimeTypes: map[string]event.CapabilitySupportLevel{
					"video/*": event.CapLevelFullySupported,
				},
				MaxSize: 100 * 1024 * 1024,
				Caption: event.CapLevelFullySupported,
			},
			event.MsgAudio: {
				MimeTypes: map[string]event.CapabilitySupportLevel{
					"audio/*": event.CapLevelFullySupported,
				},
				MaxSize: 100 * 1024 * 1024,
			},
			event.MsgFile: {
				MimeTypes: map[string]event.CapabilitySupportLevel{
					"*/*": event.CapLevelFullySupported,
				},
				MaxSize: 100 * 1024 * 1024,
			},
		},
		MaxTextLength:       16383,
		Reply:               event.CapLevelFullySupported,
		Edit:                event.CapLevelFullySupported,
		Delete:              event.CapLevelFullySupported,
		Reaction:            event.CapLevelFullySupported,
		ReadReceipts:        true,
		TypingNotifications: true,
	}
}
