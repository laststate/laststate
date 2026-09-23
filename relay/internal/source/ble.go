// Package source implements BLE GATT collector for the relay.
//
// BLE sources discover, connect to, and read characteristics from BLE
// peripherals (typically Latch-enabled MCUs) and forward collected data
// as LEP payloads through the relay's ingest pipeline.
//
// Native GATT scanning requires a platform stack (BlueZ on Linux, CoreBluetooth
// on macOS, WinRT on Windows). Instead of binding to any one stack, the relay
// talks to a BLE gateway over a JSON-over-TCP link. A companion device (e.g. an
// ESP32 or a host running bluez) runs the gateway and relays scan results and
// GATT notifications, so the same binary works everywhere.
//
// Configuration:
//
//	BLE_ADAPTER=127.0.0.1:9000       # gateway host:port
//	BLE_SCAN_DURATION=30s
//	BLE_SERVICE_UUID=0000fff0-0000-1000-8000-00805f9b34fb
//	BLE_CHAR_UUID=0000fff1-0000-1000-8000-00805f9b34fb
//	BLE_NOTIFY_ENABLED=true
//	BLE_RSSI_THRESHOLD=-80
//	BLE_MAX_DEVICES=50
package source

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// BLEConfig describes a BLE GATT source.
type BLEConfig struct {
	Adapter         string
	ScanDuration    time.Duration
	ServiceUUID     string
	CharUUID        string
	NotifyEnabled   bool
	RSSIThreshold   int
	MaxDevices      int
	FilterByAddress []string
}

// BLEDevice represents a discovered BLE peripheral.
type BLEDevice struct {
	Address     string
	Name        string
	RSSI        int
	ServiceUUID string
	CharUUID    string
	Connected   bool
}

// BLECharacteristic represents a GATT characteristic.
type BLECharacteristic struct {
	UUID       string
	Value      []byte
	Properties []string
	Notify     bool
}

// BLEHandler is invoked for each BLE notification or read result.
type BLEHandler func(device *BLEDevice, char *BLECharacteristic, payload []byte) error

// BLEScanner discovers BLE devices and manages connections.
type BLEScanner struct {
	config  BLEConfig
	devices []*BLEDevice
	mu      sync.RWMutex
	log     *slog.Logger
	running bool
	cancel  context.CancelFunc
	connMu  sync.Mutex
	conn    net.Conn
}

// NewBLEScanner creates a new BLE scanner.
func NewBLEScanner(cfg BLEConfig, log *slog.Logger) *BLEScanner {
	if cfg.ScanDuration == 0 {
		cfg.ScanDuration = 30 * time.Second
	}
	if cfg.RSSIThreshold == 0 {
		cfg.RSSIThreshold = -80
	}
	if cfg.MaxDevices == 0 {
		cfg.MaxDevices = 50
	}
	if log == nil {
		log = slog.Default()
	}
	return &BLEScanner{
		config:  cfg,
		devices: make([]*BLEDevice, 0, cfg.MaxDevices),
		log:     log,
	}
}

// Run starts BLE scanning and notification collection.
func (s *BLEScanner) Run(ctx context.Context, handle BLEHandler) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("BLE scanner already running")
	}
	s.running = true
	s.mu.Unlock()

	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	defer func() {
		cancel()
		s.shutdown()
	}()

	log := s.log
	log.Info("BLE scanner starting",
		"adapter", s.config.Adapter,
		"scan_duration", s.config.ScanDuration,
		"service_uuid", s.config.ServiceUUID,
		"max_devices", s.config.MaxDevices)

	// Scan loop
	for {
		select {
		case <-runCtx.Done():
			return runCtx.Err()
		default:
		}

		devices, err := s.scan(runCtx)
		if err != nil {
			log.Warn("BLE scan error, retrying", "error", err)
			select {
			case <-runCtx.Done():
				return runCtx.Err()
			case <-time.After(5 * time.Second):
				continue
			}
		}

		s.mu.Lock()
		s.devices = devices
		s.mu.Unlock()

		for _, dev := range devices {
			dev := dev
			go func() {
				if err := s.readDevice(runCtx, dev, handle); err != nil {
					log.Warn("BLE read error", "address", dev.Address, "error", err)
				}
			}()
		}

		select {
		case <-runCtx.Done():
			return runCtx.Err()
		case <-time.After(s.config.ScanDuration):
		}
	}
}

// gatewayMsg is a JSON message exchanged with the BLE gateway.
type gatewayMsg struct {
	Cmd     string `json:"cmd,omitempty"`
	Address string `json:"address,omitempty"`
	UUID    string `json:"uuid,omitempty"`
	Event   string `json:"event,omitempty"`
	Name    string `json:"name,omitempty"`
	RSSI    int    `json:"rssi,omitempty"`
	Data    string `json:"data,omitempty"` // hex
}

// gatewayConn opens a JSON line connection to the BLE gateway.
func (s *BLEScanner) gatewayConn(ctx context.Context) (net.Conn, *bufio.Reader, error) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if s.conn != nil {
		return s.conn, bufio.NewReader(s.conn), nil
	}
	if s.config.Adapter == "" {
		return nil, nil, fmt.Errorf("ble: gateway address is required")
	}
	host := s.config.Adapter
	if !strings.Contains(host, ":") {
		host = net.JoinHostPort(host, "9000")
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, nil, fmt.Errorf("ble: gateway connect %s: %w", host, err)
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	s.conn = conn
	return conn, bufio.NewReader(conn), nil
}

// scan performs a BLE device scan through the gateway and returns discovered
// devices filtered by RSSI threshold, address allow-list, and max devices.
func (s *BLEScanner) scan(ctx context.Context) ([]*BLEDevice, error) {
	conn, reader, err := s.gatewayConn(ctx)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := fmt.Fprintf(conn, `{"cmd":"scan"}`+"\n"); err != nil {
		return nil, fmt.Errorf("ble: gateway scan: %w", err)
	}

	var out []*BLEDevice
	seen := make(map[string]bool)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if len(out) > 0 {
				return out, nil
			}
			return nil, fmt.Errorf("ble: gateway scan read: %w", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == `{"event":"scan_done"}` || line == `{"event":"done"}` {
			return out, nil
		}
		var msg gatewayMsg
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if msg.Event != "found" || msg.Address == "" {
			continue
		}
		dev := &BLEDevice{
			Address:     msg.Address,
			Name:        msg.Name,
			RSSI:        msg.RSSI,
			ServiceUUID: s.config.ServiceUUID,
			CharUUID:    s.config.CharUUID,
		}
		if s.config.RSSIThreshold != 0 && dev.RSSI < s.config.RSSIThreshold {
			continue
		}
		if len(s.config.FilterByAddress) > 0 && !containsString(s.config.FilterByAddress, dev.Address) {
			continue
		}
		key := strings.ToLower(dev.Address)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, dev)
		if len(out) >= s.config.MaxDevices {
			return out, nil
		}
	}
}

// readDevice connects to a BLE device through the gateway, enables
// notifications on the configured characteristic, and forwards each value.
func (s *BLEScanner) readDevice(ctx context.Context, dev *BLEDevice, handle BLEHandler) error {
	conn, reader, err := s.gatewayConn(ctx)
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	if _, err := fmt.Fprintf(conn, `{"cmd":"connect","address":%q}`+"\n", dev.Address); err != nil {
		return fmt.Errorf("ble: gateway connect device: %w", err)
	}
	if s.config.CharUUID != "" {
		mode := "read"
		if s.config.NotifyEnabled {
			mode = "notify"
		}
		if _, err := fmt.Fprintf(conn, `{"cmd":%q,"address":%q,"uuid":%q}`+"\n", mode, dev.Address, s.config.CharUUID); err != nil {
			return fmt.Errorf("ble: gateway %s: %w", mode, err)
		}
	}

	s.log.Info("BLE reading device", "address", dev.Address, "name", dev.Name)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("ble: gateway read: %w", err)
		}
		var msg gatewayMsg
		if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &msg); err != nil {
			continue
		}
		if msg.Address != "" && !strings.EqualFold(msg.Address, dev.Address) {
			continue
		}
		switch msg.Event {
		case "notify", "read":
			data, err := hex.DecodeString(msg.Data)
			if err != nil {
				continue
			}
			char := &BLECharacteristic{
				UUID:   msg.UUID,
				Value:  data,
				Notify: msg.Event == "notify",
			}
			payload := encodeBLEPayload(data, dev.Address)
			if err := handle(dev, char, payload); err != nil {
				s.log.Warn("BLE handler error", "address", dev.Address, "error", err)
			}
		case "disconnect":
			return nil
		}
	}
}

// encodeBLEPayload encodes BLE data into LEP-compatible format.
// [address:6][data_length:2][data:N]
func encodeBLEPayload(data []byte, address string) []byte {
	buf := make([]byte, 8+len(data))
	copy(buf[:6], hexDecode(address))
	buf[6] = byte(len(data) >> 8)
	buf[7] = byte(len(data))
	copy(buf[8:], data)
	return buf
}

// hexDecode decodes a hex address string to bytes. It tolerates ':' and '-'
// separators and uppercase hex.
func hexDecode(s string) []byte {
	clean := strings.NewReplacer(":", "", "-", "").Replace(s)
	b := make([]byte, 0, len(clean)/2)
	for i := 0; i+1 < len(clean); i += 2 {
		v := byte(0)
		for _, c := range clean[i : i+2] {
			v <<= 4
			switch {
			case c >= '0' && c <= '9':
				v |= byte(c - '0')
			case c >= 'a' && c <= 'f':
				v |= byte(c - 'a' + 10)
			case c >= 'A' && c <= 'F':
				v |= byte(c - 'A' + 10)
			}
		}
		b = append(b, v)
	}
	return b
}

// GetDevices returns the currently discovered BLE devices.
func (s *BLEScanner) GetDevices() []*BLEDevice {
	s.mu.RLock()
	defer s.mu.RUnlock()
	devices := make([]*BLEDevice, len(s.devices))
	copy(devices, s.devices)
	return devices
}

// shutdown stops the BLE scanner and closes any open gateway connection.
func (s *BLEScanner) shutdown() {
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
	s.connMu.Lock()
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
	s.connMu.Unlock()
}

// ContainsString reports whether s is present in list (case-insensitive).
func containsString(list []string, s string) bool {
	for _, item := range list {
		if strings.EqualFold(item, s) {
			return true
		}
	}
	return false
}

// BLEDeviceSimulator simulates BLE devices for testing.
type BLEDeviceSimulator struct {
	Devices []*BLEDevice
	mu      sync.RWMutex
}

// NewBLEDeviceSimulator creates a BLE simulator.
func NewBLEDeviceSimulator() *BLEDeviceSimulator {
	return &BLEDeviceSimulator{
		Devices: make([]*BLEDevice, 0),
	}
}

// AddDevice adds a simulated BLE device.
func (s *BLEDeviceSimulator) AddDevice(dev *BLEDevice) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Devices = append(s.Devices, dev)
}

// SimulateScan returns simulated scan results.
func (s *BLEDeviceSimulator) SimulateScan() []*BLEDevice {
	s.mu.RLock()
	defer s.mu.RUnlock()
	devices := make([]*BLEDevice, len(s.Devices))
	copy(devices, s.Devices)
	return devices
}

// ValidBLEAddress reports whether addr looks like a 6-byte EUI-48.
func ValidBLEAddress(addr string) bool {
	clean := strings.NewReplacer(":", "", "-", "").Replace(addr)
	_, err := strconv.ParseUint(clean, 16, 64)
	return err == nil && len(clean) == 12
}
