// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package source

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLoRaWANUplinkJSON(t *testing.T) {
	in := `{"device_eui":"0000000000000001","port":2,"f_cnt":42,"data":"aGVsbG8=","rssi":-87,"snr":9.5}`
	uplink, err := parseLoRaWANUplink([]byte(in), LoRaWANConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uplink.DeviceEUI != "0000000000000001" {
		t.Fatalf("device_eui = %q", uplink.DeviceEUI)
	}
	if uplink.Port != 2 {
		t.Fatalf("port = %d", uplink.Port)
	}
	if uplink.FCnt != 42 {
		t.Fatalf("f_cnt = %d", uplink.FCnt)
	}
	if string(uplink.Data) != "hello" {
		t.Fatalf("data = %q", uplink.Data)
	}
	if uplink.RSSI != -87 {
		t.Fatalf("rssi = %d", uplink.RSSI)
	}
}

func TestParseLoRaWANUplinkMissingEUI(t *testing.T) {
	_, err := parseLoRaWANUplink([]byte(`{"port":1}`), LoRaWANConfig{})
	if err == nil || !strings.Contains(err.Error(), "device_eui") {
		t.Fatalf("want device_eui error, got %v", err)
	}
}

func TestParseLoRaWANUplinkEUIFilter(t *testing.T) {
	_, err := parseLoRaWANUplink([]byte(`{"device_eui":"aaaa"}`), LoRaWANConfig{DeviceEUI: "bbbb"})
	if err == nil || !strings.Contains(err.Error(), "filter mismatch") {
		t.Fatalf("want filter mismatch, got %v", err)
	}
}

func TestParseLoRaWANUplinkBinary(t *testing.T) {
	// [dev_eui:8][dev_addr:4][port:1][fcnt:4][data:N]
	buf := make([]byte, 17+5)
	for i := 0; i < 8; i++ {
		buf[i] = byte(i + 1)
	}
	copy(buf[13:17], []byte{0, 0, 0, 7})
	copy(buf[17:], []byte("hello"))
	uplink, err := parseLoRaWANUplink(buf, LoRaWANConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uplink.DeviceEUI != "0102030405060708" {
		t.Fatalf("device_eui = %q", uplink.DeviceEUI)
	}
	if uplink.Port != 0 {
		t.Fatalf("port = %d", uplink.Port)
	}
	if uplink.FCnt != 7 {
		t.Fatalf("f_cnt = %d", uplink.FCnt)
	}
	if !bytes.Equal(uplink.Data, []byte("hello")) {
		t.Fatalf("data = %q", uplink.Data)
	}
}

func TestParseLoRaWANUplinkBinaryTooShort(t *testing.T) {
	if _, err := parseLoRaWANUplink(make([]byte, 16), LoRaWANConfig{}); err == nil {
		t.Fatal("want error for short binary frame")
	}
}

func TestEncodeLoRaWANPayload(t *testing.T) {
	uplink := LoRaWANUplink{
		DeviceEUI: "0000000000000001",
		Port:      5,
		FCnt:      0x01020304,
		RSSI:      -80,
		SNR:       8.5,
		Data:      []byte("payload"),
	}
	buf := encodeLoRaWANPayload(uplink)
	if len(buf) != 25+7 {
		t.Fatalf("len = %d, want 32", len(buf))
	}
	if string(buf[25:]) != "payload" {
		t.Fatalf("payload not preserved: %q", buf[25:])
	}
	if buf[16] != 5 {
		t.Fatalf("port byte = %d", buf[16])
	}
}

func TestLoRaWANServerSimulator(t *testing.T) {
	log := slog.Default()
	sim := NewLoRaWANServerSimulator(log)
	up := LoRaWANUplink{DeviceEUI: "0000000000000001", Port: 1, Data: []byte("x")}
	sim.SimulateUplink(up)
	if got := sim.Devices["0000000000000001"]; got == nil {
		t.Fatal("uplink not stored")
	} else if got.Port != 1 {
		t.Fatalf("port = %d", got.Port)
	}

	raw, _ := json.Marshal(LoRaWANUplink{DeviceEUI: "0000000000000002", Port: 2})
	if err := sim.SimulateUplinkJSON(raw); err != nil {
		t.Fatalf("SimulateUplinkJSON: %v", err)
	}
	if sim.Devices["0000000000000002"] == nil {
		t.Fatal("JSON uplink not stored")
	}
}

func TestLoRaWANUnsupportedProtocol(t *testing.T) {
	client := NewLoRaWANClient(LoRaWANConfig{Protocol: "WEIRD"}, slog.Default())
	err := client.Run(context.Background(), func(LoRaWANUplink, []byte) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "unsupported protocol") {
		t.Fatalf("want unsupported protocol, got %v", err)
	}
}
