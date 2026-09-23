// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

// Package config loads and validates relay.yaml.
package config

import (
	"bytes"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the versioned on-disk configuration contract. Unknown YAML fields
// are rejected so a typo never silently disables a safety or delivery option.
type Config struct {
	Version      int           `yaml:"version"`
	Instance     Instance      `yaml:"instance"`
	Sources      []Source      `yaml:"sources"`
	Destinations []Destination `yaml:"destinations"`
	Delivery     Delivery      `yaml:"delivery"`
	Spool        Spool         `yaml:"spool"`
	Admin        Admin         `yaml:"admin"`
	Metrics      Metrics       `yaml:"metrics"`
	Analysis     Analysis      `yaml:"analysis"`
	Artifacts    Artifacts     `yaml:"artifacts"`
	Privacy      Privacy       `yaml:"privacy"`
	Crypto       Crypto        `yaml:"crypto"`
}

type Instance struct {
	ID      string `yaml:"id"`
	Name    string `yaml:"name"`
	DataDir string `yaml:"data_dir"`
}

type Source struct {
	ID      string `yaml:"id"`
	Type    string `yaml:"type"`
	Enabled *bool  `yaml:"enabled"`
	Path    string `yaml:"path"` // deprecated directory-source compatibility

	Directory struct {
		Path           string        `yaml:"path"`
		ArchivePath    string        `yaml:"archive_path"`
		QuarantinePath string        `yaml:"quarantine_path"`
		PollInterval   time.Duration `yaml:"poll_interval"`
		StableFor      time.Duration `yaml:"stable_for"`
		MaxFiles       int           `yaml:"max_files_per_poll"`
	} `yaml:"directory"`

	Serial struct {
		Device       string        `yaml:"device"`
		Baud         int           `yaml:"baud"`
		Reconnect    bool          `yaml:"reconnect"`
		ReconnectMin time.Duration `yaml:"reconnect_min"`
		ReconnectMax time.Duration `yaml:"reconnect_max"`
		ReadTimeout  time.Duration `yaml:"read_timeout"`
	} `yaml:"serial"`

	TCP struct {
		Listen       string        `yaml:"listen"`
		MaxClients   int           `yaml:"max_clients"`
		ReadTimeout  time.Duration `yaml:"read_timeout"`
		WriteTimeout time.Duration `yaml:"write_timeout"`
		TLS          TLS           `yaml:"tls"`
	} `yaml:"tcp"`

	UDP struct {
		Listen       string   `yaml:"listen"`
		MaxDatagram  int      `yaml:"max_datagram_bytes"`
		AllowedCIDRs []string `yaml:"allowed_cidrs"`
	} `yaml:"udp"`

	HTTP struct {
		Listen            string        `yaml:"listen"`
		Token             string        `yaml:"token"` // literal or secret reference
		MaxBodyBytes      int64         `yaml:"max_body_bytes"`
		ReadTimeout       time.Duration `yaml:"read_timeout"`
		WriteTimeout      time.Duration `yaml:"write_timeout"`
		IdleTimeout       time.Duration `yaml:"idle_timeout"`
		MaxConcurrent     int           `yaml:"max_concurrent"`
		RequestsPerSecond float64       `yaml:"requests_per_second"`
		BurstSize         int           `yaml:"burst_size"`
		RateLimitMaxKeys  int           `yaml:"rate_limit_max_keys"`
		TLS               TLS           `yaml:"tls"`
	} `yaml:"http"`

	MQTT struct {
		Broker   string   `yaml:"broker"`
		ClientID string   `yaml:"client_id"`
		Topics   []string `yaml:"topics"`
		QoS      int      `yaml:"qos"`
		Username string   `yaml:"username"`
		Password string   `yaml:"password"`
		TLS      TLS      `yaml:"tls"`
	} `yaml:"mqtt"`

	// Experimental transports. These collectors talk to platform backends
	// (a JSON-over-TCP BLE gateway, native SocketCAN on Linux, LoRaWAN network
	// servers over MQTT/HTTP) and are not yet recommended for production.
	BLE struct {
		Adapter         string        `yaml:"adapter"`
		ScanDuration    time.Duration `yaml:"scan_duration"`
		ServiceUUID     string        `yaml:"service_uuid"`
		CharUUID        string        `yaml:"char_uuid"`
		NotifyEnabled   bool          `yaml:"notify_enabled"`
		RSSIThreshold   int           `yaml:"rssi_threshold"`
		MaxDevices      int           `yaml:"max_devices"`
		FilterByAddress []string      `yaml:"filter_by_address"`
	} `yaml:"ble"`

	CAN struct {
		Interface   string `yaml:"interface"`
		Baudrate    int    `yaml:"baudrate"`
		FDEnabled   bool   `yaml:"fd_enabled"`
		BRS         bool   `yaml:"brs"`
		FilterID    uint32 `yaml:"filter_id"`
		FilterMask  uint32 `yaml:"filter_mask"`
		Protocol    string `yaml:"protocol"` // LEP, JSON, RAW
		AdapterPath string `yaml:"adapter_path"`
		RemoteHost  string `yaml:"remote_host"`
		RemotePort  int    `yaml:"remote_port"`
	} `yaml:"can"`

	LoRaWAN struct {
		Server     string `yaml:"server"`
		APIKey     string `yaml:"api_key"` // literal or secret reference
		NetworkID  string `yaml:"network_id"`
		DeviceEUI  string `yaml:"device_eui"`
		Port       uint8  `yaml:"port"`
		Protocol   string `yaml:"protocol"` // MQTT, HTTP, GRPC
		TLSEnabled bool   `yaml:"tls_enabled"`
		MaxRetries int    `yaml:"max_retries"`
	} `yaml:"lorawan"`

	Adapter struct {
		Command string   `yaml:"command"`
		Args    []string `yaml:"args"`
		Env     []string `yaml:"env"`
		Dir     string   `yaml:"dir"`
	} `yaml:"adapter"`

	Framing struct {
		Type           string        `yaml:"type"`
		MaxFrameBytes  int           `yaml:"max_frame_bytes"`
		PartialTimeout time.Duration `yaml:"partial_timeout"`
	} `yaml:"framing"`

	ACK struct {
		Mode         string        `yaml:"mode"` // none or lsak-v1
		WriteTimeout time.Duration `yaml:"write_timeout"`
	} `yaml:"ack"`
}

type TLS struct {
	Enabled            bool   `yaml:"enabled"`
	CertFile           string `yaml:"cert_file"`
	KeyFile            string `yaml:"key_file"`
	CAFile             string `yaml:"ca_file"`
	ClientCAFile       string `yaml:"client_ca_file"`
	RequireClientCert  bool   `yaml:"require_client_cert"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
	ServerName         string `yaml:"server_name"`
}

type Destination struct {
	ID       string `yaml:"id"`
	Type     string `yaml:"type"`
	Enabled  *bool  `yaml:"enabled"`
	Required bool   `yaml:"required"`
	Priority int    `yaml:"priority"`
	URL      string `yaml:"url"`
	Auth     struct {
		Type  string `yaml:"type"`
		Token string `yaml:"token"` // env:, file:, keyring: or literal
	} `yaml:"auth"`
	TLS   TLS `yaml:"tls"`
	Batch struct {
		Enabled      bool          `yaml:"enabled"`
		MaxEvents    int           `yaml:"max_events"`
		MaxBytes     int           `yaml:"max_bytes"`
		MaxWait      time.Duration `yaml:"max_wait"`
		PreferBinary bool          `yaml:"prefer_binary"`
		PreferZstd   bool          `yaml:"prefer_zstd"`
	} `yaml:"batch"`
}

type Route struct {
	ProjectID  string   `yaml:"project_id"`
	SourceID   string   `yaml:"source_id"`
	EventTypes []uint8  `yaml:"event_types"`
	Targets    []string `yaml:"destinations"`
}

type Delivery struct {
	Mode        string  `yaml:"mode"` // mirror, priority, route, local-only
	Concurrency int     `yaml:"concurrency"`
	Routes      []Route `yaml:"routes"`
	Retry       struct {
		MinDelay    time.Duration `yaml:"min_delay"`
		MaxDelay    time.Duration `yaml:"max_delay"`
		Multiplier  float64       `yaml:"multiplier"`
		Jitter      bool          `yaml:"jitter"`
		MaxAttempts int           `yaml:"max_attempts"`
	} `yaml:"retry"`
	CircuitBreaker struct {
		FailureThreshold int           `yaml:"failure_threshold"`
		OpenFor          time.Duration `yaml:"open_for"`
	} `yaml:"circuit_breaker"`
}

type Spool struct {
	MaxBytes           int64         `yaml:"max_bytes"`
	MinFreeBytes       int64         `yaml:"min_free_bytes"`
	DeliveredRetention time.Duration `yaml:"delivered_retention"`
	PressurePolicy     string        `yaml:"pressure_policy"`
	Fsync              string        `yaml:"fsync"`
}

type Admin struct {
	Listen       string        `yaml:"listen"`
	Token        string        `yaml:"token"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	IdleTimeout  time.Duration `yaml:"idle_timeout"`
	TLS          TLS           `yaml:"tls"`
}

type Metrics struct {
	Enabled bool   `yaml:"enabled"`
	Listen  string `yaml:"listen"`
}

type Analysis struct {
	Enabled           bool              `yaml:"enabled"`
	LLVMSymbolizer    string            `yaml:"llvm_symbolizer"`
	ARMAddr2Line      string            `yaml:"arm_addr2line"`
	RISCVAddr2Line    string            `yaml:"riscv_addr2line"`
	SourcePathPrefix  string            `yaml:"source_path_prefix"`
	SourcePathMaps    []SourcePathMap   `yaml:"source_path_maps"`
	MemoryMap         []MemoryMapRegion `yaml:"memory_map"`
	SymbolCacheDir    string            `yaml:"symbol_cache_dir"`
	SymbolServer      string            `yaml:"symbol_server"`
	SymbolServerToken string            `yaml:"symbol_server_token"`
}

type SourcePathMap struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

type MemoryMapRegion struct {
	Name  string `yaml:"name"`
	Start uint64 `yaml:"start"`
	End   uint64 `yaml:"end"`
	Class string `yaml:"class"`
}

type Artifacts struct {
	Directory    string   `yaml:"directory"`
	AutoDiscover []string `yaml:"auto_discover"`
}

type Crypto struct {
	Enabled            bool        `yaml:"enabled"`
	Keys               []CryptoKey `yaml:"keys"`
	ActiveKeyID        string      `yaml:"active_key_id"`
	AuthKeyID          string      `yaml:"auth_key_id"`
	DecryptOnIngest    bool        `yaml:"decrypt_on_ingest"`
	AllowOpaqueForward bool        `yaml:"allow_opaque_forward"`
	ReplayWindow       int         `yaml:"replay_window"`
}

type CryptoKey struct {
	ID  string `yaml:"id"`
	Key string `yaml:"key"` // env:, file:, or hex/base64 material
}

type Privacy struct {
	PreserveRawLocally bool                          `yaml:"preserve_raw_locally"`
	Destinations       map[string]DestinationPrivacy `yaml:"destinations"`
}

type DestinationPrivacy struct {
	DropTLVTypes    []uint16 `yaml:"drop_tlv_types"`
	MaxPayloadBytes int      `yaml:"max_payload_bytes"`
	BlockEncrypted  bool     `yaml:"block_encrypted"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var cfg Config
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, err
	}
	if cfg.Instance.DataDir == "" {
		cfg.Instance.DataDir = filepath.Join(filepath.Dir(path), "data")
	}
	if !filepath.IsAbs(cfg.Instance.DataDir) {
		cfg.Instance.DataDir = filepath.Join(filepath.Dir(path), cfg.Instance.DataDir)
	}
	abs, err := filepath.Abs(cfg.Instance.DataDir)
	if err != nil {
		return cfg, fmt.Errorf("instance.data_dir: %w", err)
	}
	cfg.Instance.DataDir = filepath.Clean(abs)
	cfg.defaults()
	if !filepath.IsAbs(cfg.Artifacts.Directory) {
		cfg.Artifacts.Directory = filepath.Join(filepath.Dir(path), cfg.Artifacts.Directory)
	}
	cfg.Artifacts.Directory, err = filepath.Abs(cfg.Artifacts.Directory)
	if err != nil {
		return cfg, fmt.Errorf("artifacts.directory: %w", err)
	}
	for i, candidate := range cfg.Artifacts.AutoDiscover {
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(filepath.Dir(path), candidate)
		}
		cfg.Artifacts.AutoDiscover[i] = filepath.Clean(candidate)
	}
	return cfg, Validate(cfg)
}

func (cfg *Config) defaults() {
	if cfg.Delivery.Mode == "" {
		cfg.Delivery.Mode = "mirror"
	}
	if cfg.Delivery.Concurrency <= 0 {
		cfg.Delivery.Concurrency = 4
	}
	if cfg.Delivery.Retry.MinDelay <= 0 {
		cfg.Delivery.Retry.MinDelay = time.Second
	}
	if cfg.Delivery.Retry.MaxDelay <= 0 {
		cfg.Delivery.Retry.MaxDelay = 15 * time.Minute
	}
	if cfg.Delivery.Retry.Multiplier < 1 {
		cfg.Delivery.Retry.Multiplier = 2
	}
	if cfg.Delivery.Retry.MaxAttempts <= 0 {
		cfg.Delivery.Retry.MaxAttempts = 50
	}
	if cfg.Delivery.CircuitBreaker.FailureThreshold <= 0 {
		cfg.Delivery.CircuitBreaker.FailureThreshold = 5
	}
	if cfg.Delivery.CircuitBreaker.OpenFor <= 0 {
		cfg.Delivery.CircuitBreaker.OpenFor = 30 * time.Second
	}
	if cfg.Spool.MaxBytes <= 0 {
		cfg.Spool.MaxBytes = 10 << 30
	}
	if cfg.Spool.MinFreeBytes <= 0 {
		cfg.Spool.MinFreeBytes = 256 << 20
	}
	if cfg.Spool.DeliveredRetention <= 0 {
		cfg.Spool.DeliveredRetention = 30 * 24 * time.Hour
	}
	if cfg.Spool.PressurePolicy == "" {
		cfg.Spool.PressurePolicy = "reject-new"
	}
	if cfg.Spool.Fsync == "" {
		cfg.Spool.Fsync = "full"
	}
	if cfg.Admin.Listen == "" {
		cfg.Admin.Listen = "127.0.0.1:8383"
	}
	if cfg.Admin.ReadTimeout <= 0 {
		cfg.Admin.ReadTimeout = 15 * time.Second
	}
	if cfg.Admin.WriteTimeout <= 0 {
		cfg.Admin.WriteTimeout = 30 * time.Second
	}
	if cfg.Admin.IdleTimeout <= 0 {
		cfg.Admin.IdleTimeout = 60 * time.Second
	}
	if cfg.Metrics.Listen == "" {
		cfg.Metrics.Listen = "127.0.0.1:9467"
	}
	if !cfg.Privacy.PreserveRawLocally {
		// Raw preservation is the safe default. The field remains explicit in
		// normalized configuration and can only be changed by a future storage policy.
		cfg.Privacy.PreserveRawLocally = true
	}
	if cfg.Artifacts.Directory == "" {
		cfg.Artifacts.Directory = filepath.Join(cfg.Instance.DataDir, "artifacts")
	}
	if cfg.Analysis.SymbolCacheDir == "" {
		cfg.Analysis.SymbolCacheDir = filepath.Join(cfg.Instance.DataDir, "symbol-cache")
	}
	if cfg.Crypto.ReplayWindow <= 0 {
		cfg.Crypto.ReplayWindow = 10000
	}
	if !cfg.Crypto.Enabled {
		cfg.Crypto.AllowOpaqueForward = true
	}
	for i := range cfg.Sources {
		s := &cfg.Sources[i]
		if s.Directory.PollInterval <= 0 {
			s.Directory.PollInterval = 2 * time.Second
		}
		if s.Directory.StableFor <= 0 {
			s.Directory.StableFor = time.Second
		}
		if s.Directory.MaxFiles <= 0 {
			s.Directory.MaxFiles = 100
		}
		if s.Framing.Type == "" {
			if s.Type == "udp" || s.Type == "http" || s.Type == "mqtt" || s.Type == "adapter" {
				s.Framing.Type = "raw"
			} else {
				s.Framing.Type = "latch-stream"
			}
		}
		if s.Framing.MaxFrameBytes <= 0 {
			s.Framing.MaxFrameBytes = 4 << 20
		}
		if s.Framing.PartialTimeout <= 0 {
			s.Framing.PartialTimeout = 30 * time.Second
		}
		if s.ACK.Mode == "" {
			s.ACK.Mode = "none"
		}
		if s.ACK.WriteTimeout <= 0 {
			s.ACK.WriteTimeout = 2 * time.Second
		}
		if s.Serial.ReconnectMin <= 0 {
			s.Serial.ReconnectMin = time.Second
		}
		if s.Serial.ReconnectMax <= 0 {
			s.Serial.ReconnectMax = 30 * time.Second
		}
		if s.Serial.ReadTimeout <= 0 {
			s.Serial.ReadTimeout = time.Second
		}
		if s.TCP.MaxClients <= 0 {
			s.TCP.MaxClients = 64
		}
		if s.UDP.MaxDatagram <= 0 {
			s.UDP.MaxDatagram = 65507
		}
		if s.TCP.ReadTimeout <= 0 {
			s.TCP.ReadTimeout = 2 * time.Minute
		}
		if s.TCP.WriteTimeout <= 0 {
			s.TCP.WriteTimeout = 10 * time.Second
		}
		if s.HTTP.MaxBodyBytes <= 0 {
			s.HTTP.MaxBodyBytes = 4 << 20
		}
		if s.HTTP.ReadTimeout <= 0 {
			s.HTTP.ReadTimeout = 15 * time.Second
		}
		if s.HTTP.WriteTimeout <= 0 {
			s.HTTP.WriteTimeout = 30 * time.Second
		}
		if s.HTTP.IdleTimeout <= 0 {
			s.HTTP.IdleTimeout = 60 * time.Second
		}
		if s.HTTP.MaxConcurrent <= 0 {
			s.HTTP.MaxConcurrent = 32
		}
		if s.MQTT.QoS < 0 {
			s.MQTT.QoS = 1
		}
		if s.Type == "mqtt" && s.MQTT.ClientID == "" {
			s.MQTT.ClientID = "laststate-relay"
		}
	}
	for i := range cfg.Destinations {
		d := &cfg.Destinations[i]
		if d.Auth.Type == "" {
			d.Auth.Type = "none"
		}
		if d.Batch.MaxEvents <= 0 {
			d.Batch.MaxEvents = 100
		}
		if d.Batch.MaxBytes <= 0 {
			d.Batch.MaxBytes = 4 << 20
		}
		if d.Batch.MaxWait <= 0 {
			d.Batch.MaxWait = 2 * time.Second
		}
	}
}

func Validate(cfg Config) error {
	if cfg.Version != 1 {
		return fmt.Errorf("version: must be 1")
	}
	if strings.TrimSpace(cfg.Instance.DataDir) == "" {
		return fmt.Errorf("instance.data_dir: must not be empty")
	}
	if cfg.Spool.MaxBytes < 1<<20 {
		return fmt.Errorf("spool.max_bytes: must be at least 1 MiB")
	}
	if cfg.Spool.PressurePolicy != "reject-new" && cfg.Spool.PressurePolicy != "drop-oldest-delivered" {
		return fmt.Errorf("spool.pressure_policy: supported values are reject-new and drop-oldest-delivered")
	}
	if cfg.Spool.Fsync != "full" && cfg.Spool.Fsync != "balanced" && cfg.Spool.Fsync != "none" {
		return fmt.Errorf("spool.fsync: supported values are full, balanced and none")
	}

	seen := map[string]bool{}
	for index, source := range cfg.Sources {
		prefix := fmt.Sprintf("sources[%d]", index)
		if strings.TrimSpace(source.ID) == "" {
			return fmt.Errorf("%s.id: must not be empty", prefix)
		}
		if seen[source.ID] {
			return fmt.Errorf("%s.id: duplicate source", prefix)
		}
		seen[source.ID] = true
		switch source.Type {
		case "directory":
			if source.Directory.Path == "" && source.Path == "" {
				return fmt.Errorf("%s.directory.path: must not be empty", prefix)
			}
		case "serial":
			if source.Serial.Device == "" || source.Serial.Baud <= 0 {
				return fmt.Errorf("%s.serial: device and positive baud are required", prefix)
			}
		case "tcp":
			if err := validateListen(prefix+".tcp.listen", source.TCP.Listen); err != nil {
				return err
			}
			if err := validateServerTLS(prefix+".tcp.tls", source.TCP.TLS); err != nil {
				return err
			}
		case "udp":
			if err := validateListen(prefix+".udp.listen", source.UDP.Listen); err != nil {
				return err
			}
			for _, cidr := range source.UDP.AllowedCIDRs {
				if _, _, err := net.ParseCIDR(cidr); err != nil {
					return fmt.Errorf("%s.udp.allowed_cidrs: invalid CIDR %q", prefix, cidr)
				}
			}
		case "http":
			if err := validateListen(prefix+".http.listen", source.HTTP.Listen); err != nil {
				return err
			}
			if err := validateServerTLS(prefix+".http.tls", source.HTTP.TLS); err != nil {
				return err
			}
		case "mqtt":
			if strings.TrimSpace(source.MQTT.Broker) == "" {
				return fmt.Errorf("%s.mqtt.broker: must not be empty", prefix)
			}
			if len(source.MQTT.Topics) == 0 {
				return fmt.Errorf("%s.mqtt.topics: must not be empty", prefix)
			}
			if source.MQTT.QoS < 0 || source.MQTT.QoS > 2 {
				return fmt.Errorf("%s.mqtt.qos: must be 0, 1, or 2", prefix)
			}
		case "adapter":
			if strings.TrimSpace(source.Adapter.Command) == "" {
				return fmt.Errorf("%s.adapter.command: must not be empty", prefix)
			}
		default:
			return fmt.Errorf("%s.type: supported types are directory, serial, tcp, udp, http, mqtt and adapter", prefix)
		}
		if source.Framing.Type != "latch-stream" && source.Framing.Type != "cobs" && source.Framing.Type != "raw" {
			return fmt.Errorf("%s.framing.type: supported types are latch-stream, cobs and raw", prefix)
		}
		if source.ACK.Mode != "none" && source.ACK.Mode != "lsak-v1" {
			return fmt.Errorf("%s.ack.mode: supported values are none and lsak-v1", prefix)
		}
	}

	seen = map[string]bool{}
	for index, destination := range cfg.Destinations {
		prefix := fmt.Sprintf("destinations[%d]", index)
		if destination.ID == "" || seen[destination.ID] {
			return fmt.Errorf("%s.id: must be unique and non-empty", prefix)
		}
		seen[destination.ID] = true
		if destination.Type != "trace" {
			return fmt.Errorf("%s.type: only trace is supported", prefix)
		}
		parsed, err := url.Parse(destination.URL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("%s.url: must be an absolute URL", prefix)
		}
		if parsed.User != nil {
			return fmt.Errorf("%s.url: embedded credentials are forbidden", prefix)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("%s.url: scheme must be http or https", prefix)
		}
		if destination.Auth.Type != "none" && destination.Auth.Type != "bearer" {
			return fmt.Errorf("%s.auth.type: supported values are none and bearer", prefix)
		}
		if destination.Auth.Type == "bearer" && destination.Auth.Token == "" {
			return fmt.Errorf("%s.auth.token: must not be empty", prefix)
		}
		if parsed.Scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
			return fmt.Errorf("%s.url: remote destinations must use https", prefix)
		}
		if destination.TLS.InsecureSkipVerify && parsed.Scheme != "https" {
			return fmt.Errorf("%s.tls.insecure_skip_verify: only valid for https", prefix)
		}
		if destination.TLS.InsecureSkipVerify && !isLoopbackHost(parsed.Hostname()) {
			// Guard: insecure_skip_verify is lab-only; require explicit opt-in for non-loopback
			if env := strings.TrimSpace(strings.ToLower(os.Getenv("RELAY_ALLOW_INSECURE"))); env != "1" && env != "true" {
				return fmt.Errorf("%s.tls.insecure_skip_verify: refusing non-loopback insecure TLS without RELAY_ALLOW_INSECURE=1 (lab-only)", prefix)
			}
		}
		if destination.Batch.MaxEvents < 1 || destination.Batch.MaxEvents > 1000 {
			return fmt.Errorf("%s.batch.max_events: must be between 1 and 1000", prefix)
		}
		if destination.Batch.MaxBytes < 1024 || destination.Batch.MaxBytes > 64<<20 {
			return fmt.Errorf("%s.batch.max_bytes: must be between 1 KiB and 64 MiB", prefix)
		}
	}

	switch cfg.Delivery.Mode {
	case "mirror", "priority", "route", "local-only":
	default:
		return fmt.Errorf("delivery.mode: supported modes are mirror, priority, route and local-only")
	}
	if cfg.Delivery.Mode == "route" && len(cfg.Delivery.Routes) == 0 {
		return fmt.Errorf("delivery.routes: at least one route is required in route mode")
	}
	for i, route := range cfg.Delivery.Routes {
		if route.ProjectID != "" {
			return fmt.Errorf("delivery.routes[%d].project_id: project routing requires the protocol repository to define a canonical project identity TLV", i)
		}
		if len(route.Targets) == 0 {
			return fmt.Errorf("delivery.routes[%d].destinations: must not be empty", i)
		}
		for _, target := range route.Targets {
			if !seen[target] {
				return fmt.Errorf("delivery.routes[%d].destinations: unknown destination %q", i, target)
			}
		}
	}
	if err := validateListen("admin.listen", cfg.Admin.Listen); err != nil {
		return err
	}
	if err := validateServerTLS("admin.tls", cfg.Admin.TLS); err != nil {
		return err
	}
	if cfg.Metrics.Enabled {
		if err := validateListen("metrics.listen", cfg.Metrics.Listen); err != nil {
			return err
		}
	}
	for destinationID, policy := range cfg.Privacy.Destinations {
		if !seen[destinationID] {
			return fmt.Errorf("privacy.destinations.%s: unknown destination", destinationID)
		}
		if policy.MaxPayloadBytes < 0 {
			return fmt.Errorf("privacy.destinations.%s.max_payload_bytes: must not be negative", destinationID)
		}
	}
	if cfg.Crypto.Enabled {
		if len(cfg.Crypto.Keys) == 0 {
			return fmt.Errorf("crypto.keys: at least one key is required when crypto.enabled is true")
		}
		ids := map[string]bool{}
		for i, key := range cfg.Crypto.Keys {
			if strings.TrimSpace(key.ID) == "" {
				return fmt.Errorf("crypto.keys[%d].id: must not be empty", i)
			}
			if strings.TrimSpace(key.Key) == "" {
				return fmt.Errorf("crypto.keys[%d].key: must not be empty", i)
			}
			if strings.HasPrefix(key.Key, "keyring:") {
				return fmt.Errorf("crypto.keys[%d].key: keyring references are not supported in this build; use env or file references", i)
			}
			ids[key.ID] = true
		}
		if cfg.Crypto.ActiveKeyID != "" && !ids[cfg.Crypto.ActiveKeyID] {
			return fmt.Errorf("crypto.active_key_id: %q is not present in crypto.keys", cfg.Crypto.ActiveKeyID)
		}
		if cfg.Crypto.AuthKeyID != "" && !ids[cfg.Crypto.AuthKeyID] {
			return fmt.Errorf("crypto.auth_key_id: %q is not present in crypto.keys", cfg.Crypto.AuthKeyID)
		}
	}
	if !cfg.Privacy.PreserveRawLocally {
		return fmt.Errorf("privacy.preserve_raw_locally: must remain true in this release")
	}
	// Admin token empty is allowed only on loopback for lab use.
	if strings.TrimSpace(cfg.Admin.Token) == "" {
		host, _, err := net.SplitHostPort(cfg.Admin.Listen)
		if err != nil || !isLoopbackHost(host) {
			return fmt.Errorf("admin.token: required when admin.listen is not loopback")
		}
	}
	return nil
}

func validateListen(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s: must not be empty", field)
	}
	if _, _, err := net.SplitHostPort(value); err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	return nil
}

func validateServerTLS(field string, value TLS) error {
	if !value.Enabled {
		return nil
	}
	if value.CertFile == "" || value.KeyFile == "" {
		return fmt.Errorf("%s: cert_file and key_file are required", field)
	}
	if value.RequireClientCert && value.ClientCAFile == "" {
		return fmt.Errorf("%s.client_ca_file: required when require_client_cert is true", field)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") || !strings.Contains(host, ".") || strings.HasSuffix(host, ".local") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

func (source Source) IsEnabled() bool { return source.Enabled == nil || *source.Enabled }
func (source Source) DirectoryPath() string {
	if source.Directory.Path != "" {
		return source.Directory.Path
	}
	return source.Path
}
func (destination Destination) IsEnabled() bool {
	return destination.Enabled == nil || *destination.Enabled
}
