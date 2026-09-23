// Package chaos implements controlled fault injection for MCU testing.
// Injects faults via Relay adapters and verifies that Latch captures everything correctly.
//
// Injection types:
//   - hardfault: Trigger a hard fault exception
//   - watchdog: Trigger watchdog timeout
//   - brownout: Simulate voltage drop
//   - corrupt-stack: Corrupt stack pointer
//   - nested-fault: Trigger nested exception
//   - interrupted-flash: Interrupt flash operation
//
// Configuration:
//
//	CHAOS_ENABLED=true
//	CHAOS_ADAPTER=serial|tcp|adapter-sdk
package chaos

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// InjectionType represents a type of fault injection.
type InjectionType string

const (
	TypeHardFault        InjectionType = "hardfault"
	TypeWatchdog         InjectionType = "watchdog"
	TypeBrownout         InjectionType = "brownout"
	TypeCorruptStack     InjectionType = "corrupt-stack"
	TypeNestedFault      InjectionType = "nested-fault"
	TypeInterruptedFlash InjectionType = "interrupted-flash"
)

// AllInjectionTypes returns all supported injection types.
func AllInjectionTypes() []InjectionType {
	return []InjectionType{
		TypeHardFault, TypeWatchdog, TypeBrownout, TypeCorruptStack, TypeNestedFault, TypeInterruptedFlash,
	}
}

// InjectionResult holds the result of a fault injection test.
type InjectionResult struct {
	ID         string        `json:"id"`
	Type       InjectionType `json:"type"`
	DeviceID   string        `json:"device_id"`
	Status     string        `json:"status"` // "success", "timeout", "corrupt", "error"
	Captured   bool          `json:"captured"`
	DurationMs int           `json:"duration_ms"`
	Error      string        `json:"error,omitempty"`
	Timestamp  time.Time     `json:"timestamp"`
}

// Manager handles chaos engineering operations.
type Manager struct {
	config     Config
	log        *slog.Logger
	mu         sync.RWMutex
	results    []InjectionResult
	maxResults int
	adapter    Adapter
}

// Config for the chaos engineering feature.
type Config struct {
	Enabled    bool
	Adapter    string // "serial", "tcp", "adapter-sdk"
	TimeoutSec int
}

// Load reads config from environment.
func Load() Config {
	return Config{
		Enabled:    false,
		Adapter:    "serial",
		TimeoutSec: 10,
	}
}

// Adapter interface for fault injection execution.
type Adapter interface {
	Inject(ctx context.Context, deviceID string, injectType InjectionType) (*InjectionResult, error)
}

// NewManager creates a chaos engineering manager.
func NewManager(cfg Config, log *slog.Logger) *Manager {
	return &Manager{
		config:     cfg,
		log:        log,
		results:    make([]InjectionResult, 0, 100),
		maxResults: 100,
	}
}

// SetAdapter sets the adapter for fault injection.
func (m *Manager) SetAdapter(a Adapter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.adapter = a
}

// InjectFault triggers a fault injection on a device.
func (m *Manager) InjectFault(ctx context.Context, deviceID string, injectType InjectionType) (*InjectionResult, error) {
	if !m.config.Enabled {
		return nil, fmt.Errorf("chaos engineering is disabled")
	}

	m.mu.RLock()
	adapter := m.adapter
	m.mu.RUnlock()

	if adapter == nil {
		return nil, fmt.Errorf("no adapter configured")
	}

	result, err := adapter.Inject(ctx, deviceID, injectType)
	if err != nil {
		return nil, err
	}

	// Store result
	m.mu.Lock()
	m.results = append(m.results, *result)
	if len(m.results) > m.maxResults {
		m.results = m.results[len(m.results)-m.maxResults:]
	}
	m.mu.Unlock()

	return result, nil
}

// GetResults returns recent injection results.
func (m *Manager) GetResults() []InjectionResult {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]InjectionResult, len(m.results))
	copy(result, m.results)
	return result
}

// HandleInject handles POST /api/chaos/inject
func (m *Manager) HandleInject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		DeviceID  string        `json:"device_id"`
		Injection InjectionType `json:"injection"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if body.DeviceID == "" || body.Injection == "" {
		http.Error(w, "device_id and injection required", http.StatusBadRequest)
		return
	}

	result, err := m.InjectFault(r.Context(), body.DeviceID, body.Injection)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":{"code":"%s","message":"%s"}}`, "injection_failed", err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// HandleResults handles GET /api/chaos/results
func (m *Manager) HandleResults(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"results": m.GetResults(),
		"enabled": m.config.Enabled,
	})
}

// HandleStatus handles GET /api/chaos/status
func (m *Manager) HandleStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"enabled": m.config.Enabled,
		"adapter": m.config.Adapter,
		"types":   AllInjectionTypes(),
		"total":   len(m.GetResults()),
	})
}
