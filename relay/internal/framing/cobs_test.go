// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package framing

import "testing"

func TestCOBSAcrossWrites(t *testing.T) {
	framer := NewCOBS(32)
	frames, err := framer.Push([]byte{3, 1})
	if err != nil || len(frames) != 0 {
		t.Fatal(err)
	}
	frames, err = framer.Push([]byte{2, 2, 3, 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || string(frames[0]) != string([]byte{1, 2, 0, 3}) {
		t.Fatalf("frames: %v", frames)
	}
}

func TestCOBSRecoversWithinSameRead(t *testing.T) {
	framer := NewCOBS(64)
	bad := []byte{5, 1, 2, 0} // code says four bytes follow, but only two do.
	goodPayload := []byte{7, 0, 8}
	stream := append(bad, Encode(goodPayload)...)
	frames, err := framer.Push(stream)
	if err == nil {
		t.Fatal("expected malformed COBS frame to be reported")
	}
	if len(frames) != 1 || string(frames[0]) != string(goodPayload) {
		t.Fatalf("valid trailing frame was not recovered: %#v", frames)
	}
}

func TestCOBSEncodeDecodeRoundTrip(t *testing.T) {
	payload := []byte{0, 1, 2, 0, 3, 4, 0}
	encoded := Encode(payload)
	decoded, err := Decode(encoded[:len(encoded)-1])
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != string(payload) {
		t.Fatalf("decoded %x, want %x", decoded, payload)
	}
}
