package soundboard

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

// CrashEvent represents a crash event that triggers a sound.
type CrashEvent struct {
	ID           string        `json:"id"`
	EventType    EventType     `json:"event_type"`
	Severity     EventSeverity `json:"severity"`
	Architecture string        `json:"architecture"`
	DeviceID     string        `json:"device_id"`
	ProjectID    string        `json:"project_id"`
	Timestamp    time.Time     `json:"timestamp"`
	Fingerprint  string        `json:"fingerprint"`
}

// SoundEvent is sent to the frontend via the soundboard API.
type SoundEvent struct {
	Event      CrashEvent   `json:"event"`
	Profile    SoundProfile `json:"profile"`
	ShouldPlay bool         `json:"should_play"`
}

// Manager coordinates the soundboard service.
type Manager struct {
	mu         sync.RWMutex
	config     Config
	handlers   []func(SoundEvent)
	history    []SoundEvent
	maxHistory int
	log        *slog.Logger
}

// NewManager creates a new soundboard manager.
func NewManager(log *slog.Logger) *Manager {
	return &Manager{
		config:     Load(),
		history:    make([]SoundEvent, 0, 100),
		maxHistory: 100,
		log:        log,
	}
}

// SetConfig updates the soundboard configuration.
func (m *Manager) SetConfig(c Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = c
}

// Config returns the current configuration.
func (m *Manager) Config() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// RegisterHandler adds a callback for when a sound event is fired.
func (m *Manager) RegisterHandler(h func(SoundEvent)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers = append(m.handlers, h)
}

// FireEvent processes a crash event and notifies handlers.
func (m *Manager) FireEvent(evt CrashEvent) {
	m.mu.RLock()
	cfg := m.config
	handlers := make([]func(SoundEvent), len(m.handlers))
	copy(handlers, m.handlers)
	m.mu.RUnlock()

	profile := ProfileFor(evt.EventType)
	shouldPlay := cfg.Enabled && ShouldPlaySound(evt.Severity)

	soundEvt := SoundEvent{
		Event:      evt,
		Profile:    profile,
		ShouldPlay: shouldPlay,
	}

	if cfg.Enabled {
		m.mu.Lock()
		m.history = append(m.history, soundEvt)
		if len(m.history) > m.maxHistory {
			m.history = m.history[len(m.history)-m.maxHistory:]
		}
		m.mu.Unlock()
	}

	for _, h := range handlers {
		h(soundEvt)
	}
}

// History returns recent sound events.
func (m *Manager) History() []SoundEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]SoundEvent, len(m.history))
	copy(result, m.history)
	return result
}

// HandleWebhook processes a crash event from the webhook.
func (m *Manager) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var evt CrashEvent
	if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if evt.ID == "" {
		evt.ID = uuid.New().String()
	}
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now().UTC()
	}

	m.FireEvent(evt)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":      "ok",
		"event_id":    evt.ID,
		"should_play": m.Config().Enabled && ShouldPlaySound(evt.Severity),
	})
}

// HandleStatus returns the soundboard status.
func (m *Manager) HandleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"enabled":  m.config.Enabled,
		"volume":   m.config.Volume,
		"history":  m.History(),
		"profiles": DefaultProfiles(),
	})
}

// HandleUpdate updates soundboard configuration.
func (m *Manager) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Enabled *bool    `json:"enabled"`
		Volume  *float64 `json:"volume"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	if body.Enabled != nil {
		m.config.Enabled = *body.Enabled
	}
	if body.Volume != nil {
		v := *body.Volume
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		m.config.Volume = v
	}
	cfg := m.config
	m.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"enabled": cfg.Enabled,
		"volume":  cfg.Volume,
	})
}
