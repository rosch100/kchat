// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

// Compile-time assertion that KChatClient implements BackfillingNetworkAPI.
var _ bridgev2.BackfillingNetworkAPI = (*KChatClient)(nil)

// FetchMessages implements bridgev2.BackfillingNetworkAPI.
func (m *KChatClient) FetchMessages(ctx context.Context, params bridgev2.FetchMessagesParams) (*bridgev2.FetchMessagesResponse, error) {
	channelID := ParsePortalID(params.Portal.ID)

	maxCount := m.connector.Config.BackfillMaxCount
	if maxCount <= 0 {
		maxCount = 100
	}
	if params.Count > 0 {
		maxCount = params.Count
	}

	perPage := maxCount
	if perPage > 200 {
		perPage = 200
	}

	var path string
	if params.Forward && params.AnchorMessage != nil {
		path = fmt.Sprintf("/channels/%s/posts?after=%s&per_page=%d", channelID, ParseMessageID(params.AnchorMessage.ID), perPage)
	} else if params.AnchorMessage != nil {
		path = fmt.Sprintf("/channels/%s/posts?before=%s&per_page=%d", channelID, ParseMessageID(params.AnchorMessage.ID), perPage)
	} else {
		path = fmt.Sprintf("/channels/%s/posts?per_page=%d", channelID, perPage)
	}
	postList, err := rawGetPosts(ctx, m.client, path)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch posts for backfill: %w", err)
	}

	// Sort chronologically (oldest first).
	posts := postList.ToSlice()
	sort.Slice(posts, func(i, j int) bool {
		return posts[i].CreateAt < posts[j].CreateAt
	})

	if len(posts) > maxCount {
		posts = posts[:maxCount]
	}

	var messages []*bridgev2.BackfillMessage
	for _, post := range posts {
		// Skip system messages.
		if post.Type != "" && post.Type != model.PostTypeDefault {
			continue
		}

		converted := m.convertPostToMatrix(post)

		msg := &bridgev2.BackfillMessage{
			ConvertedMessage: converted,
			Sender: bridgev2.EventSender{
				Sender: MakeUserID(post.UserId),
			},
			ID:        MakeMessageID(post.Id),
			Timestamp: time.UnixMilli(post.CreateAt),
		}

		if post.RootId != "" {
			msg.ShouldBackfillThread = true
		}

		messages = append(messages, msg)
	}

	hasMore := len(postList.Order) >= perPage

	resp := &bridgev2.FetchMessagesResponse{
		Messages: messages,
		HasMore:  hasMore,
		Forward:  params.Forward,
	}

	// Use PrevPostId as cursor for backward pagination.
	if !params.Forward && postList.PrevPostId != "" {
		resp.Cursor = networkid.PaginationCursor(postList.PrevPostId)
	}

	return resp, nil
}
