// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package traceapi

import (
	"encoding/binary"
	"fmt"
)

const BinaryBatchMagic = "LSBT"
const BinaryBatchVersion = 1
const BinaryBatchFlagZstd uint8 = 1 << 0

// EncodeBinaryBatch builds an uncompressed binary batch body.
func EncodeBinaryBatch(events []BatchEvent) ([]byte, error) {
	if len(events) > 0xffffffff {
		return nil, fmt.Errorf("too many events")
	}
	size := 12
	for _, event := range events {
		if len(event.EventID) > 0xffff {
			return nil, fmt.Errorf("event id too long")
		}
		size += 2 + len(event.EventID) + 4 + len(event.Payload)
	}
	out := make([]byte, size)
	copy(out[0:4], BinaryBatchMagic)
	out[4] = BinaryBatchVersion
	out[5] = 0
	binary.LittleEndian.PutUint16(out[6:8], 0)
	binary.LittleEndian.PutUint32(out[8:12], uint32(len(events)))
	offset := 12
	for _, event := range events {
		binary.LittleEndian.PutUint16(out[offset:offset+2], uint16(len(event.EventID)))
		offset += 2
		copy(out[offset:], event.EventID)
		offset += len(event.EventID)
		binary.LittleEndian.PutUint32(out[offset:offset+4], uint32(len(event.Payload)))
		offset += 4
		copy(out[offset:], event.Payload)
		offset += len(event.Payload)
	}
	return out, nil
}

// DecodeBinaryBatch parses an uncompressed binary batch body.
func DecodeBinaryBatch(data []byte) ([]BatchEvent, error) {
	if len(data) < 12 || string(data[0:4]) != BinaryBatchMagic {
		return nil, fmt.Errorf("invalid binary batch magic")
	}
	if data[4] != BinaryBatchVersion {
		return nil, fmt.Errorf("unsupported binary batch version %d", data[4])
	}
	if data[5]&BinaryBatchFlagZstd != 0 {
		return nil, fmt.Errorf("binary batch body is compressed; decompress first")
	}
	count := binary.LittleEndian.Uint32(data[8:12])
	offset := 12
	events := make([]BatchEvent, 0, count)
	for i := uint32(0); i < count; i++ {
		if offset+2 > len(data) {
			return nil, fmt.Errorf("truncated event id length")
		}
		idLen := int(binary.LittleEndian.Uint16(data[offset : offset+2]))
		offset += 2
		if offset+idLen+4 > len(data) {
			return nil, fmt.Errorf("truncated event id")
		}
		id := string(data[offset : offset+idLen])
		offset += idLen
		payloadLen := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
		offset += 4
		if offset+payloadLen > len(data) {
			return nil, fmt.Errorf("truncated event payload")
		}
		payload := append([]byte(nil), data[offset:offset+payloadLen]...)
		offset += payloadLen
		events = append(events, BatchEvent{EventID: id, Payload: payload})
	}
	return events, nil
}
