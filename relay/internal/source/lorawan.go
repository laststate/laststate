// Package source implements LoRa/LoRaWAN collector for the relay.
//
// LoRa sources connect to a LoRaWAN network server (e.g., The Things Network,
// ChirpStack, or a custom LoRaWAN server) and forward device uplinks as LEP
// payloads through the relay's ingest pipeline.
//
// Configuration:
//
//	LORAWAN_SERVER=ttn.example.com:1883
//	LORAWAN_API_KEY=your-api-key
//	LORAWAN_NETWORK_ID=0000000000000001
//	LORAWAN_DEVICE_EUI=0000000000000001
//	LORAWAN_PORT=1
//	LORAWAN_PROTOCOL=MQTT|HTTP
//	LORAWAN_TLS_ENABLED=true
package source

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// LoRaWANConfig describes a LoRaWAN source.
type LoRaWANConfig struct {
	Server     string
	APIKey     string
	NetworkID  string
	DeviceEUI  string
	Port       uint8
	Protocol   string // "MQTT", "HTTP", "GRPC"
	TLSEnabled bool
	MaxRetries int
}

// LoRaWANUplink represents a LoRaWAN device uplink message.
type LoRaWANUplink struct {
	DeviceEUI  string    `json:"device_eui"`
	DeviceAddr string    `json:"device_addr"`
	Port       uint8     `json:"port"`
	FCnt       uint32    `json:"f_cnt"`
	Data       []byte    `json:"data"`
	RSSI       int       `json:"rssi"`
	SNR        float64   `json:"snr"`
	Timestamp  time.Time `json:"timestamp"`
	Location   *Location `json:"location,omitempty"`
	Confirmed  bool      `json:"confirmed"`
}

// Location holds GPS coordinates for a LoRaWAN device.
type Location struct {
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lng"`
	Altitude  float64 `json:"alt"`
}

// LoRaWANHandler is invoked for each LoRaWAN uplink.
type LoRaWANHandler func(uplink LoRaWANUplink, payload []byte) error

// LoRaWANClient connects to a LoRaWAN network server and forwards uplinks.
type LoRaWANClient struct {
	config  LoRaWANConfig
	log     *slog.Logger
	mu      sync.RWMutex
	running bool
}

// NewLoRaWANClient creates a new LoRaWAN client.
func NewLoRaWANClient(cfg LoRaWANConfig, log *slog.Logger) *LoRaWANClient {
	if cfg.Protocol == "" {
		cfg.Protocol = "MQTT"
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if log == nil {
		log = slog.Default()
	}
	return &LoRaWANClient{
		config: cfg,
		log:    log,
	}
}

// Run starts the LoRaWAN client and blocks until ctx is cancelled.
func (c *LoRaWANClient) Run(ctx context.Context, handle LoRaWANHandler) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("lorawan: client already running")
	}
	c.running = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
	}()

	log := c.log
	log.Info("LoRaWAN client starting",
		"server", c.config.Server,
		"protocol", c.config.Protocol,
		"device_eui", c.config.DeviceEUI)

	switch strings.ToUpper(c.config.Protocol) {
	case "MQTT":
		return c.runMQTT(ctx, handle)
	case "HTTP":
		return c.runHTTP(ctx, handle)
	case "GRPC":
		return c.runGRPC(ctx, handle)
	default:
		return fmt.Errorf("lorawan: unsupported protocol %q", c.config.Protocol)
	}
}

// runMQTT subscribes to uplink topics on an MQTT network server (TTN,
// ChirpStack, and compatible brokers). Each payload is decoded as a
// LoRaWANUplink and handed to handle.
func (c *LoRaWANClient) runMQTT(ctx context.Context, handle LoRaWANHandler) error {
	if c.config.Server == "" {
		return fmt.Errorf("lorawan: MQTT server is required")
	}
	opts := mqtt.NewClientOptions()
	if !strings.Contains(c.config.Server, "://") {
		opts.AddBroker("tcp://" + c.config.Server)
	} else {
		opts.AddBroker(c.config.Server)
	}
	opts.SetClientID("laststate-relay")
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetOrderMatters(false)
	if c.config.APIKey != "" {
		opts.SetUsername(c.config.NetworkID)
		opts.SetPassword(c.config.APIKey)
	}
	if c.config.TLSEnabled {
		opts.SetTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12})
	}

	// Uplink topic for both TTN v3 and ChirpStack v4 conventions. When a
	// device EUI filter is set, subscribe to that specific device only;
	// otherwise subscribe to all devices under the network.
	networkID := c.config.NetworkID
	if networkID == "" {
		networkID = "+"
	}
	var device string
	if c.config.DeviceEUI != "" {
		device = c.config.DeviceEUI
	} else {
		device = "+"
	}
	topics := []string{
		fmt.Sprintf("v3/%s/devices/%s/up", networkID, device),
		fmt.Sprintf("application/%s/device/%s/event/up", networkID, device),
	}
	opts.SetDefaultPublishHandler(func(_ mqtt.Client, msg mqtt.Message) {
		uplink, err := parseLoRaWANUplink(msg.Payload(), c.config)
		if err != nil {
			c.log.Warn("LoRaWAN uplink parse error", "error", err)
			return
		}
		payload := encodeLoRaWANPayload(*uplink)
		if err := handle(*uplink, payload); err != nil {
			c.log.Warn("LoRaWAN handler error", "error", err)
		}
	})

	client := mqtt.NewClient(opts)
	token := client.Connect()
	if !token.WaitTimeout(30 * time.Second) {
		return fmt.Errorf("lorawan: MQTT connect timeout for %s", sanitizeBroker(c.config.Server))
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("lorawan: MQTT connect: %w", err)
	}
	defer client.Disconnect(250)

	for _, topic := range topics {
		token := client.Subscribe(topic, 0, func(_ mqtt.Client, msg mqtt.Message) {
			uplink, err := parseLoRaWANUplink(msg.Payload(), c.config)
			if err != nil {
				c.log.Warn("LoRaWAN uplink parse error", "error", err)
				return
			}
			payload := encodeLoRaWANPayload(*uplink)
			if err := handle(*uplink, payload); err != nil {
				c.log.Warn("LoRaWAN handler error", "error", err)
			}
		})
		if !token.WaitTimeout(15 * time.Second) {
			return fmt.Errorf("lorawan: MQTT subscribe timeout for %s", topic)
		}
		if err := token.Error(); err != nil {
			return fmt.Errorf("lorawan: MQTT subscribe %s: %w", topic, err)
		}
		c.log.Info("LoRaWAN subscribed", "topic", topic)
	}
	<-ctx.Done()
	return nil
}

// runHTTP polls a JSON uplink feed (e.g., ChirpStack REST) until ctx is
// cancelled. The endpoint is expected to return a JSON array of
// LoRaWANUplink objects.
func (c *LoRaWANClient) runHTTP(ctx context.Context, handle LoRaWANHandler) error {
	if c.config.Server == "" {
		return fmt.Errorf("lorawan: HTTP server is required")
	}
	base := c.config.Server
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	client := &http.Client{Timeout: 20 * time.Second}
	endpoint := strings.TrimSuffix(base, "/") + "/api/v3/events/up"
	if c.config.APIKey != "" {
		endpoint += "?authorization=" + url.QueryEscape(c.config.APIKey)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		if c.config.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			c.log.Warn("LoRaWAN HTTP poll error", "error", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				continue
			}
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if err != nil {
			c.log.Warn("LoRaWAN HTTP read error", "error", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				continue
			}
		}
		if resp.StatusCode != http.StatusOK {
			c.log.Warn("LoRaWAN HTTP status", "status", resp.StatusCode)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Second):
				continue
			}
		}
		var uplinks []LoRaWANUplink
		if err := json.Unmarshal(body, &uplinks); err != nil {
			c.log.Warn("LoRaWAN HTTP decode error", "error", err)
		}
		for _, uplink := range uplinks {
			payload := encodeLoRaWANPayload(uplink)
			if err := handle(uplink, payload); err != nil {
				c.log.Warn("LoRaWAN handler error", "error", err)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// parseLoRaWANUplink parses a raw JSON uplink envelope into an uplink. For
// MQTT the payload is already the JSON object; for the legacy binary format a
// 17-byte header is accepted for backward compatibility.
func parseLoRaWANUplink(data []byte, cfg LoRaWANConfig) (*LoRaWANUplink, error) {
	if len(data) >= 2 && data[0] == '{' {
		var uplink LoRaWANUplink
		if err := json.Unmarshal(data, &uplink); err != nil {
			return nil, fmt.Errorf("lorawan: decode uplink: %w", err)
		}
		if uplink.DeviceEUI == "" {
			return nil, fmt.Errorf("lorawan: uplink missing device_eui")
		}
		if cfg.DeviceEUI != "" && !strings.EqualFold(uplink.DeviceEUI, cfg.DeviceEUI) {
			return nil, fmt.Errorf("lorawan: device EUI filter mismatch")
		}
		if uplink.Timestamp.IsZero() {
			uplink.Timestamp = time.Now()
		}
		return &uplink, nil
	}

	// Legacy binary format: [dev_eui:8][dev_addr:4][port:1][fcnt:4][data:N]
	if len(data) < 17 {
		return nil, fmt.Errorf("lorawan: data too short")
	}
	uplink := &LoRaWANUplink{
		DeviceEUI:  fmt.Sprintf("%x", data[:8]),
		DeviceAddr: fmt.Sprintf("%x", data[8:12]),
		Port:       data[12],
		FCnt:       uint32(data[13])<<24 | uint32(data[14])<<16 | uint32(data[15])<<8 | uint32(data[16]),
		Data:       data[17:],
		Timestamp:  time.Now(),
	}
	if cfg.DeviceEUI != "" && uplink.DeviceEUI != cfg.DeviceEUI {
		return nil, fmt.Errorf("lorawan: device EUI filter mismatch")
	}
	return uplink, nil
}

// encodeLoRaWANPayload encodes a LoRaWAN uplink into LEP-compatible format.
// [dev_eui:16][port:1][fcnt:4][rssi:2][snr:2][data:N]
func encodeLoRaWANPayload(uplink LoRaWANUplink) []byte {
	buf := make([]byte, 25+len(uplink.Data))
	copy(buf[:16], []byte(fmt.Sprintf("%016s", uplink.DeviceEUI)))
	buf[16] = uplink.Port
	buf[17] = byte(uplink.FCnt >> 24)
	buf[18] = byte(uplink.FCnt >> 16)
	buf[19] = byte(uplink.FCnt >> 8)
	buf[20] = byte(uplink.FCnt)
	buf[21] = byte(uplink.RSSI >> 8)
	buf[22] = byte(uplink.RSSI)
	buf[23] = byte(uplink.SNR)
	buf[24] = byte(uplink.SNR * 256) // fixed point
	copy(buf[25:], uplink.Data)
	return buf
}

// LoRaWANServerSimulator simulates a LoRaWAN network server for testing.
type LoRaWANServerSimulator struct {
	Devices map[string]*LoRaWANUplink
	mu      sync.RWMutex
	log     *slog.Logger
}

// NewLoRaWANServerSimulator creates a LoRaWAN server simulator.
func NewLoRaWANServerSimulator(log *slog.Logger) *LoRaWANServerSimulator {
	if log == nil {
		log = slog.Default()
	}
	return &LoRaWANServerSimulator{
		Devices: make(map[string]*LoRaWANUplink),
		log:     log,
	}
}

// SimulateUplink injects a simulated uplink message.
func (s *LoRaWANServerSimulator) SimulateUplink(uplink LoRaWANUplink) {
	s.mu.Lock()
	defer s.mu.Unlock()
	uplink.Timestamp = time.Now()
	s.Devices[uplink.DeviceEUI] = &uplink
	s.log.Info("simulated LoRaWAN uplink", "device_eui", uplink.DeviceEUI, "port", uplink.Port)
}

// SimulateUplinkJSON injects a simulated uplink from a JSON payload.
func (s *LoRaWANServerSimulator) SimulateUplinkJSON(data []byte) error {
	var uplink LoRaWANUplink
	if err := json.Unmarshal(data, &uplink); err != nil {
		return err
	}
	s.SimulateUplink(uplink)
	return nil
}
