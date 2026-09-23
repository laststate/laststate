package api

// mockHandler returns a mock HTTP handler that serves the Trace web UI with
// pre-populated mock data. This is used when TRACE_MOCK=true to allow
// development and preview without a database, S3, or queue.
//
// The handler serves:
// - Static files from the UI filesystem (for the React SPA)
// - Mock JSON responses for all API endpoints
//
// It does NOT require any of these services:
// - Database (Postgres)
// - Object storage (S3/local)
// - Message queue (Redis/Postgres/NATS)
// - Workers (no background processing)
//
// All data is in-memory and deterministic so charts render consistently
// across restarts. Authentication is bypassed — the handler treats every
// request as coming from an authenticated admin user.
import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/laststate/trace/internal/mockdata"
)

// MockServer serves the web UI with mock data. It is a minimal http.Handler
// that handles SPA routing and all API endpoints with in-memory responses.
type MockServer struct {
	Log *slog.Logger
	UI  http.FileSystem
}

// NewMockServer creates a new mock server. If webDir is empty or does not
// exist, it falls back to an empty filesystem (only API endpoints work).
func NewMockServer(webDir string, log *slog.Logger) *MockServer {
	var ui http.FileSystem
	if webDir != "" {
		if stInfo, err := os.Stat(webDir); err == nil && stInfo.IsDir() {
			ui = http.Dir(webDir)
		}
	}
	if ui == nil {
		ui = http.FS(emptyFS{})
	}
	return &MockServer{Log: log, UI: ui}
}

// Handler returns the HTTP handler for the mock server. It serves both the
// static UI and the mock API endpoints.
func (s *MockServer) Handler() http.Handler {
	mux := http.NewServeMux()

	// Health endpoints
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"status":  "ready",
			"checks":  map[string]string{"mock": "ok"},
			"version": "0.8.0-mock",
		})
	})

	// API endpoints with mock data
	mux.HandleFunc("GET /api/overview", s.handleOverview)
	mux.HandleFunc("GET /api/stream", s.handleStream)
	mux.HandleFunc("GET /api/issues", s.handleIssuesPage)
	mux.HandleFunc("GET /api/issues/{id}", s.handleIssueDetail)
	mux.HandleFunc("POST /api/issues/{id}/status", s.handleIssueStatus)
	mux.HandleFunc("POST /api/issues/{id}/comments", s.handleIssueComment)
	mux.HandleFunc("POST /api/issues/{id}/assign", s.handleIssueAssign)
	mux.HandleFunc("GET /api/issues/{id}/suspect-commits", s.handleSuspectCommits)
	mux.HandleFunc("GET /api/issues/{id}/replay", s.handleIssueReplay)
	mux.HandleFunc("GET /api/events", s.handleEventsPage)
	mux.HandleFunc("GET /api/events/{id}", s.handleEventDetail)
	mux.HandleFunc("GET /api/events/{id}/raw", s.handleEventRaw)
	mux.HandleFunc("POST /api/events/{id}/reprocess", s.handleEventReprocess)
	mux.HandleFunc("POST /api/events/reprocess-stale", s.handleReprocessStale)
	mux.HandleFunc("GET /api/devices", s.handleDevicesPage)
	mux.HandleFunc("GET /api/devices/{id}", s.handleDeviceDetail)
	mux.HandleFunc("GET /api/devices/{id}/firmware-history", s.handleDeviceHistory)
	mux.HandleFunc("GET /api/releases", s.handleReleases)
	mux.HandleFunc("GET /api/releases/{id}", s.handleRelease)
	mux.HandleFunc("GET /api/releases/{id}/stats", s.handleReleaseStats)
	mux.HandleFunc("GET /api/artifacts", s.handleArtifacts)
	mux.HandleFunc("POST /api/artifacts", s.handleUploadArtifact)
	mux.HandleFunc("GET /api/alerts", s.handleAlerts)
	mux.HandleFunc("POST /api/alerts", s.handleCreateAlert)
	mux.HandleFunc("PATCH /api/alerts/{id}", s.handleUpdateAlert)
	mux.HandleFunc("GET /api/channels", s.handleChannels)
	mux.HandleFunc("POST /api/channels", s.handleCreateChannel)
	mux.HandleFunc("GET /api/relays", s.handleRelays)
	mux.HandleFunc("GET /api/projects", s.handleProjects)
	mux.HandleFunc("POST /api/projects", s.handleCreateProject)
	mux.HandleFunc("PATCH /api/projects/{id}", s.handleUpdateProject)
	mux.HandleFunc("DELETE /api/projects/{id}", s.handleDeleteProject)
	mux.HandleFunc("GET /api/hardware", s.handleHardware)
	mux.HandleFunc("GET /api/hardware/compare", s.handleHardware)
	mux.HandleFunc("POST /api/hardware", s.handleCreateHardware)
	mux.HandleFunc("GET /api/boots", s.handleBootSessions)
	mux.HandleFunc("GET /api/jobs/dead", s.handleDeadJobs)
	mux.HandleFunc("POST /api/jobs/dead/{id}/requeue", s.handleRequeueDead)
	mux.HandleFunc("DELETE /api/jobs/dead/{id}", s.handleDiscardDead)
	mux.HandleFunc("GET /api/audit", s.handleAudit)
	mux.HandleFunc("GET /api/search", s.handleSearch)
	mux.HandleFunc("GET /api/me", s.handleMe)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/bootstrap", s.handleBootstrap)
	mux.HandleFunc("GET /api/settings", s.handleSettings)
	mux.HandleFunc("PUT /api/settings", s.handleSettings)
	mux.HandleFunc("POST /api/tokens", s.handleCreateToken)
	mux.HandleFunc("GET /api/tokens", s.handleListTokens)
	mux.HandleFunc("DELETE /api/tokens/{id}", s.handleRevokeToken)
	mux.HandleFunc("POST /api/analytics/export", s.handleAnalyticsExport)

	// Auth endpoints
	mux.HandleFunc("GET /api/auth/oidc/login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/", http.StatusFound)
	})
	mux.HandleFunc("GET /api/auth/oidc/callback", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/", http.StatusFound)
	})
	mux.HandleFunc("POST /api/auth/register", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 201, map[string]any{"user": mockdata.Me()})
	})
	mux.HandleFunc("POST /api/auth/signup", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 201, map[string]any{"user": mockdata.Me()})
	})

	// Capabilities and OpenAPI
	mux.HandleFunc("GET /v1/relay/capabilities", s.handleCapabilities)
	mux.HandleFunc("GET /openapi.json", s.handleOpenAPI)

	// Billing & usage (mock = local deployment: everything unlocked)
	mux.HandleFunc("GET /v1/billing/tiers", s.handleBillingTiers)
	mux.HandleFunc("GET /v1/billing/subscription", s.handleBillingSubscription)
	mux.HandleFunc("POST /v1/billing/checkout", s.handleBillingCheckout)
	mux.HandleFunc("GET /v1/billing/plans", s.handleBillingPlans)
	mux.HandleFunc("GET /v1/usage", s.handleUsage)

	// Ingest endpoints (accept but don't store)
	mux.HandleFunc("POST /v1/ingest", s.handleIngest)
	mux.HandleFunc("POST /v1/events", s.handleIngest)
	mux.HandleFunc("POST /v1/events:batch", s.handleIngestBatch)
	mux.HandleFunc("POST /v1/artifacts", s.handleIngestArtifact)
	mux.HandleFunc("POST /v1/relay/heartbeat", s.handleRelayHeartbeat)

	// Metrics
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprint(w, "# HELP trace_mock_total Total mock requests\n# TYPE trace_mock_total counter\ntrace_mock_total 0\n")
	})

	// SPA fallback: serve index.html for any non-API, non-static path
	if s.UI != nil {
		fileServer := http.FileServer(s.UI)
		mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") ||
				strings.HasPrefix(r.URL.Path, "/v1/") ||
				strings.HasPrefix(r.URL.Path, "/health/") ||
				r.URL.Path == "/metrics" ||
				r.URL.Path == "/openapi.json" {
				http.NotFound(w, r)
				return
			}
			if r.URL.Path != "/" && !strings.Contains(r.URL.Path, ".") {
				r.URL.Path = "/"
			}
			if r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, "index.html") {
				w.Header().Set("Cache-Control", "no-cache")
			} else if strings.Contains(r.URL.Path, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			fileServer.ServeHTTP(w, r)
		}))
	}

	return mux
}

// ---- Mock handlers ----

func (s *MockServer) handleOverview(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, mockdata.Overview())
}

// handleStream pushes fresh mock overview snapshots over SSE so the mock UI
// has the same real-time behavior as the production server.
func (s *MockServer) handleStream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "no_stream", "streaming unsupported by connection", false)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	send := func(event string, payload any) bool {
		b, err := json.Marshal(payload)
		if err != nil {
			return true
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return false
		}
		fl.Flush()
		return true
	}
	if !send("hello", map[string]any{"ok": true, "ts": time.Now().UTC()}) {
		return
	}
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			if !send("overview", mockdata.Overview()) {
				return
			}
		}
	}
}

func (s *MockServer) handleIssuesPage(w http.ResponseWriter, r *http.Request) {
	offset := 0
	limit := 25
	if p := r.URL.Query().Get("offset"); p != "" {
		fmt.Sscanf(p, "%d", &offset)
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	writeJSON(w, 200, mockdata.IssuesPage(offset, limit))
}

func (s *MockServer) handleIssueDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, 400, "bad_id", "id required", false)
		return
	}
	writeJSON(w, 200, mockdata.IssueDetail(id))
}

func (s *MockServer) handleIssueStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"status": "updated"})
}

func (s *MockServer) handleIssueComment(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 201, map[string]any{"id": "c-new-" + fmt.Sprint(time.Now().Unix())})
}

func (s *MockServer) handleIssueAssign(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"assigned": true})
}

func (s *MockServer) handleSuspectCommits(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"items": []map[string]any{
		{"sha": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2", "message": "fix: sensor init race condition", "author": "alice@fleet.io", "release": "v1.8.11"},
	}})
}

func (s *MockServer) handleIssueReplay(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"frames": []map[string]any{
		{"type": "log", "message": "sensor_driver initialized", "timestamp": time.Now().Add(-72 * time.Hour).Format(time.RFC3339), "category": "init"},
		{"type": "log", "message": "CAN bus active", "timestamp": time.Now().Add(-71 * time.Hour).Format(time.RFC3339), "category": "bus"},
		{"type": "error", "message": "null ptr at sensor_driver.c:142", "timestamp": time.Now().Add(-2 * time.Hour).Format(time.RFC3339), "category": "crash"},
	}})
}

func (s *MockServer) handleEventsPage(w http.ResponseWriter, r *http.Request) {
	offset := 0
	limit := 25
	if p := r.URL.Query().Get("offset"); p != "" {
		fmt.Sscanf(p, "%d", &offset)
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	writeJSON(w, 200, mockdata.EventsPage(offset, limit))
}

func (s *MockServer) handleEventDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	writeJSON(w, 200, map[string]any{
		"event": map[string]any{
			"id":          id,
			"event_id":    "evt_" + id + "abcdef01",
			"severity":    "error",
			"state":       "processed",
			"pipeline":    "issue",
			"frames":      `[{"function":"main","file":"main.c","line":1,"address":4198000}]`,
			"analysis":    `{"summary":"Mock event for preview","architecture_name":"arm64"}`,
			"received_at": time.Now().Format(time.RFC3339),
		},
		"history": []map[string]any{},
	})
}

func (s *MockServer) handleEventRaw(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\"mock-event.lep\"")
	w.WriteHeader(200)
	w.Write([]byte("MOCK_LEP_EVENT_DATA"))
}

func (s *MockServer) handleEventReprocess(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 202, map[string]string{"status": "queued"})
}

func (s *MockServer) handleReprocessStale(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"queued": 0})
}

func (s *MockServer) handleDevicesPage(w http.ResponseWriter, r *http.Request) {
	offset := 0
	limit := 25
	if p := r.URL.Query().Get("offset"); p != "" {
		fmt.Sscanf(p, "%d", &offset)
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	writeJSON(w, 200, mockdata.DevicesPage(offset, limit))
}

func (s *MockServer) handleDeviceDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	writeJSON(w, 200, map[string]any{
		"device": map[string]any{
			"id":               id,
			"device_id":        "FW-mock-dev-" + id,
			"status":           "online",
			"product":          "Fleet Node A",
			"firmware_version": "v1.8.11",
			"last_seen":        time.Now().Format(time.RFC3339),
		},
		"events": []map[string]any{},
	})
}

func (s *MockServer) handleDeviceHistory(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"items": []map[string]any{
		{"firmware_version": "v1.8.11", "build_id": "build-2026081501", "last_seen": time.Now().Format(time.RFC3339)},
		{"firmware_version": "v1.8.10", "build_id": "build-2026081401", "last_seen": time.Now().Add(-48 * time.Hour).Format(time.RFC3339)},
	}})
}

func (s *MockServer) handleReleases(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, mockdata.Releases())
}

func (s *MockServer) handleRelease(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	releases := mockdata.Releases()
	items := releases["items"].([]map[string]any)
	for _, rel := range items {
		if rel["id"] == id {
			writeJSON(w, 200, rel)
			return
		}
	}
	writeErr(w, 404, "not_found", "release not found", false)
}

func (s *MockServer) handleReleaseStats(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	writeJSON(w, 200, mockdata.ReleaseStats(id))
}

func (s *MockServer) handleArtifacts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, mockdata.Artifacts())
}

func (s *MockServer) handleUploadArtifact(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 201, map[string]any{"id": "ART-new", "status": "quarantine"})
}

func (s *MockServer) handleAlerts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, mockdata.Alerts())
}

func (s *MockServer) handleCreateAlert(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 201, map[string]any{"id": "ALT-new"})
}

func (s *MockServer) handleUpdateAlert(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"id": r.PathValue("id"), "updated": true})
}

func (s *MockServer) handleChannels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, mockdata.Channels())
}

func (s *MockServer) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 201, map[string]any{"id": "CH-new"})
}

func (s *MockServer) handleRelays(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, mockdata.Relays())
}

func (s *MockServer) handleProjects(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, mockdata.Projects())
}

func (s *MockServer) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 201, map[string]any{"id": "PRJ-new", "name": "New Project", "slug": "new-project"})
}

func (s *MockServer) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"id": r.PathValue("id"), "updated": true})
}

func (s *MockServer) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"deleted": true})
}

func (s *MockServer) handleHardware(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, mockdata.Hardware())
}

func (s *MockServer) handleCreateHardware(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 201, map[string]any{"id": "HW-new"})
}

func (s *MockServer) handleBootSessions(w http.ResponseWriter, r *http.Request) {
	offset := 0
	limit := 25
	if p := r.URL.Query().Get("offset"); p != "" {
		fmt.Sscanf(p, "%d", &offset)
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	writeJSON(w, 200, mockdata.BootSessions(offset, limit))
}

func (s *MockServer) handleDeadJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, mockdata.DeadJobs())
}

func (s *MockServer) handleRequeueDead(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"requeued": true})
}

func (s *MockServer) handleDiscardDead(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"discarded": true})
}

func (s *MockServer) handleAudit(w http.ResponseWriter, r *http.Request) {
	offset := 0
	limit := 25
	if p := r.URL.Query().Get("offset"); p != "" {
		fmt.Sscanf(p, "%d", &offset)
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	writeJSON(w, 200, mockdata.Audit(offset, limit))
}

func (s *MockServer) handleAuditExport(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"data": "mock-audit-export"})
}

func (s *MockServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	writeJSON(w, 200, mockdata.Search(q))
}

func (s *MockServer) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, mockdata.Me())
}

func (s *MockServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, mockdata.Login("", ""))
}

func (s *MockServer) handleLogout(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "logged_out"})
}

func (s *MockServer) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, mockdata.Bootstrap())
}

func (s *MockServer) handleSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"retention_events_days":  90,
		"retention_health_days":  30,
		"retention_logs_days":    14,
		"retention_metrics_days": 30,
		"analyzer_version_min":   1,
	})
}

func (s *MockServer) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 201, map[string]any{
		"token":  map[string]any{"id": "TKN-001", "name": "relay", "scopes": []string{"event:write", "event:read"}},
		"secret": "mock-token-secret-abc123",
	})
}

func (s *MockServer) handleListTokens(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"items": []map[string]any{
		{"id": "TKN-001", "name": "relay", "scopes": []string{"event:write", "event:read"}, "created_at": time.Now().Format(time.RFC3339)},
	}})
}

func (s *MockServer) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"revoked": true})
}

func (s *MockServer) handleAnalyticsExport(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"rows": 0, "sink": "mock"})
}

func (s *MockServer) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"api_version":     "1",
		"lep_versions":    []int{1, 2},
		"max_event_size":  1048576,
		"compression":     []string{"identity", "zstd"},
		"batch_ingest":    true,
		"binary_batch":    true,
		"artifact_upload": true,
		"authentication":  []string{"bearer", "cookie"},
		"server_id":       "trace-mock-v0.8.0",
		"oidc":            false,
		"saml":            false,
		"scim":            false,
		"public_register": true,
		"queue":           "mock",
		"deployment":      "local",
		"billing_enabled": false,
	})
}

func (s *MockServer) handleBillingTiers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"items": []map[string]any{
			{
				"id": "local", "name": "Everything unlocked", "priceCents": 0, "currency": "usd",
				"maxDevices": -1, "maxEventsPerDay": -1, "retentionDays": -1,
				"maxApiTokens": -1, "maxAlertRules": -1,
				"isUnlimitedDevices": true, "isUnlimitedEvents": true, "isUnlimitedRetention": true,
				"features": []string{"all_features", "unlimited_devices", "unlimited_events", "no_paywall"},
			},
		},
		"tiers":    []map[string]any{},
		"currency": "usd", "interval": "month",
		"deployment": "local", "billing_enabled": false,
	})
}

func (s *MockServer) handleBillingSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		writeErr(w, 400, "billing_disabled", "subscriptions are not used in local deployment mode", false)
		return
	}
	writeJSON(w, 200, map[string]any{
		"id": "local", "planId": "local", "plan_id": "local", "status": "active",
		"provider": "self_hosted", "currentPeriodEnd": "", "current_period_end": "",
		"deployment": "local", "billing_enabled": false,
	})
}

func (s *MockServer) handleBillingCheckout(w http.ResponseWriter, r *http.Request) {
	writeErr(w, 400, "billing_disabled", "billing is not available in local deployment mode — everything is unlocked", false)
}

func (s *MockServer) handleBillingPlans(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"plans": []map[string]any{
			{"id": "local", "name": "Everything unlocked", "price": "Free", "highlight": true,
				"features": []string{"unlimited devices", "unlimited events", "unlimited retention", "no paywall"}},
		},
		"interval": "month", "deployment": "local", "billing_enabled": false,
	})
}

func (s *MockServer) handleUsage(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"metrics": []map[string]any{
			{"metric_name": "events", "value": 1284},
			{"metric_name": "devices", "value": 42},
			{"metric_name": "api_calls", "value": 310},
			{"metric_name": "releases", "value": 6},
			{"metric_name": "issues", "value": 23},
		},
	})
}

func (s *MockServer) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "Trace API",
			"description": "Mock API for preview/development",
			"version":     "0.8.0-mock",
		},
		"paths": map[string]any{
			"/api/overview": map[string]any{"get": map[string]any{"summary": "Overview dashboard"}},
			"/api/issues":   map[string]any{"get": map[string]any{"summary": "List issues"}},
		},
	})
}

func (s *MockServer) handleIngest(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 202, map[string]any{"id": "mock-event-" + fmt.Sprint(time.Now().Unix()), "status": "accepted", "duplicate": false})
}

func (s *MockServer) handleIngestBatch(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"accepted":   []map[string]any{{"event_id": "batch-mock-1", "receipt": "mock-receipt-1"}},
		"duplicates": []map[string]any{},
		"rejected":   []map[string]any{},
	})
}

func (s *MockServer) handleIngestArtifact(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 201, map[string]any{"id": "ART-mock-" + fmt.Sprint(time.Now().Unix()), "status": "quarantine"})
}

func (s *MockServer) handleRelayHeartbeat(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"relay_id": "relay-mock", "status": "online"})
}

// emptyFS is a no-op filesystem used when no web directory is available.
type emptyFS struct{}

func (emptyFS) Open(name string) (fs.File, error) { return nil, fs.ErrNotExist }
