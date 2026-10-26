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

	writeMu           sync.Mutex
	pendingChannel    []string
	pendingChannelData map[string]string
	subscribed        bool

	// EventChannel receives normalized model.WebSocketEvent values.
	EventChannel chan *model.WebSocketEvent
	stopChan     chan struct{}
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
		client:      m,
		conn:        conn,
		authToken:   m.client.AuthToken,
		serverURL:   m.serverURL,
		teamID:      m.teamID,
		userID:      m.userID,
		httpClient:  &http.Client{Timeout: 15 * time.Second},
		log:         m.log.With().Str("component", "pusher_socket").Logger(),
		EventChannel: make(chan *model.WebSocketEvent, 256),
		stopChan:    m.stopChan,
	}, nil
}

// Listen runs the socket read loop and the subscription handshake. It blocks
// until the socket closes, the bridge stops, or an error occurs.
func (p *pusherSocket) Listen() error {
	go p.subscribeAfterConnect()
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
	for _, channel := range p.pendingChannel {
		p.subscribe(channel)
	}
	p.subscribed = true
	p.log.Info().Strs("channels", p.pendingChannel).Msg("Subscribed to kChat realtime channels")
}

// setPendingChannels queues the channels to subscribe after the handshake.
// Callers pass the team ID and user ID; both are required for full coverage.
func (p *pusherSocket) setPendingChannels(teamID, userID string) {
	p.pendingChannel = []string{
		"private-team." + teamID,
		"presence-user." + userID,
	}
}

// subscribe exchanges socket_id and channel_name for a signed subscription
// at the kChat broadcasting endpoint, then subscribes on the socket.
func (p *pusherSocket) subscribe(channel string) {
	auth, channelData, err := p.authorizeChannel(channel)
	if err != nil {
		p.log.Error().Err(err).Str("channel", channel).Msg("Channel authorization failed")
		return
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
	}
}

// authorizeChannel requests the signed auth string for a channel.
func (p *pusherSocket) authorizeChannel(channel string) (auth, channelData string, err error) {
	form := url.Values{
		"socket_id":    {p.socketID},
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
	switch frame.Event {
	case "pusher:connection_established":
		var connData struct {
			SocketID string `json:"socket_id"`
		}
		if err := json.Unmarshal(frame.Data, &connData); err != nil {
			p.log.Error().Err(err).Msg("Failed to parse connection_established")
			return
		}
		p.socketID = connData.SocketID
		p.log.Info().Str("socket_id", p.socketID).Msg("Pusher connection established")
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
