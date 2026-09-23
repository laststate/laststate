// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

// Package latchstream implements the byte stream emitted by Latch's
// ls_stream_transport_send: "LS", version 1, flags, uint32 LEP length,
// raw LEP payload and an IEEE CRC32 of the raw LEP payload.
package latchstream

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"strings"
)

const (
	HeaderSize  = 8
	TrailerSize = 4
	Version     = 1
	MaxFrame    = 4 << 20
)

type Decoder struct {
	buffer []byte
	max    int
}

type DecodeErrors []error

func (errs DecodeErrors) Error() string {
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		parts = append(parts, err.Error())
	}
	return strings.Join(parts, "; ")
}

func NewDecoder(max int) *Decoder {
	if max <= 0 || max > MaxFrame {
		max = MaxFrame
	}
	return &Decoder{max: max}
}

// Push returns every valid frame available in the supplied bytes. Corrupt
// frames are reported after the decoder resynchronizes, so a valid frame later
// in the same read is never left stranded waiting for another byte.
func (decoder *Decoder) Push(data []byte) (frames [][]byte, err error) {
	if len(data) > 0 {
		decoder.buffer = append(decoder.buffer, data...)
	}
	var problems DecodeErrors
	for len(decoder.buffer) >= HeaderSize {
		if decoder.buffer[0] != 'L' || decoder.buffer[1] != 'S' {
			decoder.buffer = decoder.buffer[1:]
			continue
		}
		if decoder.buffer[2] != Version || decoder.buffer[3] != 0 {
			problems = append(problems, fmt.Errorf("unsupported Latch stream header version=%d flags=0x%02x", decoder.buffer[2], decoder.buffer[3]))
			decoder.buffer = decoder.buffer[1:]
			continue
		}
		length := int(binary.LittleEndian.Uint32(decoder.buffer[4:8]))
		if length <= 0 || length > decoder.max {
			problems = append(problems, fmt.Errorf("latch stream frame length %d exceeds limit %d", length, decoder.max))
			decoder.buffer = decoder.buffer[1:]
			continue
		}
		total := HeaderSize + length + TrailerSize
		if len(decoder.buffer) < total {
			break
		}
		payload := append([]byte(nil), decoder.buffer[HeaderSize:HeaderSize+length]...)
		expected := binary.LittleEndian.Uint32(decoder.buffer[HeaderSize+length : total])
		decoder.buffer = decoder.buffer[total:]
		if crc32.ChecksumIEEE(payload) != expected {
			problems = append(problems, fmt.Errorf("invalid Latch stream payload CRC"))
			continue
		}
		frames = append(frames, payload)
	}
	if len(problems) > 0 {
		return frames, problems
	}
	return frames, nil
}

func (decoder *Decoder) Reset()        { decoder.buffer = nil }
func (decoder *Decoder) Buffered() int { return len(decoder.buffer) }

// Ack is an optional control profile for integrations that need an on-wire
// response from Relay. Its fixed binary form is "LSAK", version, status,
// reserved uint16, event ID.
type AckStatus uint8

// AckStatus values match spec/framing.md in the protocol repo (1..8).
const (
	AckStored        AckStatus = 1
	AckDuplicate     AckStatus = 2
	NackCorrupt      AckStatus = 3
	NackUnsupported  AckStatus = 4
	NackBusy         AckStatus = 5
	NackTooLarge     AckStatus = 6
	NackUnauthorized AckStatus = 7
	NackInternal     AckStatus = 8
)

func Ack(eventID uint32, status AckStatus) []byte {
	data := []byte{'L', 'S', 'A', 'K', 1, byte(status), 0, 0, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(data[8:], eventID)
	return data
}
