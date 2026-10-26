// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"context"
	"fmt"

	"github.com/mattermost/mattermost/server/public/model"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/event"
)

// HandleMatrixMessage handles a message sent from Matrix to kChat.
func (m *KChatClient) HandleMatrixMessage(ctx context.Context, msg *bridgev2.MatrixMessage) (*bridgev2.MatrixMessageResponse, error) {
	if !m.IsLoggedIn() {
		return nil, bridgev2.ErrNotLoggedIn
	}

	// Check if the real sender has a puppet kChat client.
	// If so, post as that puppet instead of the relay account.
	postClient, senderID := m.resolvePostClient(msg.OrigSender, msg.Event)

	channelID := ParsePortalID(msg.Portal.ID)
	content := msg.Content

	post := &model.Post{
		ChannelId: channelID,
	}

	switch content.MsgType {
	case event.MsgText, event.MsgNotice, event.MsgEmote:
		text := matrixfmtParse(content)
		if content.MsgType == event.MsgEmote {
			text = "/me " + text
		}
		post.Message = text

	case event.MsgImage, event.MsgVideo, event.MsgAudio, event.MsgFile:
		fileID, err := m.uploadMatrixMedia(ctx, msg)
		if err != nil {
			return nil, fmt.Errorf("failed to upload media: %w", err)
		}
		post.FileIds = []string{fileID}
		if content.Body != "" && content.Body != content.GetFileName() {
			post.Message = content.Body
		}

	default:
		return nil, fmt.Errorf("unsupported message type: %s", content.MsgType)
	}

	// Handle replies.
	if msg.ReplyTo != nil {
		post.RootId = ParseMessageID(msg.ReplyTo.ID)
	}

	createdPost, _, err := postClient.CreatePost(ctx, post)
	if err != nil {
		return nil, fmt.Errorf("failed to create post: %w", err)
	}

	return &bridgev2.MatrixMessageResponse{
		DB: &database.Message{
			ID:       MakeMessageID(createdPost.Id),
			SenderID: MakeUserID(senderID),
		},
	}, nil
}

// resolvePostClient returns the kChat API client and user ID to use for
// posting a message. If the original Matrix sender has a puppet client
// configured (i.e. a dedicated kChat bot account), that client is used.
// Otherwise falls back to the default relay client.
func (m *KChatClient) resolvePostClient(origSender *bridgev2.OrigSender, evt *event.Event) (*model.Client4, string) {
	m.connector.puppetMu.RLock()
	defer m.connector.puppetMu.RUnlock()

	// Check OrigSender first (set by bridgev2 for relayed messages).
	if origSender != nil {
		if puppet, ok := m.connector.Puppets[origSender.UserID]; ok {
			m.log.Debug().
				Str("mxid", string(origSender.UserID)).
				Str("mm_username", puppet.Username).
				Msg("Using puppet client for message")
			return puppet.Client, puppet.UserID
		}
	}

	// Also check raw event sender (covers non-relay cases).
	if evt != nil && evt.Sender != "" {
		if puppet, ok := m.connector.Puppets[evt.Sender]; ok {
			m.log.Debug().
				Str("mxid", string(evt.Sender)).
				Str("mm_username", puppet.Username).
				Msg("Using puppet client for message (via event sender)")
			return puppet.Client, puppet.UserID
		}
	}

	return m.client, m.userID
}

// HandleMatrixEdit handles an edit sent from Matrix.
func (m *KChatClient) HandleMatrixEdit(ctx context.Context, msg *bridgev2.MatrixEdit) error {
	if !m.IsLoggedIn() {
		return bridgev2.ErrNotLoggedIn
	}

	postID := ParseMessageID(msg.EditTarget.ID)
	text := matrixfmtParse(msg.Content)

	patch := &model.PostPatch{
		Message: &text,
	}

	_, _, err := m.client.PatchPost(ctx, postID, patch)
	if err != nil {
		return fmt.Errorf("failed to edit post: %w", err)
	}

	return nil
}

// HandleMatrixMessageRemove handles a message deletion from Matrix.
func (m *KChatClient) HandleMatrixMessageRemove(ctx context.Context, msg *bridgev2.MatrixMessageRemove) error {
	if !m.IsLoggedIn() {
		return bridgev2.ErrNotLoggedIn
	}

	postID := ParseMessageID(msg.TargetMessage.ID)
	_, err := m.client.DeletePost(ctx, postID)
	if err != nil {
		return fmt.Errorf("failed to delete post: %w", err)
	}
	return nil
}

// PreHandleMatrixReaction validates a reaction before sending.
func (m *KChatClient) PreHandleMatrixReaction(_ context.Context, msg *bridgev2.MatrixReaction) (bridgev2.MatrixReactionPreResponse, error) {
	emojiID := emojiToReaction(msg.Content.RelatesTo.Key)
	return bridgev2.MatrixReactionPreResponse{
		SenderID: MakeUserID(m.userID),
		EmojiID:  MakeEmojiID(emojiID),
		Emoji:    msg.Content.RelatesTo.Key,
	}, nil
}

// HandleMatrixReaction sends a reaction to kChat.
func (m *KChatClient) HandleMatrixReaction(ctx context.Context, msg *bridgev2.MatrixReaction) (reaction *database.Reaction, err error) {
	if !m.IsLoggedIn() {
		return nil, bridgev2.ErrNotLoggedIn
	}

	postID := ParseMessageID(msg.TargetMessage.ID)
	emojiName := ParseEmojiID(msg.PreHandleResp.EmojiID)

	mmReaction := &model.Reaction{
		UserId:    m.userID,
		PostId:    postID,
		EmojiName: emojiName,
	}

	_, _, err = m.client.SaveReaction(ctx, mmReaction)
	if err != nil {
		return nil, fmt.Errorf("failed to save reaction: %w", err)
	}

	return &database.Reaction{
		EmojiID: MakeEmojiID(emojiName),
	}, nil
}

// HandleMatrixReactionRemove removes a reaction in kChat.
func (m *KChatClient) HandleMatrixReactionRemove(ctx context.Context, msg *bridgev2.MatrixReactionRemove) error {
	if !m.IsLoggedIn() {
		return bridgev2.ErrNotLoggedIn
	}

	postID := ParseMessageID(msg.TargetReaction.MessageID)
	emojiName := ParseEmojiID(msg.TargetReaction.EmojiID)

	_, err := m.client.DeleteReaction(ctx, &model.Reaction{
		UserId:    m.userID,
		PostId:    postID,
		EmojiName: emojiName,
	})
	if err != nil {
		return fmt.Errorf("failed to remove reaction: %w", err)
	}
	return nil
}

// HandleMatrixReadReceipt marks a channel as viewed in kChat.
func (m *KChatClient) HandleMatrixReadReceipt(ctx context.Context, msg *bridgev2.MatrixReadReceipt) error {
	if !m.IsLoggedIn() {
		return bridgev2.ErrNotLoggedIn
	}

	channelID := ParsePortalID(msg.Portal.ID)
	_, _, err := m.client.ViewChannel(ctx, m.userID, &model.ChannelView{
		ChannelId: channelID,
	})
	if err != nil {
		return fmt.Errorf("failed to mark channel as viewed: %w", err)
	}
	return nil
}

// HandleMatrixTyping sends a typing indicator to kChat.
func (m *KChatClient) HandleMatrixTyping(ctx context.Context, msg *bridgev2.MatrixTyping) error {
	if !m.IsLoggedIn() {
		return bridgev2.ErrNotLoggedIn
	}

	channelID := ParsePortalID(msg.Portal.ID)

	_, err := m.client.PublishUserTyping(ctx, m.userID, model.TypingRequest{
		ChannelId: channelID,
	})
	if err != nil {
		m.log.Debug().Err(err).Msg("Failed to send typing indicator")
	}
	return nil
}

// uploadMatrixMedia downloads media from Matrix and uploads it to kChat.
func (m *KChatClient) uploadMatrixMedia(ctx context.Context, msg *bridgev2.MatrixMessage) (string, error) {
	content := msg.Content

	data, err := msg.Portal.Bridge.Bot.DownloadMedia(ctx, content.URL, content.File)
	if err != nil {
		return "", fmt.Errorf("failed to download Matrix media: %w", err)
	}

	channelID := ParsePortalID(msg.Portal.ID)
	filename := content.GetFileName()
	if filename == "" {
		filename = "upload"
	}

	fileUploadResp, _, err := m.client.UploadFile(ctx, data, channelID, filename)
	if err != nil {
		return "", fmt.Errorf("failed to upload to kChat: %w", err)
	}

	if len(fileUploadResp.FileInfos) == 0 {
		return "", fmt.Errorf("no file info returned from upload")
	}

	return fileUploadResp.FileInfos[0].Id, nil
}

// emojiToReaction converts a Unicode emoji to a kChat emoji name.
func emojiToReaction(emoji string) string {
	reverseMap := map[string]string{
		"\U0001f44d":   "+1",
		"\U0001f44e":   "-1",
		"\u2764\ufe0f": "heart",
		"\U0001f604":   "smile",
		"\U0001f606":   "laughing",
		"\U0001f44b":   "wave",
		"\U0001f44f":   "clap",
		"\U0001f525":   "fire",
		"\U0001f4af":   "100",
		"\U0001f389":   "tada",
		"\U0001f440":   "eyes",
		"\U0001f914":   "thinking",
		"\u2705":       "white_check_mark",
		"\u274c":       "x",
		"\u26a0\ufe0f": "warning",
		"\U0001f680":   "rocket",
		"\u2b50":       "star",
		"\U0001f64f":   "pray",
	}

	if name, ok := reverseMap[emoji]; ok {
		return name
	}

	// Strip colons for custom emoji names.
	if len(emoji) > 2 && emoji[0] == ':' && emoji[len(emoji)-1] == ':' {
		return emoji[1 : len(emoji)-1]
	}

	return emoji
}
