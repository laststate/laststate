package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Listen          string
	PublicURL       string
	DatabaseURL     string
	ObjectDir       string
	MaxEventSize    int64
	MaxArtifactSize int64
	MaxBatchEvents  int
	MaxBatchBytes   int64
	WorkerN         int
	Lease           time.Duration
	Bootstrap       bool
	AdminEmail      string
	AdminPassword   string
	// OpenUI allows unauthenticated GET on UI APIs. Default false (secure).
	OpenUI bool
	// Mode: "all" (API+workers), "api", "worker"
	Mode string
	// modeWasCorrected is true if Mode was silently corrected from an invalid value.
	// Used by ValidateProduction to warn about potential misconfiguration.
	modeWasCorrected bool

	// Deployment is the deployment mode: "local" (self-hosted, everything
	// unlocked — no mandatory auth, no quotas, no paywall) or "enterprise"
	// (managed LastState SaaS — auth required, quotas, billing/paywall).
	// TRACE_DEPLOYMENT (default "enterprise"). "local" forces OpenUI and
	// AllowPublicRegister on.
	Deployment string

	// HTTP hardening
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	MaxHeaderBytes  int
	RateLimitPerMin int
	MaxConnsPerIP   int

	// Retention / GC
	RetentionDays   int
	GCInterval      time.Duration
	EnableRetention bool

	// Queue backend: postgres|memory (nats|redis|cf reserved)
	QueueDriver string
	QueueURL    string

	// S3 / MinIO
	S3Endpoint  string
	S3Region    string
	S3Bucket    string
	S3AccessKey string
	S3SecretKey string

	// Mailer (SMTP)
	SMTPHost   string
	SMTPPort   int
	SMTPUser   string
	SMTPPass   string
	SMTPFrom   string
	SMTPSecure string // "tls" or "ssl" or ""

	// OIDC
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string

	// Security / enterprise flags
	AllowPublicRegister bool   // TRACE_ALLOW_PUBLIC_REGISTER (default false)
	OIDCAutoJoin        bool   // TRACE_OIDC_AUTO_JOIN first org as developer (default false)
	SAMLInsecure        bool   // TRACE_SAML_INSECURE allow unsigned ACS (default false)
	TrustedProxies      string // TRACE_TRUSTED_PROXIES comma-separated CIDRs/IPs
	SecretsKey          string // TRACE_SECRETS_KEY 32-byte base64/hex for AES-GCM secret at rest
	CookieSecure        bool          // TRACE_COOKIE_SECURE (default true when PublicURL is https)
	SessionIdleTimeout time.Duration // TRACE_SESSION_IDLE_TIMEOUT (default 24h; 0 disables idle expiry)
	ShutdownTimeoutSec  int    // TRACE_SHUTDOWN_TIMEOUT_SEC (default 10)
	AppVersion          string

	// --- Feature flags ---

	// MockMode enables mock data mode (no DB required). TRACE_MOCK=true
	MockMode bool

	// Chaos engineering
	ChaosEnabled bool   // CHAOS_ENABLED
	ChaosAdapter string // CHAOS_ADAPTER
	ChaosTimeout int    // CHAOS_TIMEOUT_SEC

	// Public crash API
	PublicCrashAPI     bool // PUBLIC_CRASH_API
	PublicAPIRateLimit int  // PUBLIC_API_RATE_LIMIT

	// Soundboard
	SoundboardEnabled bool    // SOUNDBOARD_ENABLED
	SoundboardVolume  float64 // SOUNDBOARD_VOLUME

	// Device DNA
	DNADetectionThreshold float64 // DNA_DETECTION_THRESHOLD

	// Fleet health recalc interval
	FleetHealthRecalcInterval int // FLEET_HEALTH_RECALC_INTERVAL_MIN

	// Postmortem AI
	PostmortemAPIKey  string // TRACE_POSTMORTEM_API_KEY
	PostmortemMaxFree int    // TRACE_POSTMORTEM_MAX_FREE

	// Crash-to-PR
	GitHubToken  string // TRACE_GITHUB_TOKEN
	GitHubRepo   string // TRACE_GITHUB_REPO
	GitHubBranch string // TRACE_GITHUB_DEFAULT_BRANCH

	// Anomaly detection (server-side backup)
	AnomalyEnabled bool // ANOMALY_ENABLED

	// Memorial wall
	MemorialEnabled bool // MEMORIAL_ENABLED
}

func Load() Config {
	c := Config{
		Listen:          env("TRACE_LISTEN", ":8080"),
		PublicURL:       env("TRACE_PUBLIC_URL", "http://localhost:8080"),
		DatabaseURL:     env("TRACE_DATABASE_URL", "postgres://trace:trace@localhost:5432/trace"),
		ObjectDir:       env("TRACE_OBJECT_DIR", "./data/objects"),
		MaxEventSize:    envInt64("TRACE_MAX_EVENT_SIZE", 4<<20),
		MaxArtifactSize: envInt64("TRACE_MAX_ARTIFACT_SIZE", 64<<20),
		MaxBatchEvents:  int(envInt64("TRACE_MAX_BATCH_EVENTS", 100)),
		MaxBatchBytes:   envInt64("TRACE_MAX_BATCH_BYTES", 50*(4<<20)),
		WorkerN:         int(envInt64("TRACE_WORKERS", 2)),
		Lease:           time.Duration(envInt64("TRACE_JOB_LEASE_SEC", 30)) * time.Second,
		Bootstrap:       env("TRACE_BOOTSTRAP", "true") == "true",
		AdminEmail:      env("TRACE_ADMIN_EMAIL", "admin@localhost"),
		AdminPassword:   env("TRACE_ADMIN_PASSWORD", ""), // empty = generate random; never default to "admin"
		OpenUI:          env("TRACE_OPEN_UI", "false") == "true",
		Mode:            strings.ToLower(env("TRACE_MODE", "all")),
		Deployment:      strings.ToLower(env("TRACE_DEPLOYMENT", "enterprise")),

		ReadTimeout:     time.Duration(envInt64("TRACE_HTTP_READ_TIMEOUT_SEC", 30)) * time.Second,
		WriteTimeout:    time.Duration(envInt64("TRACE_HTTP_WRITE_TIMEOUT_SEC", 60)) * time.Second,
		IdleTimeout:     time.Duration(envInt64("TRACE_HTTP_IDLE_TIMEOUT_SEC", 120)) * time.Second,
		MaxHeaderBytes:  int(envInt64("TRACE_HTTP_MAX_HEADER_BYTES", 1<<20)),
		RateLimitPerMin: int(envInt64("TRACE_RATE_LIMIT_PER_MIN", 600)),
		MaxConnsPerIP:   int(envInt64("TRACE_MAX_CONNS_PER_IP", 64)),

		RetentionDays:   int(envInt64("TRACE_RETENTION_DAYS", 90)),
		GCInterval:      time.Duration(envInt64("TRACE_GC_INTERVAL_MIN", 60)) * time.Minute,
		EnableRetention: env("TRACE_ENABLE_RETENTION", "true") == "true",

		QueueDriver: env("TRACE_QUEUE", "postgres"),
		QueueURL:    env("TRACE_QUEUE_URL", ""),

		S3Endpoint:       env("TRACE_S3_ENDPOINT", ""),
		S3Region:         env("TRACE_S3_REGION", "us-east-1"),
		S3Bucket:         env("TRACE_S3_BUCKET", ""),
		S3AccessKey:      env("TRACE_S3_ACCESS_KEY", ""),
		S3SecretKey:      env("TRACE_S3_SECRET_KEY", ""),
		OIDCIssuer:       env("TRACE_OIDC_ISSUER", ""),
		OIDCClientID:     env("TRACE_OIDC_CLIENT_ID", ""),
		OIDCClientSecret: env("TRACE_OIDC_CLIENT_SECRET", ""),
		OIDCRedirectURL:  env("TRACE_OIDC_REDIRECT_URL", ""),

		// Mailer (SMTP)
		SMTPHost:   env("TRACE_SMTP_HOST", ""),
		SMTPPort:   int(envInt64("TRACE_SMTP_PORT", 587)),
		SMTPUser:   env("TRACE_SMTP_USER", ""),
		SMTPPass:   env("TRACE_SMTP_PASS", ""),
		SMTPFrom:   env("TRACE_SMTP_FROM", ""),
		SMTPSecure: env("TRACE_SMTP_SECURE", ""),

		AllowPublicRegister: env("TRACE_ALLOW_PUBLIC_REGISTER", "false") == "true",
		OIDCAutoJoin:        env("TRACE_OIDC_AUTO_JOIN", "false") == "true",
		SAMLInsecure:        env("TRACE_SAML_INSECURE", "false") == "true",
		SessionIdleTimeout:  24 * time.Hour,
		TrustedProxies:      env("TRACE_TRUSTED_PROXIES", ""),
		SecretsKey:          env("TRACE_SECRETS_KEY", ""),
		ShutdownTimeoutSec:  int(envInt64("TRACE_SHUTDOWN_TIMEOUT_SEC", 10)),
		AppVersion:          env("TRACE_VERSION", "0.8.0"),

		// Feature flags
		MockMode:                  env("TRACE_MOCK", "false") == "true",
		ChaosEnabled:              env("CHAOS_ENABLED", "false") == "true",
		ChaosAdapter:              env("CHAOS_ADAPTER", "serial"),
		ChaosTimeout:              int(envInt64("CHAOS_TIMEOUT_SEC", 10)),
		PublicCrashAPI:            env("PUBLIC_CRASH_API", "true") == "true",
		PublicAPIRateLimit:        int(envInt64("PUBLIC_API_RATE_LIMIT", 100)),
		SoundboardEnabled:         env("SOUNDBOARD_ENABLED", "true") == "true",
		SoundboardVolume:          float64(envInt64("SOUNDBOARD_VOLUME", 50)) / 100.0,
		DNADetectionThreshold:     float64(envInt64("DNA_DETECTION_THRESHOLD", 95)) / 100.0,
		FleetHealthRecalcInterval: int(envInt64("FLEET_HEALTH_RECALC_INTERVAL_MIN", 15)),
		PostmortemAPIKey:          env("TRACE_POSTMORTEM_API_KEY", ""),
		PostmortemMaxFree:         int(envInt64("TRACE_POSTMORTEM_MAX_FREE", 10)),
		GitHubToken:               env("TRACE_GITHUB_TOKEN", ""),
		GitHubRepo:                env("TRACE_GITHUB_REPO", ""),
		GitHubBranch:              env("TRACE_GITHUB_DEFAULT_BRANCH", "main"),
		AnomalyEnabled:            env("ANOMALY_ENABLED", "false") == "true",
		MemorialEnabled:           env("MEMORIAL_ENABLED", "true") == "true",
	}
	if c.OIDCRedirectURL == "" && c.OIDCIssuer != "" {
		c.OIDCRedirectURL = strings.TrimRight(c.PublicURL, "/") + "/api/auth/oidc/callback"
	}
	if c.Mode != "all" && c.Mode != "api" && c.Mode != "worker" {
		c.modeWasCorrected = true
		c.Mode = "all"
	}
	// Deployment: only "local" and "enterprise" are valid. Anything else is
	// silently corrected to "enterprise" (the safe default). Local mode is an
	// opt-in escape hatch for self-hosters: everything unlocked.
	if c.Deployment != "local" && c.Deployment != "enterprise" {
		c.Deployment = "enterprise"
	}
	if c.IsLocal() {
		// Local = self-hosted by the operator. The person running the server
		// is implicitly trusted: no auth gate on the UI and open registration,
		// mirroring what TRACE_MOCK does for previews.
		c.OpenUI = true
		c.AllowPublicRegister = true
	}
	// Default SMTPSecure to 'tls' only when SMTPHost is configured
	if c.SMTPHost != "" && c.SMTPSecure == "" {
		c.SMTPSecure = "tls"
	}
	if v := strings.TrimSpace(os.Getenv("TRACE_COOKIE_SECURE")); v != "" {
		c.CookieSecure = v == "true" || v == "1"
	} else {
		c.CookieSecure = strings.HasPrefix(strings.ToLower(c.PublicURL), "https://")
		// In production, default to Secure cookies even if PublicURL not set to https
		// (common when TLS is terminated at reverse proxy)
		if !c.CookieSecure && (strings.EqualFold(env("TRACE_ENV", ""), "production") || strings.EqualFold(env("TRACE_ENV", ""), "prod")) {
			c.CookieSecure = true
		}
	}
	if v := strings.TrimSpace(os.Getenv("TRACE_SESSION_IDLE_TIMEOUT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			c.SessionIdleTimeout = d
		}
	}
	return c
}

// IsLocal reports whether Trace runs in self-hosted "everything unlocked"
// deployment mode (TRACE_DEPLOYMENT=local).
func (c Config) IsLocal() bool { return c.Deployment == "local" }

// IsEnterprise reports whether Trace runs in managed SaaS mode
// (TRACE_DEPLOYMENT=enterprise) with auth, quotas and billing enabled.
func (c Config) IsEnterprise() bool { return c.Deployment == "enterprise" }

// ValidateProduction rejects insecure production settings.
// It returns a slice of warnings (non-fatal) and errors (fatal).
type ValidationWarning struct {
	Field string
	Msg   string
}

func (c Config) ValidateProduction() ([]ValidationWarning, error) {
	var warnings []ValidationWarning

	if env("TRACE_ENV", "") != "production" && env("TRACE_ENV", "") != "prod" {
		return warnings, nil
	}

	if c.OpenUI && !c.IsLocal() {
		return warnings, fmt.Errorf("TRACE_OPEN_UI must be false in production")
	}
	if c.AdminPassword == "admin" || c.AdminPassword == "password" {
		return warnings, fmt.Errorf("insecure TRACE_ADMIN_PASSWORD in production")
	}
	if c.AllowPublicRegister && !c.IsLocal() {
		return warnings, fmt.Errorf("TRACE_ALLOW_PUBLIC_REGISTER must be false in production")
	}
	if c.SAMLInsecure {
		return warnings, fmt.Errorf("TRACE_SAML_INSECURE must be false in production")
	}
	if c.OIDCAutoJoin {
		return warnings, fmt.Errorf("TRACE_OIDC_AUTO_JOIN must be false in production")
	}

	// Validate sslmode in DatabaseURL
	if c.DatabaseURL != "" {
		if strings.Contains(c.DatabaseURL, "sslmode=disable") {
			warnings = append(warnings, ValidationWarning{
				Field: "DatabaseURL",
				Msg:   "sslmode=disable detected in DatabaseURL — TLS encryption is not enabled",
			})
		}
	}

	// Validate SecretsKey length (must be 32 bytes for AES-GCM)
	if c.SecretsKey != "" {
		keyBytes, err := decodeSecretsKey(c.SecretsKey)
		if err != nil {
			return warnings, fmt.Errorf("invalid TRACE_SECRETS_KEY: %w", err)
		}
		if len(keyBytes) != 32 {
			return warnings, fmt.Errorf("TRACE_SECRETS_KEY must be 32 bytes (got %d)", len(keyBytes))
		}
		// Reject placeholder / dev secrets in production
		placeholders := []string{"changeme", "super-secret", "devlocal", "example", "placeholder"}
		lower := strings.ToLower(c.SecretsKey)
		for _, p := range placeholders {
			if strings.Contains(lower, p) {
				return warnings, fmt.Errorf("TRACE_SECRETS_KEY contains placeholder value (%q) — generate with: openssl rand -hex 32", p)
			}
		}
	}
	// Reject placeholder JWT / bootstrap secrets in production
	if c.SecretsKey != "" || true {
		placeholderChecks := map[string]string{
			"TRACE_JWT_SECRET":      os.Getenv("TRACE_JWT_SECRET"),
			"TRACE_BOOTSTRAP_TOKEN": os.Getenv("TRACE_BOOTSTRAP_TOKEN"),
		}
		for k, v := range placeholderChecks {
			lv := strings.ToLower(v)
			if v != "" && (strings.Contains(lv, "super-secret") || strings.Contains(lv, "changeme") || strings.Contains(lv, "devlocal123") || strings.Contains(lv, "fullsecrettokenforlocaldev")) {
				return warnings, fmt.Errorf("%s contains placeholder dev value — must be overridden in production", k)
			}
		}
	}
	// In production, require strong admin password
	if c.AdminPassword != "" && len(c.AdminPassword) < 12 {
		return warnings, fmt.Errorf("TRACE_ADMIN_PASSWORD must be at least 12 characters in production")
	}

	if c.modeWasCorrected {
		warnings = append(warnings, ValidationWarning{
			Field: "Mode",
			Msg:   "TRACE_MODE was silently corrected to 'all' — check your configuration",
		})
	}

	return warnings, nil
}

// decodeSecretsKey decodes a hex or base64-encoded secrets key to bytes.
func decodeSecretsKey(key string) ([]byte, error) {
	// Try hex first
	if len(key)%2 == 0 {
		if b, err := decodeHex(key); err == nil {
			return b, nil
		}
	}
	// Try base64
	if b, err := decodeBase64(key); err == nil {
		return b, nil
	}
	return nil, fmt.Errorf("key is not valid hex or base64")
}

func decodeHex(s string) ([]byte, error) {
	b := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		var v byte
		for j := i; j < i+2; j++ {
			c := s[j]
			v <<= 4
			switch {
			case c >= '0' && c <= '9':
				v |= c - '0'
			case c >= 'a' && c <= 'f':
				v |= c - 'a' + 10
			case c >= 'A' && c <= 'F':
				v |= c - 'A' + 10
			default:
				return nil, fmt.Errorf("invalid hex char")
			}
		}
		b[i/2] = v
	}
	return b, nil
}

func decodeBase64(s string) ([]byte, error) {
	b := make([]byte, base64.StdEncoding.DecodedLen(len(s)))
	n, err := base64.StdEncoding.Decode(b, []byte(s))
	if err != nil {
		return nil, err
	}
	return b[:n], nil
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func envInt64(k string, def int64) int64 {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}
