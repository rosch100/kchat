// Copyright 2024-2026 Remi Philippe
// Copyright 2026 rosch
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2/status"

	"github.com/mattermost/mattermost/server/public/model"
)

// kChat does not implement the Mattermost /api/v4/websocket route. Its webapp
// receives realtime updates over a Pusher-compatible socket instead:
//
//   - wss://websocket.kchat.infomaniak.com/app/kchat-key (Laravel Echo)
//   - subscriptions to private-team.<teamID> and presence-user.<userID>
//   - per-subscription authorization via POST <server>/broadcasting/auth
//     with the kChat access token as bearer.
//
// pusherSocket implements that protocol and converts incoming events into
// model.WebSocketEvent values so the existing event pipeline keeps working.
type pusherSocket struct {
	client     *KChatClient
	conn       *websocket.Conn
	socketID   string
	authToken  string
	serverURL  string
	teamID     string
	userID     string
	httpClient *http.Client
	log        zerolog.Logger

	writeMu        sync.Mutex
	stateMu        sync.Mutex
	pendingChannel []string
	extraChannels  []string
	subscribed     bool

	// EventChannel receives normalized model.WebSocketEvent values.
	EventChannel chan *model.WebSocketEvent
	stopChan     chan struct{}
	// activityTimeout is the server-provided inactivity window after which
	// the socket is closed (pusher close code 4201) unless the client pings.
	activityTimeout time.Duration
}

const (
	kchatWSHost  = "websocket.kchat.infomaniak.com"
	kchatAppKey  = "kchat-key"
	pusherClient = "go-kchat-bridge"
	pusherVer    = "1.0.0"
)

// newPusherSocket dials the kChat realtime socket. The handshake
// (connection_established + channel subscriptions) runs in Listen().
func newPusherSocket(m *KChatClient) (*pusherSocket, error) {
	u := url.URL{
		Scheme: "wss",
		Host:   kchatWSHost,
		Path:   "/app/" + kchatAppKey,
		RawQuery: url.Values{
			"protocol": {"7"},
			"client":   {pusherClient},
			"version":  {pusherVer},
			"flash":    {"false"},
		}.Encode(),
	}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to dial %s: %w", u.String(), err)
	}
	return &pusherSocket{
		client:       m,
		conn:         conn,
		authToken:    m.client.AuthToken,
		serverURL:    m.serverURL,
		teamID:       m.teamID,
		userID:       m.userID,
		httpClient:   &http.Client{Timeout: 15 * time.Second},
		log:          m.log.With().Str("component", "pusher_socket").Logger(),
		EventChannel: make(chan *model.WebSocketEvent, 256),
		stopChan:     m.stopChan,
	}, nil
}

// Listen runs the socket read loop and the subscription handshake. It blocks
// until the socket closes, the bridge stops, or an error occurs.
func (p *pusherSocket) Listen() error {
	go p.subscribeAfterConnect()
	go p.pingLoop()
	for {
		select {
		case <-p.stopChan:
			_ = p.conn.Close()
			return nil
		default:
		}
		msgType, data, err := p.conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("websocket read failed: %w", err)
		}
		if msgType != websocket.TextMessage {
			continue
		}
		p.handleFrame(data)
	}
}

// pingLoop keeps the socket alive: pusher servers close connections that
// stay silent for activity_timeout (close code 4201).
func (p *pusherSocket) pingLoop() {
	p.stateMu.Lock()
	interval := p.activityTimeout / 2
	p.stateMu.Unlock()
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopChan:
			return
		case <-ticker.C:
			if err := p.writeJSON(map[string]any{"event": "pusher:ping", "data": map[string]any{}}); err != nil {
				p.log.Warn().Err(err).Msg("Failed to send pusher ping")
				return
			}
		}
	}
}

// subscribeAfterConnect waits for pusher:connection_established, then
// authorizes and subscribes the channels used by the kChat webapp.
func (p *pusherSocket) subscribeAfterConnect() {
	deadline := time.After(2 * time.Minute)
	for p.socketID == "" {
		select {
		case <-p.stopChan:
			return
		case <-deadline:
			p.log.Error().Msg("Timed out waiting for pusher connection_established")
			_ = p.conn.Close()
			return
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
	p.stateMu.Lock()
	channels := append(append([]string{}, p.pendingChannel...), p.extraChannels...)
	p.stateMu.Unlock()
	for _, channel := range channels {
		if err := p.subscribe(channel); err != nil {
			// Presence channels may be denied by kChat's broadcasting policy;
			// message events arrive on the team channel, so this is not fatal.
			p.log.Warn().Err(err).Str("channel", channel).Msg("Channel subscription failed")
			continue
		}
	}
	p.stateMu.Lock()
	p.subscribed = true
	p.stateMu.Unlock()
	p.log.Info().Strs("channels", channels).Msg("Subscribed to kChat realtime channels")
}

// setPendingChannels queues the channels to subscribe after the handshake.
// Callers pass the team ID and user ID; both are required for full coverage.
func (p *pusherSocket) setPendingChannels(teamID, userID string) {
	p.pendingChannel = []string{
		"private-team." + teamID,
		"presence-user." + userID,
	}
}

// subscribeChannel subscribes to an additional channel. If the handshake has
// not completed yet, the request is queued and replayed afterwards so
// subscriptions requested during startup are never lost.
func (p *pusherSocket) subscribeChannel(channel string) {
	if p == nil {
		return
	}
	p.stateMu.Lock()
	ready := p.socketID != ""
	if !ready {
		p.extraChannels = append(p.extraChannels, channel)
	}
	p.stateMu.Unlock()
	if ready {
		p.subscribe(channel)
	}
}

// setExtraChannels queues channels to subscribe after the handshake
// (re-applied by the caller on every reconnect).
func (p *pusherSocket) setExtraChannels(channelIDs []string) {
	if p == nil || len(channelIDs) == 0 {
		return
	}
	p.stateMu.Lock()
	p.extraChannels = append(p.extraChannels, channelIDs...)
	channels := append([]string{}, p.extraChannels...)
	ready := p.socketID != ""
	p.stateMu.Unlock()
	if ready {
		for _, ch := range channels {
			p.subscribe(ch)
		}
	}
}

// subscribe exchanges socket_id and channel_name for a signed subscription
// at the kChat broadcasting endpoint, then subscribes on the socket.
func (p *pusherSocket) subscribe(channel string) error {
	auth, channelData, err := p.authorizeChannel(channel)
	if err != nil {
		return err
	}
	subData := map[string]any{
		"channel": channel,
		"auth":    auth,
	}
	if channelData != "" {
		subData["channel_data"] = channelData
	}
	if err := p.writeJSON(map[string]any{"event": "pusher:subscribe", "data": subData}); err != nil {
		p.log.Error().Err(err).Str("channel", channel).Msg("Failed to send subscription")
		return err
	}
	return nil
}

// authorizeChannel requests the signed auth string for a channel.
func (p *pusherSocket) authorizeChannel(channel string) (auth, channelData string, err error) {
	p.stateMu.Lock()
	socketID := p.socketID
	p.stateMu.Unlock()
	form := url.Values{
		"socket_id":    {socketID},
		"channel_name": {channel},
	}
	req, reqErr := http.NewRequest(http.MethodPost, p.serverURL+"/broadcasting/auth", strings.NewReader(form.Encode()))
	if reqErr != nil {
		return "", "", reqErr
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+p.authToken)
	resp, doErr := p.httpClient.Do(req)
	if doErr != nil {
		return "", "", fmt.Errorf("broadcasting auth request failed: %w", doErr)
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return "", "", readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("broadcasting auth returned HTTP %d: %s", resp.StatusCode, string(body))
	}
	var parsed struct {
		Auth        string `json:"auth"`
		ChannelData string `json:"channel_data"`
	}
	if parseErr := json.Unmarshal(body, &parsed); parseErr != nil {
		return "", "", fmt.Errorf("failed to parse broadcasting auth response: %w", parseErr)
	}
	if parsed.Auth == "" {
		return "", "", fmt.Errorf("broadcasting auth response has no auth signature: %s", string(body))
	}
	return parsed.Auth, parsed.ChannelData, nil
}

// unmarshalPusherData decodes pusher event data, which arrives either as a
// JSON string containing the payload or as the payload object directly.
func unmarshalPusherData(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		return fmt.Errorf("empty pusher data")
	}
	if err := json.Unmarshal(raw, target); err == nil {
		return nil
	}
	var dataStr string
	if err := json.Unmarshal(raw, &dataStr); err != nil {
		return err
	}
	return json.Unmarshal([]byte(dataStr), target)
}

// handleFrame processes a single raw pusher frame. App events carry their
// payload as a JSON-encoded string roughly matching the Mattermost
// WebSocketEvent layout, so they are normalized into model events.
func (p *pusherSocket) handleFrame(raw []byte) {
	var frame struct {
		Event   string          `json:"event"`
		Channel string          `json:"channel"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		p.log.Warn().Err(err).Msg("Failed to parse pusher frame")
		return
	}
	if !strings.HasPrefix(frame.Event, "pusher:") {
		p.log.Debug().Str("event", frame.Event).Str("channel", frame.Channel).Msg("Pusher app event")
	} else if frame.Event != "pusher:pong" {
		p.log.Debug().Str("event", frame.Event).Str("channel", frame.Channel).Msg("Pusher control frame")
	}
	switch frame.Event {
	case "pusher:connection_established":
		var connData struct {
			SocketID        string `json:"socket_id"`
			ActivityTimeout int    `json:"activity_timeout"`
		}
		if err := unmarshalPusherData(frame.Data, &connData); err != nil {
			p.log.Error().Err(err).RawJSON("data", frame.Data).Msg("Failed to parse connection_established")
			return
		}
		p.socketID = connData.SocketID
		if connData.ActivityTimeout > 0 {
			p.activityTimeout = time.Duration(connData.ActivityTimeout) * time.Second
		}
		p.log.Info().Str("socket_id", p.socketID).Dur("activity_timeout", p.activityTimeout).Msg("Pusher connection established")
	case "pusher:ping":
		if err := p.writeJSON(map[string]any{"event": "pusher:pong"}); err != nil {
			p.log.Warn().Err(err).Msg("Failed to answer pusher ping")
		}
	case "pusher:subscription_error":
		p.log.Error().Str("channel", frame.Channel).RawJSON("data", frame.Data).Msg("Pusher subscription rejected")
		p.client.userLogin.BridgeState.Send(status.BridgeState{
			StateEvent: status.StateBadCredentials,
			Error:      "kchat-subscription-rejected",
			Message:    "kChat rejected the realtime subscription (token permissions?)",
		})
	case "pusher:error":
		p.log.Error().RawJSON("data", frame.Data).Msg("Pusher protocol error")
	case "pusher:pong", "pusher:internal:subscription_succeeded":
		// informational only
	default:
		p.dispatchAppEvent(frame.Event, frame.Data)
	}
}

// dispatchAppEvent normalizes a kChat app event and queues it for handling.
func (p *pusherSocket) dispatchAppEvent(eventName string, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var dataStr string
	var payload map[string]any
	if err := json.Unmarshal(raw, &dataStr); err != nil {
		if err := json.Unmarshal(raw, &payload); err != nil {
			p.log.Warn().Err(err).Str("event", eventName).Msg("Failed to parse event payload")
			return
		}
	} else if err := json.Unmarshal([]byte(dataStr), &payload); err != nil {
		p.log.Warn().Err(err).Str("event", eventName).Msg("Failed to parse event payload string")
		return
	}

	broadcast, _ := payload["broadcast"].(map[string]any)
	if broadcast == nil {
		broadcast = make(map[string]any)
	}
	wsEvent := model.NewWebSocketEvent("", "", "", "", nil, "").SetData(make(map[string]any))

	// Determine the channel ID for the broadcast metadata and hoist the
	// nested "data" object (kChat wraps some payloads one level deeper than
	// Mattermost does).
	channelID := ""
	if post, ok := payload["post"].(map[string]any); ok {
		channelID, _ = post["channel_id"].(string)
		// The event handlers expect stringified JSON objects.
		if encoded, err := json.Marshal(post); err == nil {
			payload["post"] = string(encoded)
		}
	}
	if nested, ok := payload["data"].(map[string]any); ok {
		for _, key := range []string{"channel_id", "user_id", "parent_id"} {
			if v, ok := nested[key]; ok {
				wsEvent.Add(key, v)
			}
		}
		if channelID == "" {
			channelID, _ = nested["channel_id"].(string)
		}
	}
	if cid, ok := payload["channel_id"].(string); ok && channelID == "" {
		channelID = cid
	}
	if reaction, ok := payload["reaction"].(map[string]any); ok {
		if encoded, err := json.Marshal(reaction); err == nil {
			payload["reaction"] = string(encoded)
		}
	}
	broadcast["channel_id"] = channelID

	for k, v := range payload {
		if k != "broadcast" {
			wsEvent.Add(k, v)
		}
	}
	bc := model.WebsocketBroadcast{}
	if cid, ok := broadcast["channel_id"].(string); ok {
		bc.ChannelId = cid
	}
	if uid, ok := broadcast["user_id"].(string); ok {
		bc.UserId = uid
	}
	if tid, ok := broadcast["team_id"].(string); ok {
		bc.TeamId = tid
	}
	wsEvent.SetBroadcast(&bc).SetEvent(model.WebsocketEventType(eventName))

	select {
	case p.EventChannel <- wsEvent:
	default:
		p.log.Warn().Str("event", eventName).Msg("Event channel full, dropping event")
	}
}

func (p *pusherSocket) writeJSON(v any) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		return err
	}
	return p.conn.WriteMessage(websocket.TextMessage, buf.Bytes())
}
