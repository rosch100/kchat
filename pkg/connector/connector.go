// Copyright 2024-2026 Remi Philippe
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
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/id"
)

// PuppetEntry describes a single puppet agent for config-driven loading
// via the hot-reload JSON API.
type PuppetEntry struct {
	Slug  string `json:"slug"`
	MXID  string `json:"mxid"`
	Token string `json:"token"`
}

// PuppetClient holds a kChat API client for a specific Matrix user,
// allowing their messages to appear as a dedicated kChat bot/user.
type PuppetClient struct {
	MXID     id.UserID
	Client   *model.Client4
	UserID   string // kChat user/bot ID
	Username string
}

// KChatConnector implements bridgev2.NetworkConnector for kChat.
type KChatConnector struct {
	Bridge   *bridgev2.Bridge
	Config   Config
	Puppets  map[id.UserID]*PuppetClient
	puppetMu sync.RWMutex

	// dpLogins maps kChat user IDs to UserLoginIDs for double puppet
	// resolution. When an incoming MM event's sender matches a key in this
	// map, the corresponding UserLoginID is set on EventSender.SenderLogin
	// so the bridgev2 framework uses that user's double puppet intent.
	dpLogins   map[string]networkid.UserLoginID
	dpLoginsMu sync.RWMutex
}

var _ bridgev2.NetworkConnector = (*KChatConnector)(nil)

func (mc *KChatConnector) Init(bridge *bridgev2.Bridge) {
	mc.Bridge = bridge
}

func (mc *KChatConnector) Start(ctx context.Context) error {
	if err := mc.Config.PostProcess(); err != nil {
		return fmt.Errorf("failed to post-process config: %w", err)
	}
	mc.Puppets = make(map[id.UserID]*PuppetClient)
	mc.dpLogins = make(map[string]networkid.UserLoginID)
	mc.loadPuppets(ctx)
	go mc.autoLogin(ctx)

	// Start continuous portal watcher for relay setup on new rooms.
	go mc.WatchNewPortals(ctx, 0)

	// Start admin HTTP API for puppet hot-reload.
	apiAddr := mc.Config.AdminAPIAddr
	if apiAddr == "" {
		apiAddr = os.Getenv("BRIDGE_API_ADDR")
	}
	if apiAddr == "" {
		apiAddr = ":29320"
	}
	if apiAddr != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/reload-puppets", mc.HandleReloadPuppets)
		mux.HandleFunc("/api/double-puppet", mc.HandleDoublePuppet)
		server := &http.Server{
			Addr:         apiAddr,
			Handler:      mux,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout:  60 * time.Second,
		}
		go func() {
			mc.Bridge.Log.Info().Str("addr", apiAddr).Msg("Starting bridge admin API")
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				mc.Bridge.Log.Error().Err(err).Msg("Bridge admin API error")
			}
		}()
	}

	return nil
}

// loadPuppets reads KCHAT_PUPPET_* env vars to create dedicated
// kChat clients for specific Matrix users. This allows a puppet bot
// to post as its own kChat identity instead of being relayed.
//
// Env var format:
//
//	KCHAT_PUPPET_<NAME>_MXID  = @puppet-bot:example.com
//	KCHAT_PUPPET_<NAME>_TOKEN = <kchat bot access token>
//	KCHAT_PUPPET_<NAME>_URL   = http://kchat:8065  (optional, falls back to network.server_url)
func (mc *KChatConnector) loadPuppets(ctx context.Context) {
	// Scan for puppet env vars. We look for known names first,
	// then fall back to scanning KCHAT_PUPPET_*_MXID patterns.
	puppetNames := []string{}

	// Also scan environment for any KCHAT_PUPPET_*_MXID vars.
	for _, env := range os.Environ() {
		if len(env) > len("KCHAT_PUPPET_") {
			rest := env[len("KCHAT_PUPPET_"):]
			if idx := findSuffix(rest, "_MXID="); idx > 0 {
				name := rest[:idx]
				found := false
				for _, n := range puppetNames {
					if n == name {
						found = true
						break
					}
				}
				if !found {
					puppetNames = append(puppetNames, name)
				}
			}
		}
	}

	for _, name := range puppetNames {
		mxid := os.Getenv("KCHAT_PUPPET_" + name + "_MXID")
		token := os.Getenv("KCHAT_PUPPET_" + name + "_TOKEN")
		if mxid == "" || token == "" {
			continue
		}

		serverURL := os.Getenv("KCHAT_PUPPET_" + name + "_URL")
		if serverURL == "" {
			serverURL = mc.Config.ServerURL
		}

		client := model.NewAPIv4Client(serverURL)
		client.SetToken(token)

		me, _, err := client.GetMe(ctx, "")
		if err != nil {
			mc.Bridge.Log.Error().Err(err).
				Str("puppet", name).
				Str("mxid", mxid).
				Msg("Failed to verify puppet token")
			continue
		}

		puppet := &PuppetClient{
			MXID:     id.UserID(mxid),
			Client:   client,
			UserID:   me.Id,
			Username: me.Username,
		}
		mc.Puppets[puppet.MXID] = puppet
		mc.Bridge.Log.Info().
			Str("puppet", name).
			Str("mxid", mxid).
			Str("mm_username", me.Username).
			Str("mm_user_id", me.Id).
			Msg("Loaded puppet client")

		// Also set up double puppeting so MM→Matrix events from this user
		// appear under their real Matrix MXID instead of a ghost.
		if err := mc.setupUserDoublePuppet(ctx, me.Id, mxid); err != nil {
			mc.Bridge.Log.Warn().Err(err).
				Str("puppet", name).
				Str("mxid", mxid).
				Msg("Failed to setup double puppet for puppet user")
		}
	}
}

// findSuffix returns the index where suffix starts in s, or -1 if not found.
func findSuffix(s, suffix string) int {
	for i := 0; i <= len(s)-len(suffix); i++ {
		if s[i:i+len(suffix)] == suffix {
			return i
		}
	}
	return -1
}

// autoLogin checks for KCHAT_AUTO_TOKEN and KCHAT_AUTO_SERVER_URL
// env vars and performs an automatic login if no existing logins are found.
// This allows the bridge to connect on first boot without manual bot interaction.
func (mc *KChatConnector) autoLogin(ctx context.Context) {
	token := os.Getenv("KCHAT_AUTO_TOKEN")
	serverURL := os.Getenv("KCHAT_AUTO_SERVER_URL")
	ownerMXID := os.Getenv("KCHAT_AUTO_OWNER_MXID")
	if token == "" || serverURL == "" || ownerMXID == "" {
		return
	}

	// Wait for the bridge framework to finish loading existing logins.
	time.Sleep(5 * time.Second)

	// Check if any full logins exist. Double-puppet-only logins don't count —
	// they have no MM connection and can't serve as the relay login.
	hasFullLogin, err := mc.hasFullUserLogin(ctx)
	if err != nil {
		mc.Bridge.Log.Error().Err(err).Msg("Auto-login: failed to check existing logins")
		return
	}
	if hasFullLogin {
		mc.Bridge.Log.Info().Msg("Existing full login found, skipping auto-login")
		return
	}

	mc.Bridge.Log.Info().Str("server_url", serverURL).Msg("Performing auto-login")

	client := model.NewAPIv4Client(serverURL)
	client.SetToken(token)

	me, _, err := client.GetMe(ctx, "")
	if err != nil {
		mc.Bridge.Log.Error().Err(err).Msg("Auto-login: failed to verify token")
		return
	}

	teams, _, err := client.GetTeamsForUser(ctx, me.Id, "")
	if err != nil {
		mc.Bridge.Log.Error().Err(err).Msg("Auto-login: failed to get teams")
		return
	}
	teamID := ""
	if len(teams) > 0 {
		teamID = teams[0].Id
	}

	loginID := MakeUserLoginID(me.Id)

	// Get or create the bridge user for the owner Matrix account.
	user, err := mc.Bridge.GetUserByMXID(ctx, id.UserID(ownerMXID))
	if err != nil {
		mc.Bridge.Log.Error().Err(err).Msg("Auto-login: failed to get bridge user")
		return
	}

	ul, err := user.NewLogin(ctx, &database.UserLogin{
		ID:         loginID,
		RemoteName: fmt.Sprintf("%s @ %s (auto)", me.Username, serverURL),
	}, &bridgev2.NewLoginParams{
		LoadUserLogin: mc.LoadUserLogin,
	})
	if err != nil {
		mc.Bridge.Log.Error().Err(err).Msg("Auto-login: failed to create login")
		return
	}

	meta := ul.Metadata.(*UserLoginMetadata)
	meta.ServerURL = serverURL
	meta.Token = token
	meta.UserID = me.Id
	meta.TeamID = teamID
	if err := ul.Save(ctx); err != nil {
		mc.Bridge.Log.Error().Err(err).Msg("Auto-login: failed to save login")
		return
	}

	mmClient := ul.Client.(*KChatClient)
	mmClient.client = client
	mmClient.serverURL = serverURL
	mmClient.userID = me.Id
	mmClient.teamID = teamID
	mmClient.Connect(ctx)

	mc.Bridge.Log.Info().
		Str("username", me.Username).
		Str("server_url", serverURL).
		Msg("Auto-login complete")

	// Enable double puppeting so that when this user posts on kChat,
	// the bridge creates Matrix events as their real MXID (e.g. @admin:example.com)
	// instead of the ghost (@kchat_<id>:example.com).
	// Use setupUserDoublePuppet first (as_token: from double_puppet.secrets config),
	// falling back to the legacy password-based setupDoublePuppet if that fails.
	if err := mc.setupUserDoublePuppet(ctx, me.Id, ownerMXID); err != nil {
		mc.Bridge.Log.Warn().Err(err).Msg("Auto-login: as_token double puppet failed, trying password fallback")
		mc.setupDoublePuppet(ctx, user)
	}

	// Set relay for all portals so puppet users' Matrix messages get bridged.
	// The bridgev2 framework requires a per-portal relay login before it will
	// call HandleMatrixMessage (where our puppet system selects the correct
	// kChat client). Without relay, the framework rejects with "not
	// logged in" and the puppet system is never reached.
	go mc.autoSetRelay(ctx, ul)
}

// autoSetRelay sets the auto-login user as the relay for all bridged rooms.
// Runs with retries because portals are created asynchronously during
// kChat channel sync after the WebSocket connects.
func (mc *KChatConnector) autoSetRelay(ctx context.Context, login *bridgev2.UserLogin) {
	// Wait for initial channel sync to create portals.
	time.Sleep(15 * time.Second)

	for attempt := range 3 {
		portals, err := mc.Bridge.GetAllPortalsWithMXID(ctx)
		if err != nil {
			mc.Bridge.Log.Error().Err(err).Msg("Auto-relay: failed to get portals")
			return
		}

		setCount := 0
		for _, portal := range portals {
			if portal.Relay == nil {
				if err := portal.SetRelay(ctx, login); err != nil {
					mc.Bridge.Log.Warn().Err(err).
						Str("portal_mxid", string(portal.MXID)).
						Msg("Auto-relay: failed to set relay")
				} else {
					setCount++
				}
			}
		}

		mc.Bridge.Log.Info().
			Int("set_count", setCount).
			Int("total_portals", len(portals)).
			Int("attempt", attempt+1).
			Msg("Auto-relay: updated portals")

		if attempt < 2 {
			time.Sleep(30 * time.Second)
		}
	}
}

// setupDoublePuppet enables double puppeting for a bridge user by obtaining
// a Synapse access token via password login. This allows the bridge to send
// Matrix events as the user's real MXID (e.g. @admin:example.com) instead of the
// ghost user (@kchat_<id>:example.com) when the user posts on kChat.
//
// Requires SYNAPSE_DOUBLE_PUPPET_PASSWORD env var to be set.
// The Synapse homeserver URL comes from double_puppet.servers config.
func (mc *KChatConnector) setupDoublePuppet(ctx context.Context, user *bridgev2.User) {
	password := os.Getenv("SYNAPSE_DOUBLE_PUPPET_PASSWORD")
	if password == "" {
		mc.Bridge.Log.Debug().Msg("Double puppet: SYNAPSE_DOUBLE_PUPPET_PASSWORD not set, skipping")
		return
	}

	mxid := user.MXID
	localpart, _, err := mxid.Parse()
	if err != nil {
		mc.Bridge.Log.Error().Err(err).Str("mxid", string(mxid)).Msg("Double puppet: failed to parse MXID")
		return
	}

	// Get Synapse URL from the double_puppet.servers config or fall back to env var.
	synapseURL := os.Getenv("SYNAPSE_URL")
	if synapseURL == "" {
		synapseURL = "http://synapse:8008"
	}

	// Login to Synapse to get an access token for this user.
	loginPayload, err := json.Marshal(map[string]string{
		"type":     "m.login.password",
		"user":     localpart,
		"password": password,
	})
	if err != nil {
		mc.Bridge.Log.Error().Err(err).Msg("Double puppet: failed to marshal login payload")
		return
	}
	// #nosec G704 -- URL built from operator-controlled SYNAPSE_URL env var, not user input.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, synapseURL+"/_matrix/client/v3/login",
		bytes.NewReader(loginPayload))
	if err != nil {
		mc.Bridge.Log.Error().Err(err).Msg("Double puppet: failed to create login request")
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req) // #nosec G704 -- URL built from operator-controlled SYNAPSE_URL env var, not user input
	if err != nil {
		mc.Bridge.Log.Error().Err(err).Msg("Double puppet: Synapse login request failed")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		mc.Bridge.Log.Error().
			Int("status", resp.StatusCode).
			Str("body", string(body)).
			Msg("Double puppet: Synapse login failed")
		return
	}

	var loginResp struct {
		AccessToken string `json:"access_token"` // #nosec G117 -- response decoder, token passed to LoginDoublePuppet only
		UserID      string `json:"user_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		mc.Bridge.Log.Error().Err(err).Msg("Double puppet: failed to decode login response")
		return
	}

	if err := user.LoginDoublePuppet(ctx, loginResp.AccessToken); err != nil {
		mc.Bridge.Log.Error().Err(err).Str("mxid", string(mxid)).Msg("Double puppet: LoginDoublePuppet failed")
		return
	}

	mc.Bridge.Log.Info().
		Str("mxid", string(mxid)).
		Msg("Double puppet: enabled successfully")
}

// hasFullUserLogin checks whether any full (non-double-puppet-only) UserLogin
// exists in the database. It queries all users with logins, then inspects
// each login's metadata to distinguish full logins from dp-only ones.
func (mc *KChatConnector) hasFullUserLogin(ctx context.Context) (bool, error) {
	userIDs, err := mc.Bridge.DB.UserLogin.GetAllUserIDsWithLogins(ctx)
	if err != nil {
		return false, err
	}
	for _, uid := range userIDs {
		logins, err := mc.Bridge.DB.UserLogin.GetAllForUser(ctx, uid)
		if err != nil {
			return false, err
		}
		for _, login := range logins {
			meta, ok := login.Metadata.(*UserLoginMetadata)
			if !ok || meta == nil || !meta.DoublePuppetOnly {
				return true, nil
			}
		}
	}
	return false, nil
}

// useConfigASToken is the sentinel value the bridgev2 framework uses to
// indicate that double puppeting should use the as_token from config rather
// than a per-user access token. Matches the constant in bridgev2/matrix.
const useConfigASToken = "appservice-config"

// setupUserDoublePuppet creates a lightweight UserLogin for a kChat user
// and enables double puppeting for the corresponding Matrix user. The login
// has no MM API client or WebSocket — it exists solely so the framework can
// route incoming MM events through the real Matrix user's double puppet intent.
//
// If a UserLogin already exists for the MM user (e.g. the auto-login user),
// it only registers the dpLogins mapping without creating a duplicate.
func (mc *KChatConnector) setupUserDoublePuppet(ctx context.Context, mmUserID, matrixMXID string) error {
	if mc.Bridge == nil || mc.Bridge.DB == nil {
		return fmt.Errorf("bridge not fully initialized")
	}

	mxid := id.UserID(matrixMXID)
	loginID := MakeUserLoginID(mmUserID)

	// Check if a full login already exists for this MM user (e.g. auto-login).
	existing := mc.Bridge.GetCachedUserLoginByID(loginID)
	if existing != nil {
		// Already has a UserLogin — just ensure dp mapping and double puppet.
		mc.dpLoginsMu.Lock()
		mc.dpLogins[mmUserID] = loginID
		mc.dpLoginsMu.Unlock()

		user, err := mc.Bridge.GetUserByMXID(ctx, mxid)
		if err != nil {
			return fmt.Errorf("get user for existing login: %w", err)
		}
		if err := user.LoginDoublePuppet(ctx, useConfigASToken); err != nil {
			mc.Bridge.Log.Warn().Err(err).
				Str("mxid", matrixMXID).
				Msg("Double puppet: as_token setup failed for existing login, skipping")
		} else {
			mc.Bridge.Log.Info().
				Str("mm_user_id", mmUserID).
				Str("matrix_mxid", matrixMXID).
				Msg("Double puppet: enabled for existing login")
		}
		return nil
	}

	// Get or create the bridgev2 User for this Matrix MXID.
	user, err := mc.Bridge.GetUserByMXID(ctx, mxid)
	if err != nil {
		return fmt.Errorf("get user by mxid: %w", err)
	}

	// Create a lightweight UserLogin.
	ul, err := user.NewLogin(ctx, &database.UserLogin{
		ID:         loginID,
		RemoteName: fmt.Sprintf("double-puppet:%s", mmUserID),
	}, &bridgev2.NewLoginParams{
		LoadUserLogin: mc.LoadUserLogin,
	})
	if err != nil {
		return fmt.Errorf("create login: %w", err)
	}

	meta := ul.Metadata.(*UserLoginMetadata)
	meta.UserID = mmUserID
	meta.DoublePuppetOnly = true
	if err := ul.Save(ctx); err != nil {
		return fmt.Errorf("save login: %w", err)
	}

	// Set the userID on the client so IsThisUser() matches.
	mmClient := ul.Client.(*KChatClient)
	mmClient.userID = mmUserID

	// Enable double puppeting via as_token.
	if err := user.LoginDoublePuppet(ctx, useConfigASToken); err != nil {
		return fmt.Errorf("login double puppet: %w", err)
	}

	// Register in reverse lookup map.
	mc.dpLoginsMu.Lock()
	mc.dpLogins[mmUserID] = loginID
	mc.dpLoginsMu.Unlock()

	mc.Bridge.Log.Info().
		Str("mm_user_id", mmUserID).
		Str("matrix_mxid", matrixMXID).
		Msg("Double puppet: enabled for user")

	return nil
}

// DoublePuppetLoginID returns the UserLoginID for a given kChat user ID
// if one exists in the double puppet map. Thread-safe.
func (mc *KChatConnector) DoublePuppetLoginID(mmUserID string) (networkid.UserLoginID, bool) {
	mc.dpLoginsMu.RLock()
	defer mc.dpLoginsMu.RUnlock()
	id, ok := mc.dpLogins[mmUserID]
	return id, ok
}

// maxDoublePuppetBodySize is the maximum allowed request body for double puppet registration (64 KB).
const maxDoublePuppetBodySize = 64 << 10

// HandleDoublePuppet is an HTTP handler for POST /api/double-puppet.
// It registers a MM user → Matrix MXID mapping for double puppeting without
// requiring a kChat API token.
func (mc *KChatConnector) HandleDoublePuppet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxDoublePuppetBodySize)
	defer func() { _ = r.Body.Close() }()

	var req struct {
		MMUserID   string `json:"mm_user_id"`
		MatrixMXID string `json:"matrix_mxid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.MMUserID == "" || req.MatrixMXID == "" {
		http.Error(w, "mm_user_id and matrix_mxid are required", http.StatusBadRequest)
		return
	}

	mc.Bridge.Log.Info().
		Str("remote_addr", r.RemoteAddr).
		Str("mm_user_id", req.MMUserID).
		Str("matrix_mxid", req.MatrixMXID).
		Msg("Double puppet registration requested")

	ctx := r.Context()
	if err := mc.setupUserDoublePuppet(ctx, req.MMUserID, req.MatrixMXID); err != nil {
		mc.Bridge.Log.Error().Err(err).
			Str("mm_user_id", req.MMUserID).
			Str("matrix_mxid", req.MatrixMXID).
			Msg("Double puppet registration failed")
		http.Error(w, fmt.Sprintf("failed to setup double puppet: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":      "ok",
		"mm_user_id":  req.MMUserID,
		"matrix_mxid": req.MatrixMXID,
	})
}

func (mc *KChatConnector) LoadUserLogin(_ context.Context, login *bridgev2.UserLogin) error {
	mmClient := NewKChatClient(login, mc)
	login.Client = mmClient
	return nil
}

func (mc *KChatConnector) GetName() bridgev2.BridgeName {
	return bridgev2.BridgeName{
		DisplayName:      "Infomaniak kChat",
		NetworkURL:       "https://www.infomaniak.com/en/kchat",
		NetworkIcon:      "mxc://maunium.net/kchat",
		NetworkID:        "kchat",
		BeeperBridgeType: "io.github.rosch100.kchat",
		DefaultPort:      29325,
	}
}

func (mc *KChatConnector) GetDBMetaTypes() database.MetaTypes {
	return database.MetaTypes{
		UserLogin: func() any {
			return &UserLoginMetadata{}
		},
	}
}

func (mc *KChatConnector) GetCapabilities() *bridgev2.NetworkGeneralCapabilities {
	return &bridgev2.NetworkGeneralCapabilities{
		DisappearingMessages: false,
		AggressiveUpdateInfo: false,
	}
}

func (mc *KChatConnector) GetBridgeInfoVersion() (info, capabilities int) {
	return 1, 1
}

// UserLoginMetadata stores kChat-specific login data.
type UserLoginMetadata struct {
	ServerURL string `json:"server_url"`
	Token     string `json:"token"`
	UserID    string `json:"user_id"`
	TeamID    string `json:"team_id"`

	// DoublePuppetOnly marks this login as a lightweight double-puppet-only
	// entry. It has no MM API client or WebSocket — it exists solely so the
	// bridgev2 framework can match incoming MM events to a real Matrix user
	// and send them via that user's double puppet intent.
	DoublePuppetOnly bool `json:"double_puppet_only,omitempty"`
}

// MakeUserLoginID creates a UserLoginID from a kChat user ID.
func MakeUserLoginID(userID string) networkid.UserLoginID {
	return networkid.UserLoginID(userID)
}

// ParseUserLoginID extracts the kChat user ID from a UserLoginID.
func ParseUserLoginID(loginID networkid.UserLoginID) string {
	return string(loginID)
}

// IsPuppetUserID returns true if the given kChat user ID belongs to
// any loaded puppet bot. Thread-safe.
func (mc *KChatConnector) IsPuppetUserID(mmUserID string) bool {
	mc.puppetMu.RLock()
	defer mc.puppetMu.RUnlock()
	for _, puppet := range mc.Puppets {
		if puppet.UserID == mmUserID {
			return true
		}
	}
	return false
}

// ReloadPuppets re-reads puppet configuration from environment variables and
// updates the Puppets map. New puppets are added, removed env vars cause
// puppet removal. Existing puppets with unchanged tokens are kept as-is.
// Returns the number of added and removed puppets.
func (mc *KChatConnector) ReloadPuppets(ctx context.Context) (added, removed int) {
	entries := mc.envToPuppetEntries()
	return mc.ReloadPuppetsFromEntries(ctx, entries)
}

// envToPuppetEntries scans the current environment for puppet config pairs
// and returns them as PuppetEntry values.
func (mc *KChatConnector) envToPuppetEntries() []PuppetEntry {
	const prefix = "KCHAT_PUPPET_"
	const mxidSuffix = "_MXID"
	const tokenSuffix = "_TOKEN"

	slugs := make(map[string]struct{})
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rest := key[len(prefix):]
		if strings.HasSuffix(rest, mxidSuffix) {
			slug := rest[:len(rest)-len(mxidSuffix)]
			slugs[slug] = struct{}{}
		}
	}

	var entries []PuppetEntry
	for slug := range slugs {
		mxidVal := os.Getenv(prefix + slug + mxidSuffix)
		tokenVal := os.Getenv(prefix + slug + tokenSuffix)
		if mxidVal != "" && tokenVal != "" {
			entries = append(entries, PuppetEntry{Slug: slug, MXID: mxidVal, Token: tokenVal})
		}
	}
	return entries
}

// ReloadPuppetsFromEntries updates the puppet map from an explicit list of
// entries. This is the core reload logic used by both env-based reload and
// the HTTP API endpoint. Thread-safe.
func (mc *KChatConnector) ReloadPuppetsFromEntries(ctx context.Context, entries []PuppetEntry) (added, removed int) {
	// Build desired set from entries.
	desired := make(map[id.UserID]PuppetEntry, len(entries))
	for _, e := range entries {
		desired[id.UserID(e.MXID)] = e
	}

	mc.puppetMu.Lock()
	defer mc.puppetMu.Unlock()

	// Remove puppets that are no longer in the desired set.
	for uid, puppet := range mc.Puppets {
		if _, ok := desired[uid]; !ok {
			mc.Bridge.Log.Info().Str("mxid", string(uid)).Msg("Removing puppet")
			// Remove double puppet mapping for this puppet's MM user.
			mc.dpLoginsMu.Lock()
			delete(mc.dpLogins, puppet.UserID)
			mc.dpLoginsMu.Unlock()
			delete(mc.Puppets, uid)
			removed++
		}
	}

	// Add or update puppets.
	for uid, entry := range desired {
		existing, ok := mc.Puppets[uid]
		if ok && existing.Client != nil && existing.Client.AuthToken == entry.Token {
			// Unchanged -- keep as-is.
			continue
		}

		serverURL := os.Getenv("KCHAT_PUPPET_" + entry.Slug + "_URL")
		if serverURL == "" {
			serverURL = mc.Config.ServerURL
		}

		client := model.NewAPIv4Client(serverURL)
		client.SetToken(entry.Token)

		me, _, err := client.GetMe(ctx, "")
		if err != nil {
			mc.Bridge.Log.Error().Err(err).
				Str("slug", entry.Slug).
				Str("mxid", entry.MXID).
				Msg("Failed to authenticate puppet during reload, skipping")
			continue
		}

		puppet := &PuppetClient{
			MXID:     uid,
			Client:   client,
			UserID:   me.Id,
			Username: me.Username,
		}
		mc.Puppets[uid] = puppet
		added++

		mc.Bridge.Log.Info().
			Str("slug", entry.Slug).
			Str("mxid", entry.MXID).
			Str("mm_user_id", me.Id).
			Str("mm_username", me.Username).
			Msg("Hot-loaded puppet")

		// Set up double puppeting for the new/updated puppet.
		if err := mc.setupUserDoublePuppet(ctx, me.Id, entry.MXID); err != nil {
			mc.Bridge.Log.Warn().Err(err).
				Str("slug", entry.Slug).
				Str("mxid", entry.MXID).
				Msg("Failed to setup double puppet during reload")
		}
	}

	mc.Bridge.Log.Info().
		Int("added", added).
		Int("removed", removed).
		Int("total", len(mc.Puppets)).
		Msg("Puppet reload complete")

	return added, removed
}

// PuppetCount returns the current number of loaded puppets. Thread-safe.
func (mc *KChatConnector) PuppetCount() int {
	mc.puppetMu.RLock()
	defer mc.puppetMu.RUnlock()
	return len(mc.Puppets)
}

// maxReloadBodySize is the maximum allowed request body for puppet reload (1 MB).
const maxReloadBodySize = 1 << 20

// HandleReloadPuppets is an HTTP handler for POST /api/reload-puppets.
// It accepts an optional JSON body with explicit puppet entries; if the body
// is empty or absent, it reloads from environment variables.
func (mc *KChatConnector) HandleReloadPuppets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	mc.Bridge.Log.Info().
		Str("remote_addr", r.RemoteAddr).
		Str("content_length", r.Header.Get("Content-Length")).
		Msg("Puppet reload requested")

	ctx := r.Context()
	var added, removed int

	// Try to read entries from body.
	var entries []PuppetEntry
	if r.Body != nil && r.ContentLength != 0 {
		r.Body = http.MaxBytesReader(w, r.Body, maxReloadBodySize)
		defer func() { _ = r.Body.Close() }()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &entries); err != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}
		}
	}

	mc.Bridge.Log.Info().
		Str("remote_addr", r.RemoteAddr).
		Int("entries", len(entries)).
		Str("source", func() string {
			if len(entries) > 0 {
				return "body"
			}
			return "env"
		}()).
		Msg("Processing puppet reload")

	if len(entries) > 0 {
		added, removed = mc.ReloadPuppetsFromEntries(ctx, entries)
	} else {
		added, removed = mc.ReloadPuppets(ctx)
	}

	resp := map[string]int{
		"added":   added,
		"removed": removed,
		"total":   mc.PuppetCount(),
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		mc.Bridge.Log.Warn().Err(err).Msg("Failed to write reload response")
	}
}

// WatchNewPortals periodically checks for new portal rooms that don't have
// relay set, and sets the relay user on them. This replaces the fixed 3-attempt
// boot cycle with continuous monitoring for rooms created after startup (e.g.,
// when a new PL agent is provisioned).
//
// The interval parameter controls how often the check runs. Pass 0 to use
// the default of 60 seconds.
func (mc *KChatConnector) WatchNewPortals(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 60 * time.Second
	}

	mc.Bridge.Log.Info().
		Dur("interval", interval).
		Msg("Starting WatchNewPortals loop")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			mc.Bridge.Log.Info().Msg("WatchNewPortals stopped")
			return
		case <-ticker.C:
			mc.checkAndSetRelay(ctx)
		}
	}
}

// checkAndSetRelay scans portal rooms and sets relay on any that lack it.
func (mc *KChatConnector) checkAndSetRelay(ctx context.Context) {
	if mc.Bridge == nil || mc.Bridge.DB == nil {
		return
	}
	portals, err := mc.Bridge.GetAllPortalsWithMXID(ctx)
	if err != nil {
		mc.Bridge.Log.Error().Err(err).Msg("WatchNewPortals: failed to get portals")
		return
	}

	// Find the auto-login user to use as relay.
	loginUsers, err := mc.Bridge.DB.UserLogin.GetAllUserIDsWithLogins(ctx)
	if err != nil || len(loginUsers) == 0 {
		return
	}

	setCount := 0
	for _, portal := range portals {
		if portal.Relay == nil {
			// Get any available login to use as relay.
			for _, userID := range loginUsers {
				user, err := mc.Bridge.GetUserByMXID(ctx, userID)
				if err != nil {
					continue
				}
				logins := user.GetUserLogins()
				if len(logins) > 0 {
					if err := portal.SetRelay(ctx, logins[0]); err != nil {
						mc.Bridge.Log.Warn().Err(err).
							Str("portal_mxid", string(portal.MXID)).
							Msg("WatchNewPortals: failed to set relay")
					} else {
						setCount++
					}
					break
				}
			}
		}
	}

	if setCount > 0 {
		mc.Bridge.Log.Info().
			Int("set_count", setCount).
			Int("total_portals", len(portals)).
			Msg("WatchNewPortals: set relay on new portals")
	}
}
