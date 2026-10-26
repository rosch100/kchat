// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/event"
)

// senderFor builds an EventSender for the given kChat user ID. If the
// user has a double puppet UserLogin registered, SenderLogin is set so the
// bridgev2 framework uses that user's double puppet intent instead of a ghost.
func (m *KChatClient) senderFor(mmUserID string) bridgev2.EventSender {
	sender := bridgev2.EventSender{
		Sender: MakeUserID(mmUserID),
	}
	if loginID, ok := m.connector.DoublePuppetLoginID(mmUserID); ok {
		sender.SenderLogin = loginID
	}
	return sender
}

// handleEvent dispatches a kChat WebSocket event to the appropriate handler.
func (m *KChatClient) handleEvent(evt *model.WebSocketEvent) {
	switch evt.EventType() {
	case model.WebsocketEventPosted:
		m.handlePosted(evt)
	case model.WebsocketEventPostEdited:
		m.handlePostEdited(evt)
	case model.WebsocketEventPostDeleted:
		m.handlePostDeleted(evt)
	case model.WebsocketEventReactionAdded:
		m.handleReactionAdded(evt)
	case model.WebsocketEventReactionRemoved:
		m.handleReactionRemoved(evt)
	case model.WebsocketEventTyping:
		m.handleTyping(evt)
	case model.WebsocketEventChannelViewed:
		m.handleChannelViewed(evt)
	default:
		m.log.Trace().Str("event_type", string(evt.EventType())).Msg("Unhandled event type")
	}
}

// parsePostedEvent extracts and validates a post from a WebSocket event,
// applying all echo prevention layers. Returns (nil, nil) to skip silently,
// (nil, err) to log an error, or (post, nil) to proceed.
func (m *KChatClient) parsePostedEvent(evt *model.WebSocketEvent) (*model.Post, error) {
	postJSON, ok := evt.GetData()["post"].(string)
	if !ok {
		return nil, fmt.Errorf("posted event missing post data")
	}

	var post model.Post
	if err := json.Unmarshal([]byte(postJSON), &post); err != nil {
		return nil, fmt.Errorf("failed to unmarshal post: %w", err)
	}

	// Echo prevention: skip posts sent by this bridge (tracked by post ID,
	// covers cases where kChat strips unknown props like mx_txn).
	if m.seenEcho(echoKindPost, post.Id) {
		return nil, nil
	}

	// Note: posts from the logged-in user are NOT skipped — the user may
	// write from kChat directly and expects those in Matrix. Bridge-sent
	// echoes are removed via the mx_txn transaction ID (set on send) or the
	// sent-post tracker above.

	// Echo prevention: skip non-default post types (system messages).
	if post.Type != "" && post.Type != model.PostTypeDefault {
		return nil, nil
	}

	// Echo prevention: skip posts from puppet bot users.
	if m.connector.IsPuppetUserID(post.UserId) {
		m.log.Debug().
			Str("post_id", post.Id).
			Str("user_id", post.UserId).
			Msg("Skipping puppet bot post (echo prevention)")
		return nil, nil
	}

	// Echo prevention: skip posts from usernames matching known bridge patterns.
	senderName, _ := evt.GetData()["sender_name"].(string)
	senderName = strings.TrimPrefix(senderName, "@")
	if senderName != "" && isBridgeUsername(senderName, m.connector.Config.BotPrefix) {
		m.log.Debug().
			Str("post_id", post.Id).
			Str("username", senderName).
			Msg("Skipping bridge username post (echo prevention)")
		return nil, nil
	}

	return &post, nil
}

// parsePostEditedEvent extracts and validates an edited post from a WebSocket event,
// applying echo prevention. Returns (nil, nil) to skip, (nil, err) for errors,
// or (post, nil) to proceed.
func (m *KChatClient) parsePostEditedEvent(evt *model.WebSocketEvent) (*model.Post, error) {
	postJSON, ok := evt.GetData()["post"].(string)
	if !ok {
		return nil, nil
	}

	var post model.Post
	if err := json.Unmarshal([]byte(postJSON), &post); err != nil {
		return nil, fmt.Errorf("failed to unmarshal edited post: %w", err)
	}

	// Echo prevention: skip edits performed by this bridge. Edits from kChat
	// (even of bridge-sent messages) are relayed; they are idempotent.
	if m.seenEcho(echoKindEdit, post.Id) {
		return nil, nil
	}

	// Echo prevention: skip edits from puppet bot users.
	if m.connector.IsPuppetUserID(post.UserId) {
		m.log.Debug().
			Str("post_id", post.Id).
			Str("user_id", post.UserId).
			Msg("Skipping puppet bot edit (echo prevention)")
		return nil, nil
	}

	// Echo prevention: skip edits from usernames matching known bridge patterns.
	senderName, _ := evt.GetData()["sender_name"].(string)
	senderName = strings.TrimPrefix(senderName, "@")
	if senderName != "" && isBridgeUsername(senderName, m.connector.Config.BotPrefix) {
		m.log.Debug().
			Str("post_id", post.Id).
			Str("username", senderName).
			Msg("Skipping bridge username edit (echo prevention)")
		return nil, nil
	}

	return &post, nil
}

// parsePostDeletedEvent extracts and validates a deleted post from a WebSocket event,
// applying echo prevention. Returns (nil, nil) to skip, (nil, err) for errors,
// or (post, nil) to proceed.
func (m *KChatClient) parsePostDeletedEvent(evt *model.WebSocketEvent) (*model.Post, error) {
	postJSON, ok := evt.GetData()["post"].(string)
	if !ok {
		return nil, nil
	}

	var post model.Post
	if err := json.Unmarshal([]byte(postJSON), &post); err != nil {
		return nil, fmt.Errorf("failed to unmarshal deleted post: %w", err)
	}

	// Echo prevention: skip deletes performed by this bridge.
	if m.seenEcho(echoKindDelete, post.Id) {
		return nil, nil
	}

	// Echo prevention: skip deletes from puppet bot users.
	if m.connector.IsPuppetUserID(post.UserId) {
		m.log.Debug().
			Str("post_id", post.Id).
			Str("user_id", post.UserId).
			Msg("Skipping puppet bot delete (echo prevention)")
		return nil, nil
	}

	// Echo prevention: skip deletes from usernames matching known bridge patterns.
	senderName, _ := evt.GetData()["sender_name"].(string)
	senderName = strings.TrimPrefix(senderName, "@")
	if senderName != "" && isBridgeUsername(senderName, m.connector.Config.BotPrefix) {
		m.log.Debug().
			Str("post_id", post.Id).
			Str("username", senderName).
			Msg("Skipping bridge username delete (echo prevention)")
		return nil, nil
	}

	return &post, nil
}

// parseReactionEvent extracts and validates a reaction from a WebSocket event.
// Returns (nil, nil) to skip, (nil, err) for errors, or (reaction, nil) to proceed.
func (m *KChatClient) parseReactionEvent(evt *model.WebSocketEvent) (*model.Reaction, error) {
	reactionJSON, ok := evt.GetData()["reaction"].(string)
	if !ok {
		return nil, nil
	}

	var reaction model.Reaction
	if err := json.Unmarshal([]byte(reactionJSON), &reaction); err != nil {
		return nil, fmt.Errorf("failed to unmarshal reaction: %w", err)
	}

	// Echo prevention: skip reactions added/removed by this bridge.
	if m.seenEcho(echoKindReaction, reaction.PostId+":"+reaction.EmojiName+":"+reaction.UserId) ||
		m.seenEcho(echoKindReactionRm, reaction.PostId+":"+reaction.EmojiName+":"+reaction.UserId) {
		return nil, nil
	}

	// Echo prevention: skip reactions from puppet bot users.
	if m.connector.IsPuppetUserID(reaction.UserId) {
		m.log.Debug().
			Str("post_id", reaction.PostId).
			Str("user_id", reaction.UserId).
			Str("emoji", reaction.EmojiName).
			Msg("Skipping puppet bot reaction (echo prevention)")
		return nil, nil
	}

	// Echo prevention: skip reactions from usernames matching known bridge patterns.
	senderName, _ := evt.GetData()["sender_name"].(string)
	senderName = strings.TrimPrefix(senderName, "@")
	if senderName != "" && isBridgeUsername(senderName, m.connector.Config.BotPrefix) {
		m.log.Debug().
			Str("post_id", reaction.PostId).
			Str("username", senderName).
			Str("emoji", reaction.EmojiName).
			Msg("Skipping bridge username reaction (echo prevention)")
		return nil, nil
	}

	return &reaction, nil
}

// parseTypingEvent extracts typing event data. Returns ("", "", false) to skip.
func (m *KChatClient) parseTypingEvent(evt *model.WebSocketEvent) (userID, channelID string, ok bool) {
	uid, uidOk := evt.GetData()["user_id"].(string)
	if !uidOk || uid == m.userID {
		return "", "", false
	}
	return uid, evt.GetBroadcast().ChannelId, true
}

// parseChannelViewedEvent extracts channel viewed data. Returns ("", false) to skip.
func (m *KChatClient) parseChannelViewedEvent(evt *model.WebSocketEvent) (channelID string, ok bool) {
	chID, chOk := evt.GetData()["channel_id"].(string)
	if !chOk {
		return "", false
	}
	return chID, true
}

func (m *KChatClient) handlePosted(evt *model.WebSocketEvent) {
	post, err := m.parsePostedEvent(evt)
	if err != nil {
		m.log.Warn().Err(err).Msg("Failed to parse posted event")
		return
	}
	if post == nil {
		return
	}

	m.log.Debug().
		Str("post_id", post.Id).
		Str("channel_id", post.ChannelId).
		Str("user_id", post.UserId).
		Msg("Received new message")

	ts := time.UnixMilli(post.CreateAt)

	// If the post carries the mx_txn prop set by this bridge on send, use it
	// so bridgev2 matches the remote echo against the outgoing transaction.
	txnID, _ := post.Props["mx_txn"].(string)

	m.eventSender.QueueRemoteEvent(m.userLogin, &simplevent.Message[*model.Post]{
		EventMeta: simplevent.EventMeta{
			Type: bridgev2.RemoteEventMessage,
			LogContext: func(c zerolog.Context) zerolog.Context {
				return c.Str("post_id", post.Id).Str("channel_id", post.ChannelId)
			},
			PortalKey:    makePortalKey(m.userLogin, post.ChannelId),
			Sender:       m.senderFor(post.UserId),
			Timestamp:    ts,
			CreatePortal: true,
		},
		TransactionID: networkid.TransactionID(txnID),
		ID:            MakeMessageID(post.Id),
		Data:          post,
		ConvertMessageFunc: func(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, data *model.Post) (*bridgev2.ConvertedMessage, error) {
			return m.convertPostToMatrix(data), nil
		},
	})
}

func (m *KChatClient) handlePostEdited(evt *model.WebSocketEvent) {
	post, err := m.parsePostEditedEvent(evt)
	if err != nil {
		m.log.Error().Err(err).Msg("Failed to parse post edited event")
		return
	}
	if post == nil {
		return
	}

	ts := time.UnixMilli(post.EditAt)

	m.eventSender.QueueRemoteEvent(m.userLogin, &simplevent.Message[*model.Post]{
		EventMeta: simplevent.EventMeta{
			Type: bridgev2.RemoteEventEdit,
			LogContext: func(c zerolog.Context) zerolog.Context {
				return c.Str("post_id", post.Id).Str("channel_id", post.ChannelId)
			},
			PortalKey: makePortalKey(m.userLogin, post.ChannelId),
			Sender:    m.senderFor(post.UserId),
			Timestamp: ts,
		},
		TargetMessage: MakeMessageID(post.Id),
		Data:          post,
		ConvertEditFunc: func(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, existing []*database.Message, data *model.Post) (*bridgev2.ConvertedEdit, error) {
			return m.convertEditToMatrix(data, existing), nil
		},
	})
}

func (m *KChatClient) handlePostDeleted(evt *model.WebSocketEvent) {
	post, err := m.parsePostDeletedEvent(evt)
	if err != nil {
		m.log.Error().Err(err).Msg("Failed to parse post deleted event")
		return
	}
	if post == nil {
		return
	}

	ts := time.UnixMilli(post.DeleteAt)

	m.eventSender.QueueRemoteEvent(m.userLogin, &simplevent.MessageRemove{
		EventMeta: simplevent.EventMeta{
			Type: bridgev2.RemoteEventMessageRemove,
			LogContext: func(c zerolog.Context) zerolog.Context {
				return c.Str("post_id", post.Id).Str("channel_id", post.ChannelId)
			},
			PortalKey: makePortalKey(m.userLogin, post.ChannelId),
			Sender:    m.senderFor(post.UserId),
			Timestamp: ts,
		},
		TargetMessage: MakeMessageID(post.Id),
	})
}

func (m *KChatClient) handleReactionAdded(evt *model.WebSocketEvent) {
	reaction, err := m.parseReactionEvent(evt)
	if err != nil {
		m.log.Error().Err(err).Msg("Failed to parse reaction added event")
		return
	}
	if reaction == nil {
		return
	}

	ts := time.UnixMilli(reaction.CreateAt)
	emoji := reactionToEmoji(reaction.EmojiName)

	m.eventSender.QueueRemoteEvent(m.userLogin, &simplevent.Reaction{
		EventMeta: simplevent.EventMeta{
			Type: bridgev2.RemoteEventReaction,
			LogContext: func(c zerolog.Context) zerolog.Context {
				return c.Str("post_id", reaction.PostId).Str("emoji", reaction.EmojiName)
			},
			PortalKey: makePortalKey(m.userLogin, evt.GetBroadcast().ChannelId),
			Sender:    m.senderFor(reaction.UserId),
			Timestamp: ts,
		},
		TargetMessage: MakeMessageID(reaction.PostId),
		EmojiID:       MakeEmojiID(reaction.EmojiName),
		Emoji:         emoji,
	})
}

func (m *KChatClient) handleReactionRemoved(evt *model.WebSocketEvent) {
	reaction, err := m.parseReactionEvent(evt)
	if err != nil {
		m.log.Error().Err(err).Msg("Failed to parse reaction removed event")
		return
	}
	if reaction == nil {
		return
	}

	m.eventSender.QueueRemoteEvent(m.userLogin, &simplevent.Reaction{
		EventMeta: simplevent.EventMeta{
			Type: bridgev2.RemoteEventReactionRemove,
			LogContext: func(c zerolog.Context) zerolog.Context {
				return c.Str("post_id", reaction.PostId).Str("emoji", reaction.EmojiName)
			},
			PortalKey: makePortalKey(m.userLogin, evt.GetBroadcast().ChannelId),
			Sender:    m.senderFor(reaction.UserId),
		},
		TargetMessage: MakeMessageID(reaction.PostId),
		EmojiID:       MakeEmojiID(reaction.EmojiName),
	})
}

func (m *KChatClient) handleTyping(evt *model.WebSocketEvent) {
	userID, channelID, ok := m.parseTypingEvent(evt)
	if !ok {
		return
	}

	timeout := m.connector.Config.TypingTimeout
	if timeout <= 0 {
		timeout = 5
	}

	m.eventSender.QueueRemoteEvent(m.userLogin, &simplevent.Typing{
		EventMeta: simplevent.EventMeta{
			Type:      bridgev2.RemoteEventTyping,
			PortalKey: makePortalKey(m.userLogin, channelID),
			Sender:    m.senderFor(userID),
		},
		Timeout: time.Duration(timeout) * time.Second,
	})
}

func (m *KChatClient) handleChannelViewed(evt *model.WebSocketEvent) {
	channelID, ok := m.parseChannelViewedEvent(evt)
	if !ok {
		return
	}

	m.eventSender.QueueRemoteEvent(m.userLogin, &simplevent.Receipt{
		EventMeta: simplevent.EventMeta{
			Type:      bridgev2.RemoteEventReadReceipt,
			PortalKey: makePortalKey(m.userLogin, channelID),
			Sender: bridgev2.EventSender{
				IsFromMe: true,
				Sender:   MakeUserID(m.userID),
			},
		},
	})
}

// convertPostToMatrix converts a kChat post to a bridgev2.ConvertedMessage.
func (m *KChatClient) convertPostToMatrix(post *model.Post) *bridgev2.ConvertedMessage {
	var parts []*bridgev2.ConvertedMessagePart

	if post.Message != "" {
		parsed := kChatFmtParse(post.Message)

		parts = append(parts, &bridgev2.ConvertedMessagePart{
			ID:   MakeMessagePartID(0),
			Type: event.EventMessage,
			Content: &event.MessageEventContent{
				MsgType:       event.MsgText,
				Body:          parsed.Body,
				Format:        parsed.Format,
				FormattedBody: parsed.FormattedBody,
			},
		})
	}

	for i, fileID := range post.FileIds {
		filePart := m.convertFileToMatrix(fileID, i+1)
		if filePart != nil {
			parts = append(parts, filePart)
		}
	}

	msg := &bridgev2.ConvertedMessage{
		Parts: parts,
	}

	if post.RootId != "" {
		replyTo := MakeMessageID(post.RootId)
		msg.ReplyTo = &networkid.MessageOptionalPartID{MessageID: replyTo}
	}

	return msg
}

// convertEditToMatrix converts an edited kChat post to a bridgev2.ConvertedEdit.
func (m *KChatClient) convertEditToMatrix(post *model.Post, existing []*database.Message) *bridgev2.ConvertedEdit {
	parsed := kChatFmtParse(post.Message)

	var editParts []*bridgev2.ConvertedEditPart
	var targetPart *database.Message
	if len(existing) > 0 {
		targetPart = existing[0]
	}

	editParts = append(editParts, &bridgev2.ConvertedEditPart{
		Part: targetPart,
		Type: event.EventMessage,
		Content: &event.MessageEventContent{
			MsgType:       event.MsgText,
			Body:          parsed.Body,
			Format:        parsed.Format,
			FormattedBody: parsed.FormattedBody,
		},
	})

	return &bridgev2.ConvertedEdit{
		ModifiedParts: editParts,
	}
}

// convertFileToMatrix converts a kChat file attachment to a Matrix message part.
func (m *KChatClient) convertFileToMatrix(fileID string, partIndex int) *bridgev2.ConvertedMessagePart {
	ctx := context.Background()
	fileInfo, _, err := m.client.GetFileInfo(ctx, fileID)
	if err != nil {
		m.log.Error().Err(err).Str("file_id", fileID).Msg("Failed to get file info")
		return nil
	}

	msgType := event.MsgFile
	mimeType := fileInfo.MimeType
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		msgType = event.MsgImage
	case strings.HasPrefix(mimeType, "video/"):
		msgType = event.MsgVideo
	case strings.HasPrefix(mimeType, "audio/"):
		msgType = event.MsgAudio
	}

	return &bridgev2.ConvertedMessagePart{
		ID:   MakeMessagePartID(partIndex),
		Type: event.EventMessage,
		Content: &event.MessageEventContent{
			MsgType: msgType,
			Body:    fileInfo.Name,
			Info: &event.FileInfo{
				MimeType: mimeType,
				Size:     int(fileInfo.Size),
			},
		},
		Extra: map[string]any{
			"ch.altanis.kchat.file_id": fileID,
		},
	}
}

// isBridgeUsername returns true if the username belongs to a known bridge
// infrastructure bot that should never be relayed. It checks against
// hardcoded bridge usernames and an optional configurable prefix.
func isBridgeUsername(username, botPrefix string) bool {
	switch {
	case username == "kchat-bridge":
		return true
	case strings.HasPrefix(username, "kchat_"):
		// Ghost users created by the bridge (username_template: kchat_{{.}})
		return true
	case botPrefix != "" && strings.HasPrefix(username, botPrefix):
		return true
	default:
		return false
	}
}

// reactionToEmoji converts a kChat emoji name to a Unicode emoji.
func reactionToEmoji(name string) string {
	emojiMap := map[string]string{
		"+1":               "\U0001f44d",
		"-1":               "\U0001f44e",
		"heart":            "\u2764\ufe0f",
		"smile":            "\U0001f604",
		"laughing":         "\U0001f606",
		"thumbsup":         "\U0001f44d",
		"thumbsdown":       "\U0001f44e",
		"wave":             "\U0001f44b",
		"clap":             "\U0001f44f",
		"fire":             "\U0001f525",
		"100":              "\U0001f4af",
		"tada":             "\U0001f389",
		"eyes":             "\U0001f440",
		"thinking":         "\U0001f914",
		"white_check_mark": "\u2705",
		"x":                "\u274c",
		"warning":          "\u26a0\ufe0f",
		"rocket":           "\U0001f680",
		"star":             "\u2b50",
		"pray":             "\U0001f64f",
	}

	if emoji, ok := emojiMap[name]; ok {
		return emoji
	}
	return fmt.Sprintf(":%s:", name)
}
