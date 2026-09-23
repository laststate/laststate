// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package lep

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
)

// ParseKeyMaterial decodes a 32-byte key from hex (64 chars) or standard/raw base64.
// Leading "hex:" or "base64:" prefixes are accepted.
func ParseKeyMaterial(reference string) ([]byte, error) {
	value := strings.TrimSpace(reference)
	if value == "" {
		return nil, fmt.Errorf("empty key material")
	}
	switch {
	case strings.HasPrefix(value, "hex:"):
		raw, err := hex.DecodeString(strings.TrimSpace(strings.TrimPrefix(value, "hex:")))
		if err != nil {
			return nil, fmt.Errorf("decode hex key: %w", err)
		}
		if len(raw) != chacha20poly1305.KeySize {
			return nil, fmt.Errorf("key must be %d bytes, got %d", chacha20poly1305.KeySize, len(raw))
		}
		return raw, nil
	case strings.HasPrefix(value, "base64:"):
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(value, "base64:")))
		if err != nil {
			return nil, fmt.Errorf("decode base64 key: %w", err)
		}
		if len(raw) != chacha20poly1305.KeySize {
			return nil, fmt.Errorf("key must be %d bytes, got %d", chacha20poly1305.KeySize, len(raw))
		}
		return raw, nil
	default:
		if raw, err := hex.DecodeString(value); err == nil && len(raw) == chacha20poly1305.KeySize {
			return raw, nil
		}
		if raw, err := base64.StdEncoding.DecodeString(value); err == nil && len(raw) == chacha20poly1305.KeySize {
			return raw, nil
		}
		if raw, err := base64.RawStdEncoding.DecodeString(value); err == nil && len(raw) == chacha20poly1305.KeySize {
			return raw, nil
		}
		return nil, fmt.Errorf("key material must be 32-byte hex or base64")
	}
}

// ParseNumericKeyID maps a config key id string to Latch's u32 key_id.
// Decimal numbers parse directly; other strings use the first 4 LE bytes of SHA-256.
func ParseNumericKeyID(id string) uint32 {
	id = strings.TrimSpace(id)
	if id == "" {
		return 0
	}
	if n, err := strconv.ParseUint(id, 10, 32); err == nil {
		return uint32(n)
	}
	sum := sha256.Sum256([]byte(id))
	return binary.LittleEndian.Uint32(sum[:4])
}

// BuildMemoryKeyring constructs a keyring from keys with NumericID set.
// activeID/authID are config strings (decimal key ids recommended for Latch interop).
func BuildMemoryKeyring(entries []Key, activeID, authID string) (*MemoryKeyring, error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("at least one key is required")
	}
	normalized := make([]Key, 0, len(entries))
	for _, entry := range entries {
		if len(entry.Key) != chacha20poly1305.KeySize {
			return nil, fmt.Errorf("key %d has invalid length %d", entry.NumericID, len(entry.Key))
		}
		if entry.NumericID == 0 && entry.ID != ([KeyIDSize]byte{}) {
			entry.NumericID = NumericIDFromKeyID(entry.ID)
		}
		if entry.ID == ([KeyIDSize]byte{}) {
			entry.ID = KeyIDFromNumeric(entry.NumericID)
		}
		normalized = append(normalized, entry)
	}
	active := ParseNumericKeyID(activeID)
	auth := ParseNumericKeyID(authID)
	if activeID == "" {
		active = normalized[0].NumericID
	}
	if authID == "" {
		auth = active
	}
	ring := NewMemoryKeyring(normalized, active, auth)
	if !ring.HaveActive {
		return nil, fmt.Errorf("active key id %d not found in key set", active)
	}
	if !ring.HaveAuth {
		return nil, fmt.Errorf("auth key id %d not found in key set", auth)
	}
	return ring, nil
}
