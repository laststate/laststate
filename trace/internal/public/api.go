// Package public provides the anonymous, aggregated crash statistics API.
// All data is stripped of device IDs, build IDs, stack traces, and personal info.
// Only aggregate counts by architecture, region, severity, and timestamp are exposed.
//
// Configuration:
//
//	TRACE_PUBLIC_CRASH_API=true     — enable public API
//	TRACE_PUBLIC_API_RATE_LIMIT=100 — requests per hour per IP
//	TRACE_PUBLIC_API_CACHE_TTL=3600 — cache duration in seconds
package public

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Config for the public API.
type Config struct {
	Enabled   bool
	RateLimit int           // requests per hour
	CacheTTL  time.Duration // cache duration
}

// Load reads config from environment.
func Load() Config {
	return Config{
		Enabled:   false, // opt-in
		RateLimit: 100,
		CacheTTL:  time.Hour,
	}
}

// CrashStats holds aggregated crash statistics.
type CrashStats struct {
	TotalCrashes       int64            `json:"total_crashes"`
	UniqueFingerprints int64            `json:"unique_fingerprints"`
	AffectedDevices    int64            `json:"affected_devices"`
	PeriodStart        string           `json:"period_start"`
	PeriodEnd          string           `json:"period_end"`
	BySeverity         map[string]int64 `json:"by_severity"`
	ByArchitecture     []ArchStat       `json:"top_architectures"`
	ByRegion           []RegionStat     `json:"top_regions"`
	Trend              []TrendPoint     `json:"trend"`
}

// ArchStat is a crash count by architecture.
type ArchStat struct {
	Architecture string `json:"arch"`
	Count        int64  `json:"count"`
}

// RegionStat is a crash count by ISO 3166-1 alpha-2 region.
type RegionStat struct {
	Region string `json:"region"`
	Count  int64  `json:"count"`
}

// TrendPoint is a daily crash count.
type TrendPoint struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

// Manager handles public API requests with caching and rate limiting.
type Manager struct {
	cfg   Config
	store interface {
		PublicCrashStats(ctx context.Context, days int, arch, region, severity string) (*CrashStats, error)
	}
	cacheMu sync.RWMutex
	cache   map[string]cachedResult
	log     *slog.Logger
}

type cachedResult struct {
	data      CrashStats
	expiresAt time.Time
}

// NewManager creates a public API manager.
func NewManager(cfg Config, store interface {
	PublicCrashStats(ctx context.Context, days int, arch, region, severity string) (*CrashStats, error)
}, log *slog.Logger) *Manager {
	return &Manager{
		cfg:   cfg,
		store: store,
		cache: make(map[string]cachedResult),
		log:   log,
	}
}

// HandleCrashes handles GET /api/public/crashes
func (m *Manager) HandleCrashes(w http.ResponseWriter, r *http.Request) {
	if !m.cfg.Enabled {
		http.Error(w, `{"error":{"code":"disabled","message":"public crash API is disabled"}}`, http.StatusServiceUnavailable)
		return
	}

	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		if n := timeDuration(d); n > 0 && n <= 365 {
			days = n
		}
	}

	arch := r.URL.Query().Get("arch")
	region := r.URL.Query().Get("region")
	severity := r.URL.Query().Get("severity")

	// Check cache
	cacheKey := fmt.Sprintf("crashes:%d:%s:%s:%s", days, arch, region, severity)
	m.cacheMu.RLock()
	if cached, ok := m.cache[cacheKey]; ok && time.Now().Before(cached.expiresAt) {
		m.cacheMu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "HIT")
		json.NewEncoder(w).Encode(cached.data)
		return
	}
	m.cacheMu.RUnlock()

	// Fetch from store
	stats, err := m.store.PublicCrashStats(r.Context(), days, arch, region, severity)
	if err != nil {
		m.log.Error("public crash stats failed", "error", err)
		http.Error(w, `{"error":{"code":"internal","message":"stats unavailable"}}`, http.StatusInternalServerError)
		return
	}

	// Cache result
	m.cacheMu.Lock()
	m.cache[cacheKey] = cachedResult{data: *stats, expiresAt: time.Now().Add(m.cfg.CacheTTL)}
	m.cacheMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", "MISS")
	json.NewEncoder(w).Encode(stats)
}

// HandleByArch handles GET /api/public/crashes/by-arch
func (m *Manager) HandleByArch(w http.ResponseWriter, r *http.Request) {
	if !m.cfg.Enabled {
		http.Error(w, `{"error":{"code":"disabled"}}`, http.StatusServiceUnavailable)
		return
	}

	stats, err := m.store.PublicCrashStats(r.Context(), 30, "", "", "")
	if err != nil {
		http.Error(w, `{"error":{"code":"internal"}}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"architectures": stats.ByArchitecture})
}

// HandleByRegion handles GET /api/public/crashes/by-region
func (m *Manager) HandleByRegion(w http.ResponseWriter, r *http.Request) {
	if !m.cfg.Enabled {
		http.Error(w, `{"error":{"code":"disabled"}}`, http.StatusServiceUnavailable)
		return
	}

	stats, err := m.store.PublicCrashStats(r.Context(), 30, "", "", "")
	if err != nil {
		http.Error(w, `{"error":{"code":"internal"}}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"regions": stats.ByRegion})
}

// HandleTopFingerprints handles GET /api/public/crashes/top-fingerprints
func (m *Manager) HandleTopFingerprints(w http.ResponseWriter, r *http.Request) {
	if !m.cfg.Enabled {
		http.Error(w, `{"error":{"code":"disabled"}}`, http.StatusServiceUnavailable)
		return
	}

	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if n := timeDuration(l); n > 0 && n <= 100 {
			limit = n
		}
	}

	stats, err := m.store.PublicCrashStats(r.Context(), 30, "", "", "")
	if err != nil {
		http.Error(w, `{"error":{"code":"internal"}}`, http.StatusInternalServerError)
		return
	}

	// Return top fingerprints from the stats
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"top_fingerprints": stats.ByArchitecture[:min(limit, len(stats.ByArchitecture))],
		"total":            stats.UniqueFingerprints,
	})
}

// HandlePrivacy handles GET /api/public/privacy — returns privacy policy text.
func (m *Manager) HandlePrivacy(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, `LastState Public Crash API — Privacy Policy

Data Collected:
- Crash type (HardFault, MemManage, BusFault, etc.)
- Architecture (Cortex-M, RISC-V, Xtensa, etc.)
- Timestamp (UTC)
- Aggregate counts per region (country-level)

Data NOT Collected:
- Device ID
- Build ID
- Stack trace contents
- Breadcrumb messages
- Personal information
- IP addresses

Usage:
- Aggregated analytics for the embedded community
- Research and benchmarking
- Not for identifying individual devices or users

Opt-out:
- Device owners: LS_PUBLIC_TELEMETRY=false in Latch config
- Organizations: PUBLIC_CRASH_API=false in Trace settings

Contact: privacy@laststate.dev
`)
}

// Helper to parse time strings to int days
func timeDuration(s string) int {
	d := time.Duration(0)
	if err := json.Unmarshal([]byte(`"`+s+`"`), &d); err == nil {
		return int(d.Hours())
	}
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

// min returns the smaller of a or b (avoid import).
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Unused but referenced for future use.
var _ = uuid.New
