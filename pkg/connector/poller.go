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
	"sort"
	"strconv"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/simplevent"

	"github.com/mattermost/mattermost/server/public/model"
)

// The kChat backend does not reliably broadcast post events to the Pusher
// socket (only client-to-client presence messages like typing arrive), so
// the poller fetches channel posts periodically and relays new, edited and
// deleted posts into the bridge event pipeline.

type channelWatermark struct {
	CreateAt int64 `json:"create"`
	EditAt   int64 `json:"edit"`
}

func (wm *channelWatermark) max() int64 {
	if wm.EditAt > wm.CreateAt {
		return wm.EditAt
	}
	return wm.CreateAt
}

// pollerKeyPrefix namespaces the KV entries storing per-channel watermarks.
const pollerKeyPrefix = "kchat-poll-wm:"

// watermarkKey builds the per-login, per-channel KV key for poll watermarks.
func (m *KChatClient) watermarkKey(channelID string) database.Key {
	return database.Key(pollerKeyPrefix + string(m.userLogin.ID) + ":" + channelID)
}

// legacyWatermarkKey is the pre-multi-login watermark key format.
func legacyWatermarkKey(channelID string) database.Key {
	return database.Key(pollerKeyPrefix + channelID)
}

// loadWatermark reads the login-scoped watermark, falling back to the legacy
// global key for watermarks persisted before multi-login support.
func (m *KChatClient) loadWatermark(ctx context.Context, channelID string) *channelWatermark {
	wm := &channelWatermark{}
	raw := ""
	if m.connector.Bridge.DB != nil {
		raw = m.connector.Bridge.DB.KV.Get(ctx, m.watermarkKey(channelID))
		if raw == "" {
			raw = m.connector.Bridge.DB.KV.Get(ctx, legacyWatermarkKey(channelID))
		}
	}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), wm)
	}
	return wm
}

// registerPolledChannel adds a channel to the poller and loads its watermark.
func (m *KChatClient) registerPolledChannel(ctx context.Context, channelID string) {
	m.pollMu.Lock()
	defer m.pollMu.Unlock()
	if m.pollWatermarks == nil {
		m.pollWatermarks = make(map[string]*channelWatermark)
	}
	if _, ok := m.pollWatermarks[channelID]; ok {
		return
	}
	wm := m.loadWatermark(ctx, channelID)
	m.pollWatermarks[channelID] = wm
}

// pollLoop polls all registered channels until the client stops.
func (m *KChatClient) pollLoop(ctx context.Context) {
	interval := m.connector.Config.PollInterval
	if interval == 0 {
		m.log.Warn().Msg("Post polling disabled (poll_interval: 0); no messages will be relayed")
		return
	}
	if interval < 0 {
		interval = 5
	}
	m.log.Info().Int("interval_s", interval).Msg("Starting kChat post poller")
	tickerInterval := time.Duration(interval) * time.Second
	ticker := time.NewTicker(tickerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.pollOnce(ctx)
			// Respect the kChat rate limit (60 req/min): stretch the tick
			// when many channels are registered (members poll adds one more
			// request per channel).
			m.pollMu.Lock()
			n := len(m.pollWatermarks)
			m.pollMu.Unlock()
			stretched := time.Duration(interval) * time.Second
			if n > 1 {
				needed := time.Duration(n) * 1500 * time.Millisecond
				if needed > stretched {
					stretched = needed
				}
			}
			if stretched != tickerInterval {
				tickerInterval = stretched
				ticker.Reset(stretched)
				m.log.Debug().Dur("interval", stretched).Int("channels", n).Msg("Adjusted poll interval")
			}
		}
	}
}

// pollOnce polls every registered channel once. Member (read receipt) state
// is polled at a lower rate to stay within the kChat API rate limit.
func (m *KChatClient) pollOnce(ctx context.Context) {
	m.pollMu.Lock()
	channels := make([]string, 0, len(m.pollWatermarks))
	for id := range m.pollWatermarks {
		channels = append(channels, id)
	}
	m.pollMu.Unlock()

	memberTick := m.connector.Config.PollInterval * 4
	if memberTick < 15 {
		memberTick = 15
	}
	pollMembers := time.Since(m.lastMemberPoll) >= time.Duration(memberTick)*time.Second
	if pollMembers {
		m.lastMemberPoll = time.Now()
	}

	for _, channelID := range channels {
		select {
		case <-m.stopChan:
			return
		case <-ctx.Done():
			return
		default:
		}
		if err := m.pollChannel(ctx, channelID); err != nil {
			m.log.Warn().Err(err).Str("channel_id", channelID).Msg("Channel poll failed")
		}
		if pollMembers {
			if members, _, err := m.client.GetChannelMembers(ctx, channelID, 0, 200, ""); err != nil {
				m.log.Warn().Err(err).Str("channel_id", channelID).Msg("Failed to poll channel members")
			} else {
				m.relayReadReceipts(ctx, channelID, members)
			}
		}
	}
}

// pollChannel fetches recent posts for a channel and relays everything new.
func (m *KChatClient) pollChannel(ctx context.Context, channelID string) error {
	m.pollMu.Lock()
	wm, ok := m.pollWatermarks[channelID]
	m.pollMu.Unlock()
	if !ok {
		return fmt.Errorf("channel not registered")
	}

	m.baselineMu.Lock()
	if m.baselinedChannels == nil {
		m.baselinedChannels = make(map[string]bool)
	}
	_, baselined := m.baselinedChannels[channelID]
	if !baselined {
		m.baselinedChannels[channelID] = true
	}
	m.baselineMu.Unlock()
	if !baselined && wm.CreateAt == 0 && wm.EditAt == 0 {
		// First poll with no persisted watermark: establish the baseline from
		// the newest post instead of relaying history. History delivery is
		// the backfill path's job. A persisted watermark always wins so
		// messages sent during downtime are relayed after a restart.
		resp0, err := m.rawGetChannelPosts(ctx, channelID, 1)
		if err != nil {
			m.baselineMu.Lock()
			delete(m.baselinedChannels, channelID)
			m.baselineMu.Unlock()
			return err
		}
		for _, post := range resp0.Posts {
			if post.CreateAt > wm.CreateAt {
				wm.CreateAt = post.CreateAt
			}
			if post.EditAt > wm.EditAt {
				wm.EditAt = post.EditAt
			}
		}
		if raw, err := json.Marshal(wm); err == nil && m.connector.Bridge.DB != nil {
			m.connector.Bridge.DB.KV.Set(ctx, m.watermarkKey(channelID), string(raw))
		}
		m.log.Info().Str("channel_id", channelID).Int64("baseline", wm.max()).Msg("Baselined channel watermark")
	}

	resp, err := m.rawGetChannelPosts(ctx, channelID, 50)
	if err != nil {
		return err
	}

	// Posts sorted ascending by create_at so events queue in order.
	ids := make([]string, 0, len(resp.Posts))
	for id := range resp.Posts {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return resp.Posts[ids[i]].CreateAt < resp.Posts[ids[j]].CreateAt
	})

	newCreate := wm.CreateAt
	newEdit := wm.EditAt
	relayed := 0

	for _, id := range ids {
		post := resp.Posts[id]

		if post.DeleteAt > 0 {
			// Deleted post: relay removal unless the bridge deleted it itself.
			if post.DeleteAt > wm.EditAt && !m.seenEcho(echoKindDelete, post.Id) && m.hasRelayed(echoKindRelayPost, post.Id) {
				m.relayPostDelete(ctx, post)
				relayed++
			}
			if post.DeleteAt > newEdit {
				newEdit = post.DeleteAt
			}
			continue
		}

		if post.CreateAt > wm.CreateAt {
			// New post: relay unless already relayed via websocket or echo.
			if !m.seenEcho(echoKindPost, post.Id) && !m.hasRelayed(echoKindRelayPost, post.Id) {
				m.relayPost(ctx, post)
				relayed++
			}
			m.markRelayed(echoKindRelayPost, post.Id)
			if post.CreateAt > newCreate {
				newCreate = post.CreateAt
			}
		} else if post.EditAt > wm.EditAt {
			// Edited post. Dedup keys include the edit timestamp so later
			// edits of the same post are not swallowed by the marker window.
			editKey := post.Id + ":" + strconv.FormatInt(post.EditAt, 10)
			if !m.seenEcho(echoKindEdit, post.Id) && !m.hasRelayed(echoKindRelayEdit, editKey) {
				m.relayPostEdit(ctx, post)
				relayed++
			}
			m.markRelayed(echoKindRelayEdit, editKey)
			if post.EditAt > newEdit {
				newEdit = post.EditAt
			}
		}
	}

	if newCreate != wm.CreateAt || newEdit != wm.EditAt {
		wm.CreateAt = newCreate
		wm.EditAt = newEdit
		if raw, err := json.Marshal(wm); err == nil && m.connector.Bridge.DB != nil {
			m.connector.Bridge.DB.KV.Set(ctx, m.watermarkKey(channelID), string(raw))
		}
	}
	if relayed > 0 {
		m.log.Debug().Str("channel_id", channelID).Int("relayed", relayed).Msg("Polled channel posts")
	}
	return nil
}

// rawGetChannelPosts fetches the recent posts of a channel via the tolerant
// raw API path (the Mattermost SDK's PostList unmarshalling has the same PHP
// quirks as elsewhere).
func (m *KChatClient) rawGetChannelPosts(ctx context.Context, channelID string, perPage int) (*model.PostList, error) {
	data, err := rawRequest(ctx, m.client, "GET",
		"/channels/"+channelID+"/posts?per_page="+strconv.Itoa(perPage), nil)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse posts response: %w", err)
	}
	fixPHPObjectArrays(doc)
	cleaned, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	list := &model.PostList{}
	if err := json.Unmarshal(cleaned, list); err != nil {
		return nil, fmt.Errorf("failed to decode posts response: %w", err)
	}
	return list, nil
}

// relayPost queues a new post as a Matrix message event.
func (m *KChatClient) relayPost(ctx context.Context, post *model.Post) {
	if post.Type != "" && post.Type != model.PostTypeDefault {
		return
	}
	m.log.Debug().Str("post_id", post.Id).Str("channel_id", post.ChannelId).Msg("Relaying polled post")
	m.markRelayed(echoKindRelayPost, post.Id)
	m.eventSender.QueueRemoteEvent(m.userLogin, &simplevent.Message[*model.Post]{
		EventMeta: simplevent.EventMeta{
			Type: bridgev2.RemoteEventMessage,
			LogContext: func(c zerolog.Context) zerolog.Context {
				return c.Str("post_id", post.Id).Str("channel_id", post.ChannelId)
			},
			PortalKey:    makePortalKey(m.userLogin, post.ChannelId),
			Sender:       m.senderFor(post.UserId),
			Timestamp:    time.UnixMilli(post.CreateAt),
			CreatePortal: true,
		},
		ID:   MakeMessageID(post.Id),
		Data: post,
		ConvertMessageFunc: func(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, data *model.Post) (*bridgev2.ConvertedMessage, error) {
			return m.convertPostToMatrix(data), nil
		},
	})
}

// relayPostEdit queues a post edit as a Matrix edit event.
func (m *KChatClient) relayPostEdit(ctx context.Context, post *model.Post) {
	m.log.Debug().Str("post_id", post.Id).Str("channel_id", post.ChannelId).Msg("Relaying polled edit")
	m.eventSender.QueueRemoteEvent(m.userLogin, &simplevent.Message[*model.Post]{
		EventMeta: simplevent.EventMeta{
			Type: bridgev2.RemoteEventEdit,
			LogContext: func(c zerolog.Context) zerolog.Context {
				return c.Str("post_id", post.Id).Str("channel_id", post.ChannelId)
			},
			PortalKey: makePortalKey(m.userLogin, post.ChannelId),
			Sender:    m.senderFor(post.UserId),
			Timestamp: time.UnixMilli(post.EditAt),
		},
		TargetMessage: MakeMessageID(post.Id),
		Data:          post,
		ConvertEditFunc: func(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, existing []*database.Message, data *model.Post) (*bridgev2.ConvertedEdit, error) {
			return m.convertEditToMatrix(data, existing), nil
		},
	})
}

// relayPostDelete queues a post deletion as a Matrix redaction event.
func (m *KChatClient) relayPostDelete(ctx context.Context, post *model.Post) {
	m.log.Debug().Str("post_id", post.Id).Str("channel_id", post.ChannelId).Msg("Relaying polled delete")
	m.markRelayed(echoKindRelayDel, post.Id)
	m.eventSender.QueueRemoteEvent(m.userLogin, &simplevent.MessageRemove{
		EventMeta: simplevent.EventMeta{
			Type: bridgev2.RemoteEventMessageRemove,
			LogContext: func(c zerolog.Context) zerolog.Context {
				return c.Str("post_id", post.Id).Str("channel_id", post.ChannelId)
			},
			PortalKey: makePortalKey(m.userLogin, post.ChannelId),
			Sender:    m.senderFor(post.UserId),
			Timestamp: time.UnixMilli(post.DeleteAt),
		},
		TargetMessage: MakeMessageID(post.Id),
	})
}
