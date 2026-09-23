// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package lep

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

// Encode builds a plain (unauthenticated, unencrypted, uncompressed) LEP v1/v2
// envelope from header fields and TLV payload bytes. version 0 defaults to 2.
func Encode(version, eventType, architecture uint8, sequence, eventID uint32, payload []byte) ([]byte, error) {
	if version == 0 {
		version = Version2
	}
	if version != Version1 && version != Version2 {
		return nil, fmt.Errorf("unsupported LEP version %d", version)
	}
	if len(payload) > MaxEnvelopeSize-HeaderSize-4 {
		return nil, fmt.Errorf("payload exceeds maximum envelope size")
	}
	if err := validateTLVs(payload); err != nil && len(payload) > 0 {
		return nil, err
	}
	raw := make([]byte, HeaderSize+len(payload)+4)
	copy(raw[0:4], Magic)
	raw[4] = version
	raw[5] = eventType
	raw[6] = architecture
	raw[7] = 0
	binary.LittleEndian.PutUint32(raw[8:12], sequence)
	binary.LittleEndian.PutUint32(raw[12:16], eventID)
	binary.LittleEndian.PutUint32(raw[16:20], uint32(len(payload)))
	binary.LittleEndian.PutUint32(raw[20:24], crc32.ChecksumIEEE(raw[:20]))
	copy(raw[HeaderSize:], payload)
	binary.LittleEndian.PutUint32(raw[HeaderSize+len(payload):], crc32.ChecksumIEEE(payload))
	return raw, nil
}

// EncodeTLVs serializes TLVs then Encode.
func EncodeTLVs(version, eventType, architecture uint8, sequence, eventID uint32, fields []TLV) ([]byte, error) {
	payload, err := MarshalTLVs(fields)
	if err != nil {
		return nil, err
	}
	return Encode(version, eventType, architecture, sequence, eventID, payload)
}

// MarshalTLVs encodes TLV fields to payload bytes.
func MarshalTLVs(fields []TLV) ([]byte, error) {
	var payload []byte
	for _, field := range fields {
		if field.Type == 0 {
			return nil, fmt.Errorf("TLV type zero is reserved")
		}
		if len(field.Value) > 0xffff {
			return nil, fmt.Errorf("TLV %d value exceeds uint16 length", field.Type)
		}
		entry := make([]byte, 4+len(field.Value))
		binary.LittleEndian.PutUint16(entry[0:2], field.Type)
		binary.LittleEndian.PutUint16(entry[2:4], uint16(len(field.Value)))
		copy(entry[4:], field.Value)
		payload = append(payload, entry...)
	}
	return payload, nil
}
