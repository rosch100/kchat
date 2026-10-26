// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/id"
)

// newTestBridgeConnector creates a KChatConnector with a minimal Bridge
// that has a logger attached. This is needed for methods that log via mc.Bridge.Log.
func newTestBridgeConnector() *KChatConnector {
	log := zerolog.Nop()
	mc := &KChatConnector{
		Bridge:  &bridgev2.Bridge{},
		Config:  Config{},
		Puppets: make(map[id.UserID]*PuppetClient),
	}
	mc.Bridge.Log = log
	return mc
}

// fakeKChatAPI creates an httptest server that responds to /api/v4/users/me
// with a canned user response based on the Bearer token.
func fakeKChatAPI(tokenToUser map[string]struct{ id, username string }) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v4/users/me" {
			token := r.Header.Get("Authorization")
			for tok, user := range tokenToUser {
				// model.Client4 uses "BEARER" (uppercase), standard HTTP uses "Bearer".
				if token == "BEARER "+tok || token == "Bearer "+tok {
					_ = json.NewEncoder(w).Encode(map[string]string{
						"id":       user.id,
						"username": user.username,
					})
					return
				}
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
			return
		}
		http.NotFound(w, r)
	}))
}

func TestIsPuppetUserID(t *testing.T) {
	mc := newTestBridgeConnector()
	mc.Puppets[id.UserID("@bot-a:localhost")] = &PuppetClient{UserID: "mm-a"}
	mc.Puppets[id.UserID("@bot-b:localhost")] = &PuppetClient{UserID: "mm-b"}

	if !mc.IsPuppetUserID("mm-a") {
		t.Error("expected mm-a to be a puppet user ID")
	}
	if !mc.IsPuppetUserID("mm-b") {
		t.Error("expected mm-b to be a puppet user ID")
	}
	if mc.IsPuppetUserID("mm-unknown") {
		t.Error("expected mm-unknown NOT to be a puppet user ID")
	}
	if mc.IsPuppetUserID("") {
		t.Error("expected empty string NOT to be a puppet user ID")
	}
}

func TestPuppetCount(t *testing.T) {
	mc := newTestBridgeConnector()

	if mc.PuppetCount() != 0 {
		t.Errorf("expected 0, got %d", mc.PuppetCount())
	}

	mc.Puppets[id.UserID("@a:x")] = &PuppetClient{UserID: "u1"}
	mc.Puppets[id.UserID("@b:x")] = &PuppetClient{UserID: "u2"}
	mc.Puppets[id.UserID("@c:x")] = &PuppetClient{UserID: "u3"}

	if mc.PuppetCount() != 3 {
		t.Errorf("expected 3, got %d", mc.PuppetCount())
	}
}

func TestReloadPuppetsFromEntries_AddsPuppets(t *testing.T) {
	mm := fakeKChatAPI(map[string]struct{ id, username string }{
		"tok-alice": {"uid-alice", "puppet-alice"},
		"tok-bob":   {"uid-bob", "puppet-bob"},
	})
	defer mm.Close()

	mc := newTestBridgeConnector()
	mc.Config.ServerURL = mm.URL

	entries := []PuppetEntry{
		{Slug: "ALICE", MXID: "@puppet-alice:example.com", Token: "tok-alice"},
		{Slug: "BOB", MXID: "@puppet-bob:example.com", Token: "tok-bob"},
	}

	added, removed := mc.ReloadPuppetsFromEntries(context.Background(), entries)

	if added != 2 {
		t.Errorf("expected 2 added, got %d", added)
	}
	if removed != 0 {
		t.Errorf("expected 0 removed, got %d", removed)
	}
	if mc.PuppetCount() != 2 {
		t.Errorf("expected 2 total, got %d", mc.PuppetCount())
	}

	// Verify the puppet was created correctly.
	mc.puppetMu.RLock()
	puppet, ok := mc.Puppets[id.UserID("@puppet-alice:example.com")]
	mc.puppetMu.RUnlock()
	if !ok {
		t.Fatal("expected to find ALICE puppet")
	}
	if puppet.Username != "puppet-alice" {
		t.Errorf("expected username puppet-alice, got %s", puppet.Username)
	}
	if puppet.UserID != "uid-alice" {
		t.Errorf("expected user ID uid-alice, got %s", puppet.UserID)
	}
}

func TestReloadPuppetsFromEntries_RemovesPuppets(t *testing.T) {
	mm := fakeKChatAPI(map[string]struct{ id, username string }{
		"tok-alice": {"uid-alice", "puppet-alice"},
	})
	defer mm.Close()

	mc := newTestBridgeConnector()
	mc.Config.ServerURL = mm.URL

	// Pre-load two puppets with tokens set.
	aliceClient := model.NewAPIv4Client(mm.URL)
	aliceClient.SetToken("tok-alice")
	bobClient := model.NewAPIv4Client(mm.URL)
	bobClient.SetToken("tok-bob")
	mc.Puppets[id.UserID("@puppet-alice:example.com")] = &PuppetClient{
		MXID:     "@puppet-alice:example.com",
		Client:   aliceClient,
		UserID:   "uid-alice",
		Username: "puppet-alice",
	}
	mc.Puppets[id.UserID("@puppet-bob:example.com")] = &PuppetClient{
		MXID:     "@puppet-bob:example.com",
		Client:   bobClient,
		UserID:   "uid-bob",
		Username: "puppet-bob",
	}

	// Reload with only ALICE -- BOB should be removed.
	entries := []PuppetEntry{
		{Slug: "ALICE", MXID: "@puppet-alice:example.com", Token: "tok-alice"},
	}

	added, removed := mc.ReloadPuppetsFromEntries(context.Background(), entries)

	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}
	// ALICE token is unchanged, so it should NOT count as added.
	if added != 0 {
		t.Errorf("expected 0 added (unchanged token), got %d", added)
	}
	if mc.PuppetCount() != 1 {
		t.Errorf("expected 1 total, got %d", mc.PuppetCount())
	}

	mc.puppetMu.RLock()
	_, bobExists := mc.Puppets[id.UserID("@puppet-bob:example.com")]
	mc.puppetMu.RUnlock()
	if bobExists {
		t.Error("BOB puppet should have been removed")
	}
}

func TestReloadPuppetsFromEntries_SkipsFailedAuth(t *testing.T) {
	// Server returns 401 for all requests.
	mm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
	}))
	defer mm.Close()

	mc := newTestBridgeConnector()
	mc.Config.ServerURL = mm.URL

	entries := []PuppetEntry{
		{Slug: "BAD", MXID: "@puppet-bad:example.com", Token: "bad-token"},
	}

	added, _ := mc.ReloadPuppetsFromEntries(context.Background(), entries)

	if added != 0 {
		t.Errorf("expected 0 added for failed auth, got %d", added)
	}
	if mc.PuppetCount() != 0 {
		t.Errorf("expected 0 puppets, got %d", mc.PuppetCount())
	}
}

func TestHandleReloadPuppets_WithBody(t *testing.T) {
	mm := fakeKChatAPI(map[string]struct{ id, username string }{
		"tok-new": {"uid-new", "puppet-new"},
	})
	defer mm.Close()

	mc := newTestBridgeConnector()
	mc.Config.ServerURL = mm.URL

	entries := []PuppetEntry{
		{Slug: "NEW_BOT", MXID: "@puppet-new:example.com", Token: "tok-new"},
	}
	body, _ := json.Marshal(entries)

	req := httptest.NewRequest(http.MethodPost, "/api/reload-puppets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mc.HandleReloadPuppets(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]int
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp["added"] != 1 {
		t.Errorf("expected 1 added, got %d", resp["added"])
	}
	if resp["total"] != 1 {
		t.Errorf("expected 1 total, got %d", resp["total"])
	}
}

func TestHandleReloadPuppets_MethodNotAllowed(t *testing.T) {
	mc := newTestBridgeConnector()

	req := httptest.NewRequest(http.MethodGet, "/api/reload-puppets", nil)
	w := httptest.NewRecorder()

	mc.HandleReloadPuppets(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleReloadPuppets_EmptyBody(t *testing.T) {
	mc := newTestBridgeConnector()
	mc.Config.ServerURL = "http://localhost:9999" // no real server

	req := httptest.NewRequest(http.MethodPost, "/api/reload-puppets", nil)
	req.ContentLength = 0
	w := httptest.NewRecorder()

	mc.HandleReloadPuppets(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]int
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp["total"] != 0 {
		t.Errorf("expected 0 total (no env vars), got %d", resp["total"])
	}
}

func TestHandleReloadPuppets_InvalidJSON(t *testing.T) {
	mc := newTestBridgeConnector()

	req := httptest.NewRequest(http.MethodPost, "/api/reload-puppets",
		bytes.NewReader([]byte("{invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mc.HandleReloadPuppets(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestIsBridgeUsername(t *testing.T) {
	tests := []struct {
		username  string
		botPrefix string
		expected  bool
	}{
		// Hardcoded bridge usernames.
		{"kchat-bridge", "", true},
		{"kchat_someone", "", true},
		{"kchat_ceo", "", true},
		// Regular users should not match.
		{"ceo", "", false},
		{"admin", "", false},
		{"puppet-alice", "", false},
		// With configurable prefix.
		{"puppet-alice", "puppet-", true},
		{"puppet-bob", "puppet-", true},
		{"admin", "puppet-", false},
		{"ceo", "puppet-", false},
		// Empty prefix means no prefix filtering.
		{"puppet-alice", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.username+"_prefix_"+tt.botPrefix, func(t *testing.T) {
			got := isBridgeUsername(tt.username, tt.botPrefix)
			if got != tt.expected {
				t.Errorf("isBridgeUsername(%q, %q) = %v, want %v",
					tt.username, tt.botPrefix, got, tt.expected)
			}
		})
	}
}

func TestIsPuppetUserID_ConcurrentSafe(t *testing.T) {
	mc := newTestBridgeConnector()
	mc.Puppets[id.UserID("@a:x")] = &PuppetClient{UserID: "mm-1"}

	done := make(chan struct{})
	go func() {
		for range 100 {
			mc.IsPuppetUserID("mm-1")
			mc.IsPuppetUserID("mm-unknown")
		}
		close(done)
	}()

	// Concurrent writes.
	for range 100 {
		mc.puppetMu.Lock()
		mc.Puppets[id.UserID("@b:x")] = &PuppetClient{UserID: "mm-2"}
		delete(mc.Puppets, id.UserID("@b:x"))
		mc.puppetMu.Unlock()
	}

	<-done
}

func TestWatchNewPortals_StopsOnCancel(t *testing.T) {
	mc := newTestBridgeConnector()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		mc.WatchNewPortals(ctx, 50*time.Millisecond)
		close(done)
	}()

	// Let it tick a couple of times.
	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// OK -- goroutine exited cleanly.
	case <-time.After(2 * time.Second):
		t.Fatal("WatchNewPortals did not stop after context cancel")
	}
}

func TestWatchNewPortals_DefaultInterval(t *testing.T) {
	mc := newTestBridgeConnector()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately -- just verify it starts without panic.
	cancel()

	mc.WatchNewPortals(ctx, 0)
	// If we got here without panic, the default interval was applied correctly.
}

func TestPuppetEntry_JSON(t *testing.T) {
	entry := PuppetEntry{
		Slug:  "ALICE",
		MXID:  "@puppet-alice:example.com",
		Token: "secret-token",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded PuppetEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Slug != "ALICE" {
		t.Errorf("Slug: got %q, want %q", decoded.Slug, "ALICE")
	}
	if decoded.MXID != "@puppet-alice:example.com" {
		t.Errorf("MXID: got %q", decoded.MXID)
	}
	if decoded.Token != "secret-token" {
		t.Errorf("Token: got %q", decoded.Token)
	}
}

// ---------------------------------------------------------------------------
// Security tests for HandleReloadPuppets
// ---------------------------------------------------------------------------

func TestHandleReloadPuppets_OversizedBody(t *testing.T) {
	mc := newTestBridgeConnector()

	// Create a body larger than maxReloadBodySize (1 MB).
	oversized := make([]byte, maxReloadBodySize+1)
	for i := range oversized {
		oversized[i] = 'A'
	}

	req := httptest.NewRequest(http.MethodPost, "/api/reload-puppets", bytes.NewReader(oversized))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mc.HandleReloadPuppets(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413 for oversized body, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleReloadPuppets_InjectionPayloads(t *testing.T) {
	tests := []struct {
		name    string
		entries []PuppetEntry
	}{
		{
			name: "MXID with script tags",
			entries: []PuppetEntry{
				{Slug: "XSS", MXID: "<script>alert(1)</script>", Token: "tok-xss"},
			},
		},
		{
			name: "Token with null bytes",
			entries: []PuppetEntry{
				{Slug: "NULL", MXID: "@null:example.com", Token: "tok-\x00-null\x00bytes"},
			},
		},
		{
			name: "Slug with control characters",
			entries: []PuppetEntry{
				{Slug: "CTRL\x01\x02\x03\x7f", MXID: "@ctrl:example.com", Token: "tok-ctrl"},
			},
		},
		{
			name: "Extremely long slug",
			entries: []PuppetEntry{
				{Slug: strings.Repeat("A", 10000), MXID: "@long:example.com", Token: "tok-long"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use a server that always returns 401 so the puppet auth fails
			// (we only care that the handler does not panic).
			mm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
			}))
			defer mm.Close()

			mc := newTestBridgeConnector()
			mc.Config.ServerURL = mm.URL

			body, err := json.Marshal(tt.entries)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/reload-puppets", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// The handler must not panic for any injection payload.
			mc.HandleReloadPuppets(w, req)

			// Accept either 200 (processed, auth failed gracefully) or 400 (rejected input).
			if w.Code != http.StatusOK && w.Code != http.StatusBadRequest {
				t.Errorf("expected 200 or 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestHandleReloadPuppets_AuditLogging(t *testing.T) {
	// Verify the handler does not panic when called with a standard httptest
	// request that has RemoteAddr set. The handler logs remote_addr from
	// r.RemoteAddr; this test confirms it works without error.
	mc := newTestBridgeConnector()
	mc.Config.ServerURL = "http://localhost:9999" // no real server needed

	req := httptest.NewRequest(http.MethodPost, "/api/reload-puppets", nil)
	req.ContentLength = 0
	// httptest.NewRequest sets RemoteAddr to "192.0.2.1:1234" by default.
	if req.RemoteAddr == "" {
		t.Fatal("httptest.NewRequest should set RemoteAddr")
	}

	w := httptest.NewRecorder()

	// Must not panic when logging remote_addr.
	mc.HandleReloadPuppets(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
