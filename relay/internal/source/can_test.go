// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package source

import (
	"bytes"
	"context"
	"encoding/binary"
	"strings"
	"testing"
)

func TestParseCANFramesLEP(t *testing.T) {
	// [id:4][flags:1][len:1][data:N]
	payload := []byte("frame")
	buf := make([]byte, 6+len(payload))
	binary.LittleEndian.PutUint32(buf[:4], 0x123)
	buf[4] = 0x00
	buf[5] = byte(len(payload))
	copy(buf[6:], payload)

	frames := parseCANFrames(buf, CANConfig{Protocol: "LEP"})
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	if frames[0].ID != 0x123 {
		t.Fatalf("id = %#x", frames[0].ID)
	}
	if !bytes.Equal(frames[0].Data, payload) {
		t.Fatalf("data = %q", frames[0].Data)
	}
}

func TestParseCANFramesLEPFlags(t *testing.T) {
	buf := make([]byte, 6+1)
	binary.LittleEndian.PutUint32(buf[:4], 0x456)
	buf[4] = 0xE0 // FD + BRS + IDE
	buf[5] = 1
	buf[6] = 0xAA

	frames := parseCANFrames(buf, CANConfig{Protocol: "LEP"})
	if len(frames) != 1 {
		t.Fatalf("got %d frames", len(frames))
	}
	f := frames[0]
	if !f.IsFD || !f.BRS || !f.IDE {
		t.Fatalf("flags not decoded: %+v", f)
	}
}

func TestParseCANFramesRawFilter(t *testing.T) {
	// Raw format: [id:4][dlc:1][data:dlc]
	build := func(id uint32, data ...byte) []byte {
		buf := make([]byte, 5+len(data))
		binary.LittleEndian.PutUint32(buf[:4], id)
		buf[4] = byte(len(data))
		copy(buf[5:], data)
		return buf
	}
	in := append(build(0x100), build(0x300)...)
	frames := parseCANFrames(in, CANConfig{Protocol: "RAW", FilterID: 0x100, FilterMask: 0x7FF})
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1 (filtered)", len(frames))
	}
	if frames[0].ID != 0x100 {
		t.Fatalf("id = %#x", frames[0].ID)
	}
}

func TestEncodeCANFrame(t *testing.T) {
	frame := CANFrame{ID: 0x7FF, DLC: 4, Data: []byte{1, 2, 3, 4}}
	lep := encodeCANFrame(frame, "LEP")
	if len(lep) != 6+4 {
		t.Fatalf("LEP len = %d", len(lep))
	}
	if lep[4] != 4 || lep[5] != 4 {
		t.Fatalf("LEP header = %v", lep[4:6])
	}
	raw := encodeCANFrame(frame, "RAW")
	if !bytes.Equal(raw, frame.Data) {
		t.Fatalf("RAW = %v", raw)
	}
	js := encodeCANFrame(frame, "JSON")
	if !bytes.Contains(js, []byte(`"id"`)) || !bytes.Contains(js, []byte(`"data"`)) {
		t.Fatalf("JSON malformed: %s", js)
	}
}

func TestSanitizeCANInterface(t *testing.T) {
	if got := SanitizeCANInterface("can0"); got != "0" {
		t.Fatalf("got %q", got)
	}
	if got := SanitizeCANInterface("vcan1"); got != "1" {
		t.Fatalf("got %q", got)
	}
}

func TestRunCANRequiresSource(t *testing.T) {
	err := RunCAN(context.Background(), CANConfig{}, func(CANFrame, []byte) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("want config error, got %v", err)
	}
}

func TestCANDeviceSimulator(t *testing.T) {
	dev := NewCANDevice("can0", 500000)
	if !dev.Available() || dev.Name() != "can0" {
		t.Fatal("device not ready")
	}
	dev.InjectFrame(CANFrame{ID: 0x123, Data: []byte("hi")})
	dev.InjectFrames([]CANFrame{{ID: 0x456}, {ID: 0x789}})
	if len(dev.Frames) != 3 {
		t.Fatalf("frames = %d, want 3", len(dev.Frames))
	}
}

func TestDecodeCANFrameLinuxClassic(t *testing.T) {
	// struct can_frame: id(4) | dlc/flags(1) | pad(3) | data(8) = 16 bytes
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint32(buf[:4], 0x123)
	buf[4] = 4
	copy(buf[8:], []byte{9, 8, 7, 6})
	frame := decodeCANFrameLinux(buf)
	if frame.ID != 0x123 || frame.IsFD || frame.DLC != 4 {
		t.Fatalf("classic frame = %+v", frame)
	}
	if !bytes.Equal(frame.Data, []byte{9, 8, 7, 6}) {
		t.Fatalf("data = %v", frame.Data)
	}
}

func TestDecodeCANFrameLinuxFD(t *testing.T) {
	// struct canfd_frame: id(4) | len(1) | flags(1) | res(2) | data(64) = 72
	buf := make([]byte, 72)
	binary.LittleEndian.PutUint32(buf[:4], 0x456)
	buf[4] = 8
	buf[5] = 0x02 // BRS flag
	copy(buf[8:16], []byte{1, 2, 3, 4, 5, 6, 7, 8})
	frame := decodeCANFrameLinux(buf)
	if !frame.IsFD || !frame.BRS || frame.DLC != 8 {
		t.Fatalf("fd frame = %+v", frame)
	}
	if !bytes.Equal(frame.Data, []byte{1, 2, 3, 4, 5, 6, 7, 8}) {
		t.Fatalf("data = %v", frame.Data)
	}
}
