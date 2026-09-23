// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

// Package privacy builds destination-specific LEP copies without changing local raw.
package privacy

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"

	"github.com/laststate/relay/internal/lep"
)

type Policy struct {
	DropTLVTypes    []uint16
	MaxPayloadBytes int
	BlockEncrypted  bool
}

func (policy Policy) Active() bool {
	return len(policy.DropTLVTypes) > 0 || policy.MaxPayloadBytes > 0 || policy.BlockEncrypted
}

// Apply creates a destination-specific derivative while preserving the raw
// event in the local object store. Authenticated, encrypted, or compressed
// envelopes cannot be rewritten without the corresponding protocol keys/codecs.
func (policy Policy) Apply(raw []byte) ([]byte, error) {
	envelope, err := lep.Validate(raw)
	if err != nil {
		return nil, err
	}
	if policy.BlockEncrypted && envelope.Flags&lep.FlagEncrypted != 0 {
		return nil, fmt.Errorf("privacy policy blocks encrypted events for this destination")
	}
	if len(policy.DropTLVTypes) == 0 && policy.MaxPayloadBytes <= 0 {
		return append([]byte(nil), raw...), nil
	}
	if envelope.Flags&(lep.FlagAuthenticated|lep.FlagEncrypted|lep.FlagCompressed) != 0 {
		return nil, fmt.Errorf("privacy transformation requires a plain, unauthenticated LEP payload")
	}
	fields, err := lep.TLVs(raw)
	if err != nil {
		return nil, err
	}
	drop := make(map[uint16]bool, len(policy.DropTLVTypes))
	for _, typeID := range policy.DropTLVTypes {
		drop[typeID] = true
	}
	payload := make([]byte, 0, envelope.PayloadLength)
	for _, field := range fields {
		if drop[field.Type] {
			continue
		}
		if len(field.Value) > 0xffff {
			return nil, fmt.Errorf("TLV %d exceeds uint16 length", field.Type)
		}
		entry := make([]byte, 4+len(field.Value))
		binary.LittleEndian.PutUint16(entry[0:2], field.Type)
		binary.LittleEndian.PutUint16(entry[2:4], uint16(len(field.Value)))
		copy(entry[4:], field.Value)
		if policy.MaxPayloadBytes > 0 && len(payload)+len(entry) > policy.MaxPayloadBytes {
			continue
		}
		payload = append(payload, entry...)
	}
	result := make([]byte, lep.HeaderSize+len(payload)+4)
	copy(result[:20], raw[:20])
	binary.LittleEndian.PutUint32(result[16:20], uint32(len(payload)))
	binary.LittleEndian.PutUint32(result[20:24], crc32.ChecksumIEEE(result[:20]))
	copy(result[lep.HeaderSize:], payload)
	binary.LittleEndian.PutUint32(result[lep.HeaderSize+len(payload):], crc32.ChecksumIEEE(payload))
	return result, nil
}
