// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package lep

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
)

// ReplayCache detects recycled (source, event_id) pairs that carry a different payload.
// Exact retransmissions of the same bytes are allowed (content-address duplicate path).
type ReplayCache struct {
	mu     sync.Mutex
	window int
	order  []string
	// seen maps composite key → first-seen SHA-256 hex of payload bytes.
	seen map[string]string
}

// NewReplayCache creates a bounded LRU-ish window of recent event IDs.
func NewReplayCache(window int) *ReplayCache {
	if window <= 0 {
		window = 10000
	}
	return &ReplayCache{
		window: window,
		seen:   make(map[string]string, window),
	}
}

// Check reports whether the payload conflicts with a prior observation.
// same=true means identical bytes (safe retransmit). conflict=true means replay attack.
func (cache *ReplayCache) Check(sourceID string, eventID uint32, payload []byte) (same, conflict bool) {
	if cache == nil {
		return false, false
	}
	key := replayKey(sourceID, eventID)
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	cache.mu.Lock()
	defer cache.mu.Unlock()
	prior, ok := cache.seen[key]
	if !ok {
		return false, false
	}
	if prior == hash {
		return true, false
	}
	return false, true
}

// Record stores the payload hash for (source, event_id).
func (cache *ReplayCache) Record(sourceID string, eventID uint32, payload []byte) {
	if cache == nil {
		return
	}
	key := replayKey(sourceID, eventID)
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if _, ok := cache.seen[key]; ok {
		return
	}
	cache.seen[key] = hash
	cache.order = append(cache.order, key)
	for len(cache.order) > cache.window {
		old := cache.order[0]
		cache.order = cache.order[1:]
		delete(cache.seen, old)
	}
}

// Seen reports whether the event_id was recorded (any payload). Prefer Check for decisions.
func (cache *ReplayCache) Seen(sourceID string, eventID uint32) bool {
	if cache == nil {
		return false
	}
	key := replayKey(sourceID, eventID)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	_, ok := cache.seen[key]
	return ok
}

func replayKey(sourceID string, eventID uint32) string {
	return fmt.Sprintf("%s\x00%x", sourceID, eventID)
}
