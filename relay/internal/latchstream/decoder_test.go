// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package latchstream

import (
	"encoding/binary"
	"hash/crc32"
	"testing"
)

func TestDecoderMatchesLatchStreamLayout(t *testing.T) {
	payload := []byte{1, 2, 3}
	frame := append([]byte{'L', 'S', 1, 0, 3, 0, 0, 0}, payload...)
	trailer := make([]byte, 4)
	binary.LittleEndian.PutUint32(trailer, crc32.ChecksumIEEE(payload))
	frame = append(frame, trailer...)
	decoder := NewDecoder(32)
	frames, err := decoder.Push(frame[:5])
	if err != nil || len(frames) != 0 {
		t.Fatalf("partial frame: %v %#v", err, frames)
	}
	frames, err = decoder.Push(frame[5:])
	if err != nil || len(frames) != 1 || string(frames[0]) != string(payload) {
		t.Fatalf("decoded: %v %#v", err, frames)
	}
}

func TestAckFormat(t *testing.T) {
	ack := Ack(9, AckStored)
	if string(ack[:4]) != "LSAK" || binary.LittleEndian.Uint32(ack[8:]) != 9 {
		t.Fatalf("ack: %x", ack)
	}
}

func TestDecoderRecoversWithinSameRead(t *testing.T) {
	bad := makeStreamFrame([]byte{9, 9, 9})
	bad[len(bad)-1] ^= 0xff
	goodPayload := []byte{4, 5, 6}
	good := makeStreamFrame(goodPayload)

	decoder := NewDecoder(64)
	frames, err := decoder.Push(append(bad, good...))
	if err == nil {
		t.Fatal("expected corrupt frame to be reported")
	}
	if len(frames) != 1 || string(frames[0]) != string(goodPayload) {
		t.Fatalf("valid trailing frame was not recovered: %#v", frames)
	}
}

func makeStreamFrame(payload []byte) []byte {
	frame := append([]byte{'L', 'S', 1, 0, 0, 0, 0, 0}, payload...)
	binary.LittleEndian.PutUint32(frame[4:8], uint32(len(payload)))
	trailer := make([]byte, 4)
	binary.LittleEndian.PutUint32(trailer, crc32.ChecksumIEEE(payload))
	return append(frame, trailer...)
}
