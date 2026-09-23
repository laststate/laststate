// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package source

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
)

func TestEncodeBLEPayload(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5}
	buf := encodeBLEPayload(data, "AA:BB:CC:DD:EE:FF")
	if len(buf) != 8+5 {
		t.Fatalf("len = %d", len(buf))
	}
	if !bytes.Equal(buf[:6], []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}) {
		t.Fatalf("address = %v", buf[:6])
	}
	if !bytes.Equal(buf[8:], data) {
		t.Fatalf("data = %v", buf[8:])
	}
}

func TestHexDecodeToleratesSeparators(t *testing.T) {
	got := hexDecode("AA:BB:CC:DD:EE:FF")
	want := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if !bytes.Equal(hexDecode("aabbccddeeff"), want) {
		t.Fatal("lowercase hex mismatch")
	}
	if !bytes.Equal(hexDecode("AA-BB-CC-DD-EE-FF"), want) {
		t.Fatal("dash separators mismatch")
	}
}

func TestValidBLEAddress(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"AA:BB:CC:DD:EE:FF", true},
		{"aabbccddeeff", true},
		{"AA-BB-CC-DD-EE-FF", true},
		{"", false},
		{"GG:HH:II:JJ:KK:LL", false},
		{"AABBCC", false},
	}
	for _, c := range cases {
		if got := ValidBLEAddress(c.in); got != c.want {
			t.Fatalf("ValidBLEAddress(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestBLEDeviceSimulator(t *testing.T) {
	sim := NewBLEDeviceSimulator()
	dev := &BLEDevice{Address: "AA:BB:CC:DD:EE:01", Name: "latch-1"}
	sim.AddDevice(dev)
	if got := sim.SimulateScan(); len(got) != 1 || got[0].Address != dev.Address {
		t.Fatalf("scan = %+v", got)
	}
}

func TestScanFiltersRSSIAndAddress(t *testing.T) {
	// Start a fake gateway that emits two found events.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		line, _ := reader.ReadString('\n')
		if !strings.Contains(line, `"cmd":"scan"`) {
			return
		}
		fmt.Fprintf(conn, `{"event":"found","address":"AA:BB:CC:DD:EE:01","name":"low","rssi":-90}`+"\n")
		fmt.Fprintf(conn, `{"event":"found","address":"AA:BB:CC:DD:EE:02","name":"ok","rssi":-60}`+"\n")
		fmt.Fprintf(conn, `{"event":"found","address":"AA:BB:CC:DD:EE:03","name":"other","rssi":-50}`+"\n")
		fmt.Fprintf(conn, `{"event":"scan_done"}`+"\n")
	}()

	scanner := NewBLEScanner(BLEConfig{
		Adapter:         ln.Addr().String(),
		RSSIThreshold:   -70,
		FilterByAddress: []string{"AA:BB:CC:DD:EE:02"},
		MaxDevices:      10,
	}, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	devices, err := scanner.scan(ctx)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("got %d devices, want 1: %+v", len(devices), devices)
	}
	if devices[0].Address != "AA:BB:CC:DD:EE:02" {
		t.Fatalf("address = %s", devices[0].Address)
	}
	if devices[0].RSSI != -60 {
		t.Fatalf("rssi = %d", devices[0].RSSI)
	}
}

func TestReadDeviceNotification(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		for i := 0; i < 2; i++ {
			if _, err := reader.ReadString('\n'); err != nil {
				return
			}
		}
		// connect then read/notify -> emit two notifications and disconnect
		fmt.Fprintf(conn, `{"event":"notify","address":"AA:BB:CC:DD:EE:01","uuid":"fff1","data":"%s"}`+"\n", hex.EncodeToString([]byte{0xDE, 0xAD}))
		fmt.Fprintf(conn, `{"event":"notify","address":"AA:BB:CC:DD:EE:01","uuid":"fff1","data":"beef"}`+"\n")
		fmt.Fprintf(conn, `{"event":"disconnect"}`+"\n")
	}()

	scanner := NewBLEScanner(BLEConfig{
		Adapter:       ln.Addr().String(),
		CharUUID:      "fff1",
		NotifyEnabled: true,
	}, slog.Default())

	dev := &BLEDevice{Address: "AA:BB:CC:DD:EE:01", Name: "latch-1"}
	var got [][]byte
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// readDevice blocks until disconnect; collect notifications.
	done := make(chan struct{})
	go func() {
		err := scanner.readDevice(ctx, dev, func(_ *BLEDevice, _ *BLECharacteristic, payload []byte) error {
			got = append(got, payload)
			return nil
		})
		if err != nil {
			t.Errorf("readDevice: %v", err)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("readDevice hung")
	}

	if len(got) != 2 {
		t.Fatalf("got %d notifications, want 2", len(got))
	}
	if !bytes.Equal(got[0][8:], []byte{0xDE, 0xAD}) {
		t.Fatalf("payload[0] = %v", got[0])
	}
	if !bytes.Equal(got[1][8:], []byte{0xBE, 0xEF}) {
		t.Fatalf("payload[1] = %v", got[1])
	}
}

func TestRunRequiresGatewayAddress(t *testing.T) {
	scanner := NewBLEScanner(BLEConfig{}, slog.Default())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// Without a gateway address Run must terminate (via deadline or error)
	// instead of hanging forever.
	err := scanner.Run(ctx, func(*BLEDevice, *BLECharacteristic, []byte) error { return nil })
	if err == nil {
		t.Fatal("want non-nil error, got nil")
	}
}
