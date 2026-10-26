// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/id"
)

// mockEventSender captures queued remote events for test assertions.
type mockEventSender struct {
	mu     sync.Mutex
	events []bridgev2.RemoteEvent
}

func (m *mockEventSender) QueueRemoteEvent(_ *bridgev2.UserLogin, evt bridgev2.RemoteEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, evt)
}

func (m *mockEventSender) Events() []bridgev2.RemoteEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]bridgev2.RemoteEvent, len(m.events))
	copy(cp, m.events)
	return cp
}

func (m *mockEventSender) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = nil
}

// endpointCall records which API endpoints were hit during a test.
type endpointCall struct {
	Method string
	Path   string
	Body   string
}

// fakeMM is a test helper that wraps an httptest.Server simulating the
// kChat API. It records calls and provides canned responses.
type fakeMM struct {
	Server *httptest.Server

	mu    sync.Mutex
	calls []endpointCall

	// Users maps user ID to model.User for GetUser/GetMe responses.
	Users map[string]*model.User
	// TokenToUser maps bearer tokens to user IDs for GetMe auth.
	TokenToUser map[string]string
	// Channels maps channel ID to model.Channel.
	Channels map[string]*model.Channel
	// ChannelMembers maps channel ID to member list.
	ChannelMembers map[string]model.ChannelMembers
	// Teams maps user ID to team list.
	Teams map[string][]*model.Team
	// ChannelsForTeamUser maps "teamID:userID" to channel list.
	ChannelsForTeamUser map[string][]*model.Channel
	// ChannelsForUser maps user ID to channel list (all channels including DMs).
	ChannelsForUser map[string][]*model.Channel
	// Files maps file ID to model.FileInfo.
	Files map[string]*model.FileInfo
	// Posts maps channel ID to PostList for backfill endpoints.
	Posts map[string]*model.PostList
	// FailEndpoints causes specific path prefixes to return 500.
	FailEndpoints map[string]bool
}

func newFakeMM() *fakeMM {
	f := &fakeMM{
		Users:               make(map[string]*model.User),
		TokenToUser:         make(map[string]string),
		Channels:            make(map[string]*model.Channel),
		ChannelMembers:      make(map[string]model.ChannelMembers),
		Teams:               make(map[string][]*model.Team),
		ChannelsForTeamUser: make(map[string][]*model.Channel),
		ChannelsForUser:     make(map[string][]*model.Channel),
		Files:               make(map[string]*model.FileInfo),
		Posts:               make(map[string]*model.PostList),
		FailEndpoints:       make(map[string]bool),
	}
	f.Server = httptest.NewServer(http.HandlerFunc(f.handler))
	return f
}

func (f *fakeMM) Close() {
	f.Server.Close()
}

func (f *fakeMM) record(method, path, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, endpointCall{Method: method, Path: path, Body: body})
}

func (f *fakeMM) Calls() []endpointCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]endpointCall, len(f.calls))
	copy(cp, f.calls)
	return cp
}

func (f *fakeMM) CalledPath(path string) bool {
	for _, c := range f.Calls() {
		if strings.Contains(c.Path, path) {
			return true
		}
	}
	return false
}

func (f *fakeMM) resolveToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	for tok, uid := range f.TokenToUser {
		if auth == "BEARER "+tok || auth == "Bearer "+tok {
			return uid
		}
	}
	return ""
}

func (f *fakeMM) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.record(r.Method, r.URL.Path, string(body))

	// Check if this endpoint should fail.
	for prefix := range f.FailEndpoints {
		if strings.Contains(r.URL.Path, prefix) {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "fake error"})
			return
		}
	}

	path := r.URL.Path

	switch {
	// GET /api/v4/users/me
	case r.Method == "GET" && path == "/api/v4/users/me":
		uid := f.resolveToken(r)
		if uid == "" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "unauthorized"})
			return
		}
		if u, ok := f.Users[uid]; ok {
			_ = json.NewEncoder(w).Encode(u)
			return
		}
		w.WriteHeader(http.StatusNotFound)

	// GET /api/v4/users/{user_id}/channels (GetChannelsForUserWithLastDeleteAt)
	case r.Method == "GET" && strings.HasPrefix(path, "/api/v4/users/") && strings.HasSuffix(path, "/channels") && !strings.Contains(path, "/teams/"):
		parts := strings.Split(path, "/")
		// /api/v4/users/{uid}/channels
		if len(parts) >= 6 {
			uid := parts[4]
			if chs, ok := f.ChannelsForUser[uid]; ok {
				_ = json.NewEncoder(w).Encode(chs)
				return
			}
		}
		_ = json.NewEncoder(w).Encode([]*model.Channel{})

	// GET /api/v4/users/{user_id}
	case r.Method == "GET" && strings.HasPrefix(path, "/api/v4/users/") && !strings.Contains(path[len("/api/v4/users/"):], "/"):
		uid := path[len("/api/v4/users/"):]
		if u, ok := f.Users[uid]; ok {
			_ = json.NewEncoder(w).Encode(u)
			return
		}
		w.WriteHeader(http.StatusNotFound)

	// GET /api/v4/users/{user_id}/teams
	case r.Method == "GET" && strings.HasSuffix(path, "/teams"):
		parts := strings.Split(path, "/")
		// /api/v4/users/{uid}/teams
		if len(parts) >= 5 {
			uid := parts[4]
			if teams, ok := f.Teams[uid]; ok {
				_ = json.NewEncoder(w).Encode(teams)
				return
			}
		}
		_ = json.NewEncoder(w).Encode([]*model.Team{})

	// GET /api/v4/channels/{channel_id}/posts (GetPostsForChannel / GetPostsBefore / GetPostsAfter)
	case r.Method == "GET" && strings.HasPrefix(path, "/api/v4/channels/") && strings.HasSuffix(path, "/posts"):
		parts := strings.Split(path, "/")
		// /api/v4/channels/{chID}/posts
		if len(parts) >= 6 {
			chID := parts[4]
			if pl, ok := f.Posts[chID]; ok {
				_ = json.NewEncoder(w).Encode(pl)
				return
			}
		}
		// Return empty post list.
		_ = json.NewEncoder(w).Encode(model.NewPostList())

	// POST /api/v4/posts
	case r.Method == "POST" && path == "/api/v4/posts":
		var post model.Post
		_ = json.Unmarshal(body, &post)
		post.Id = "created-post-id"
		_ = json.NewEncoder(w).Encode(&post)

	// PUT /api/v4/posts/{post_id}/patch
	case r.Method == "PUT" && strings.HasSuffix(path, "/patch"):
		_ = json.NewEncoder(w).Encode(&model.Post{Id: "patched"})

	// DELETE /api/v4/posts/{post_id}
	case r.Method == "DELETE" && strings.HasPrefix(path, "/api/v4/posts/"):
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

	// POST /api/v4/reactions
	case r.Method == "POST" && path == "/api/v4/reactions":
		var reaction model.Reaction
		_ = json.Unmarshal(body, &reaction)
		_ = json.NewEncoder(w).Encode(&reaction)

	// DELETE /api/v4/reactions/...
	case r.Method == "DELETE" && strings.HasPrefix(path, "/api/v4/reactions/"):
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

	// DELETE /api/v4/users/{user_id}/posts/{post_id}/reactions/{emoji_name}
	case r.Method == "DELETE" && strings.Contains(path, "/posts/") && strings.Contains(path, "/reactions/"):
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

	// POST /api/v4/channels/members/{user_id}/view
	case r.Method == "POST" && strings.Contains(path, "/members/") && strings.HasSuffix(path, "/view"):
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

	// POST /api/v4/users/{user_id}/typing
	case r.Method == "POST" && strings.HasSuffix(path, "/typing"):
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

	// GET /api/v4/channels/{channel_id}
	case r.Method == "GET" && strings.HasPrefix(path, "/api/v4/channels/") && !strings.Contains(path[len("/api/v4/channels/"):], "/"):
		chID := path[len("/api/v4/channels/"):]
		if ch, ok := f.Channels[chID]; ok {
			_ = json.NewEncoder(w).Encode(ch)
			return
		}
		w.WriteHeader(http.StatusNotFound)

	// GET /api/v4/channels/{channel_id}/members
	case r.Method == "GET" && strings.HasSuffix(path, "/members"):
		parts := strings.Split(path, "/")
		if len(parts) >= 5 {
			chID := parts[4]
			if members, ok := f.ChannelMembers[chID]; ok {
				_ = json.NewEncoder(w).Encode(members)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(model.ChannelMembers{})

	// GET /api/v4/users/{user_id}/channels (GetChannelsForTeamForUser)
	case r.Method == "GET" && strings.Contains(path, "/teams/") && strings.HasSuffix(path, "/channels"):
		parts := strings.Split(path, "/")
		// /api/v4/users/{uid}/teams/{tid}/channels
		if len(parts) >= 7 {
			uid := parts[4]
			tid := parts[6]
			key := tid + ":" + uid
			if chs, ok := f.ChannelsForTeamUser[key]; ok {
				_ = json.NewEncoder(w).Encode(chs)
				return
			}
		}
		_ = json.NewEncoder(w).Encode([]*model.Channel{})

	// GET /api/v4/files/{file_id}/info
	case r.Method == "GET" && strings.HasSuffix(path, "/info") && strings.Contains(path, "/files/"):
		parts := strings.Split(path, "/")
		if len(parts) >= 5 {
			fileID := parts[4]
			if fi, ok := f.Files[fileID]; ok {
				_ = json.NewEncoder(w).Encode(fi)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)

	// POST /api/v4/files (upload)
	case r.Method == "POST" && path == "/api/v4/files":
		_ = json.NewEncoder(w).Encode(&model.FileUploadResponse{
			FileInfos: []*model.FileInfo{{Id: "uploaded-file-id", Name: "upload"}},
		})

	// POST /api/v4/users/logout
	case r.Method == "POST" && path == "/api/v4/users/logout":
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "not found: " + path})
	}
}

// newWebSocketEvent creates a model.WebSocketEvent for testing handlers.
func newWebSocketEvent(eventType model.WebsocketEventType, channelID string, data map[string]any) *model.WebSocketEvent {
	evt := model.NewWebSocketEvent(eventType, "", channelID, "", nil, "")
	return evt.SetData(data)
}

// newFullTestClient creates a KChatClient connected to a fake server,
// with a connector, puppets, and a mock event sender configured.
// The client is considered logged in.
func newFullTestClient(serverURL string) *KChatClient {
	log := zerolog.Nop()
	connector := &KChatConnector{
		Bridge:   &bridgev2.Bridge{},
		Config:   Config{},
		Puppets:  make(map[id.UserID]*PuppetClient),
		dpLogins: make(map[string]networkid.UserLoginID),
	}
	connector.Bridge.Log = log

	client := model.NewAPIv4Client(serverURL)
	client.SetToken("test-token")

	return &KChatClient{
		connector:   connector,
		client:      client,
		eventSender: &mockEventSender{},
		userLogin:  &bridgev2.UserLogin{UserLogin: &database.UserLogin{ID: "test-login"}},
		userID:      "my-user-id",
		teamID:      "my-team-id",
		serverURL:   serverURL,
		stopChan:    make(chan struct{}),
		log:         log,
	}
}

// testMock returns the mockEventSender from a test client.
func testMock(mc *KChatClient) *mockEventSender {
	return mc.eventSender.(*mockEventSender)
}

// newNotLoggedInClient creates a KChatClient that is not logged in (nil client).
func newNotLoggedInClient() *KChatClient {
	log := zerolog.Nop()
	connector := &KChatConnector{
		Bridge:   &bridgev2.Bridge{},
		Config:   Config{},
		Puppets:  make(map[id.UserID]*PuppetClient),
		dpLogins: make(map[string]networkid.UserLoginID),
	}
	connector.Bridge.Log = log
	return &KChatClient{
		connector:   connector,
		eventSender: &mockEventSender{},
		userLogin:  &bridgev2.UserLogin{UserLogin: &database.UserLogin{ID: "test-login"}},
		userID:      "my-user-id",
		stopChan:    make(chan struct{}),
		log:         log,
	}
}

// makeTestPortal creates a minimal bridgev2.Portal for testing.
func makeTestPortal(channelID string) *bridgev2.Portal {
	return &bridgev2.Portal{
		Portal: &database.Portal{
			PortalKey: networkid.PortalKey{
				ID: MakePortalID(channelID),
			},
		},
	}
}
