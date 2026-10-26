// Copyright 2024-2026 Remi Philippe
// Copyright 2026 rosch
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

// kChat is a PHP backend that mimics the Mattermost v4 API. PHP serializes
// empty associative arrays as [] instead of {}, which breaks the Mattermost
// Go SDK's typed unmarshalling (e.g. Post.props, User.props.customStatus).
// These raw helpers fetch such endpoints and sanitize the JSON before
// decoding into SDK types.

// fixPHPObjectArrays walks a decoded JSON document and converts empty arrays
// into empty objects where the Mattermost API expects objects.
func fixPHPObjectArrays(v any) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	for key, val := range m {
		switch key {
		case "props", "notify_props", "custom_status", "settings", "fields", "posts", "metadata":
			if arr, isEmpty := val.([]any); isEmpty && len(arr) == 0 {
				m[key] = map[string]any{}
			}
		}
		if sub, ok := val.(map[string]any); ok {
			fixPHPObjectArrays(sub)
		}
	}
	// User.props is map[string]string in the SDK; stringify any object values
	// (PHP serializes empty custom_status as []).
	if props, ok := m["props"].(map[string]any); ok {
		for k, val := range props {
			switch val.(type) {
			case string, nil:
			default:
				if b, err := json.Marshal(val); err == nil {
					props[k] = string(b)
				}
			}
		}
	}
}

// rawHTTPClient bounds raw API calls so a stalled kChat backend cannot hang
// the poller or message sends.
var rawHTTPClient = &http.Client{Timeout: 30 * time.Second}

// rawRequest performs an authenticated GET/POST against the API URL and
// returns the response body after applying fixPHPObjectArrays.
func rawRequest(ctx context.Context, client *model.Client4, method, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, client.APIURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+client.AuthToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := rawHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("kChat API %s %s returned HTTP %d: %s", method, path, resp.StatusCode, string(data))
	}
	return data, nil
}

// rawGetUser fetches a user profile with PHP-quirk-tolerant parsing.
func rawGetUser(ctx context.Context, client *model.Client4, userID string) (*model.User, error) {
	data, err := rawRequest(ctx, client, http.MethodGet, "/users/"+userID, nil)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse user response: %w", err)
	}
	fixPHPObjectArrays(doc)
	cleaned, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	user := &model.User{}
	if err := json.Unmarshal(cleaned, user); err != nil {
		return nil, fmt.Errorf("failed to decode user response: %w", err)
	}
	return user, nil
}

// rawCreatePost creates a post and decodes the response with PHP quirks fixed.
// kChat's validation is stricter than Mattermost's:
//   - file_ids must be present as an array
//   - props must be absent/null ({} and objects are rejected with 422)
func rawCreatePost(ctx context.Context, client *model.Client4, post *model.Post) (*model.Post, error) {
	post.PreCommit()
	payload, err := json.Marshal(post)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(payload, &doc); err != nil {
		return nil, err
	}
	if props, ok := doc["props"].(map[string]any); !ok || len(props) == 0 {
		delete(doc, "props")
	}
	if _, ok := doc["file_ids"]; !ok {
		doc["file_ids"] = []string{}
	}
	cleaned, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	data, err := rawRequest(ctx, client, http.MethodPost, "/posts", cleaned)
	if err != nil {
		return nil, err
	}
	var respDoc map[string]any
	if err := json.Unmarshal(data, &respDoc); err != nil {
		return nil, fmt.Errorf("failed to parse post response: %w", err)
	}
	fixPHPObjectArrays(respDoc)
	cleaned, err = json.Marshal(respDoc)
	if err != nil {
		return nil, err
	}
	created := &model.Post{}
	if err := json.Unmarshal(cleaned, created); err != nil {
		return nil, fmt.Errorf("failed to decode post response: %w", err)
	}
	return created, nil
}
