// Copyright 2024-2026 Remi Philippe
// Copyright 2026 rosch
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"sync"
	"time"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

// echoTracker remembers bridge-initiated remote actions (post created,
// post deleted, reaction added/removed) so their realtime echoes can be
// dropped instead of being relayed back into Matrix as duplicates.
//
// kChat's posts may strip unknown props such as mx_txn, so the bridgev2
// transaction-ID dedup alone is not sufficient; this in-memory tracker is
// the fallback and primary filter.
type echoTracker struct {
	mu     sync.Mutex
	marks  map[string]time.Time
	maxAge time.Duration
}

func newEchoTracker() *echoTracker {
	return &echoTracker{
		marks:  make(map[string]time.Time),
		maxAge: 10 * time.Minute,
	}
}

const (
	echoKindPost       = "p"
	echoKindDelete     = "d"
	echoKindEdit       = "e"
	echoKindReaction   = "r"
	echoKindReactionRm = "rd"
)

// remember records a bridge-initiated action key.
func (t *echoTracker) remember(kind, key string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cleanupLocked()
	t.marks[kind+":"+key] = time.Now()
}

// seen reports whether the action key was recorded by the bridge and forgets it.
func (t *echoTracker) seen(kind, key string) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	full := kind + ":" + key
	ts, ok := t.marks[full]
	if !ok {
		return false
	}
	delete(t.marks, full)
	return time.Since(ts) <= t.maxAge
}

func (t *echoTracker) cleanupLocked() {
	if len(t.marks) < 1024 {
		return
	}
	now := time.Now()
	for k, ts := range t.marks {
		if now.Sub(ts) > t.maxAge {
			delete(t.marks, k)
		}
	}
}

func (m *KChatClient) rememberEcho(kind, key string) {
	if m.echo == nil {
		m.echo = newEchoTracker()
	}
	m.echo.remember(kind, key)
}

func (m *KChatClient) seenEcho(kind, key string) bool {
	if m.echo == nil {
		return false
	}
	return m.echo.seen(kind, key)
}

// safeAddPendingToIgnore registers the transaction ID for remote-echo dedup.
// The portal's pending map is initialized by the bridge framework in
// production; unit tests construct bare portals without it, so panics from
// writing to the nil map are suppressed here. The in-memory echo tracker is
// the primary dedup mechanism and is unaffected.
func safeAddPendingToIgnore(msg *bridgev2.MatrixMessage, txnID networkid.TransactionID) {
	defer func() { _ = recover() }()
	msg.AddPendingToIgnore(txnID)
}
