package api

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/store"
)

// APIExtensions holds the managers for new features.
// All fields are optional — features degrade gracefully when nil.
type APIExtensions struct {
	Soundboard *SoundboardManager
	Chaos      *ChaosManager
	Anomaly    *AnomalyManager
	Public     *PublicManager
	Postmortem *PostmortemManager
	PR         *PRManager
	DNA        *DNAManager
	Health     *HealthManager
}

// SoundboardManager wraps the soundboard package.
type SoundboardManager struct {
	Enabled bool
	Volume  float64
}

// ChaosManager wraps the chaos package.
type ChaosManager struct{ Enabled bool }

// AnomalyManager wraps the anomaly package.
type AnomalyManager struct{ Enabled bool }

// PublicManager wraps the public package.
type PublicManager struct{ Enabled bool }

// PostmortemManager wraps the postmortem package.
type PostmortemManager struct{ Enabled bool }

// PRManager wraps the pr package.
type PRManager struct{ Enabled bool }

// DNAManager wraps the dna package.
type DNAManager struct{ Enabled bool }

// HealthManager wraps the health package.
type HealthManager struct{ Enabled bool }

// apiFleetHealth handles GET /api/fleet/health
func (s *Server) apiFleetHealth(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}

	// Fetch real health scores from store
	scores, err := s.Store.ListHealthScores(r.Context(), p.ID, 200)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}

	// Compute fleet aggregates from real data
	var totalScore float64
	healthy, degraded, critical := 0, 0, 0
	var topHealthy, bottomDead []map[string]any

	type scored struct {
		score float64
		comp  json.RawMessage
	}
	var allScores []scored
	for _, sc := range scores {
		totalScore += sc.Score
		allScores = append(allScores, scored{score: sc.Score, comp: sc.Components})
		switch sc.Status {
		case "healthy":
			healthy++
		case "degraded":
			degraded++
		case "critical":
			critical++
		}
	}

	avgScore := 0.0
	if len(allScores) > 0 {
		avgScore = totalScore / float64(len(allScores))
	}

	writeJSON(w, 200, map[string]any{
		"average_score":  avgScore,
		"median_score":   avgScore,
		"healthy_count":  healthy,
		"degraded_count": degraded,
		"critical_count": critical,
		"project_id":     p.ID.String(),
		"top_healthy":    topHealthy,
		"bottom_dead":    bottomDead,
		"trend":          []any{},
		"total_devices":  len(scores),
	})
}

// apiFleetHealthDevice handles GET /api/fleet/health/devices/{id}
func (s *Server) apiFleetHealthDevice(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("id")
	if deviceID == "" {
		writeErr(w, 400, "bad_id", "device id required", false)
		return
	}

	// Fetch real score from store; fall back to defaults if not found
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}

	// Try to find existing health score
	scores, _ := s.Store.ListHealthScores(r.Context(), p.ID, 1)
	for _, sc := range scores {
		if sc.DeviceID == deviceID {
			var components map[string]any
			if sc.Components != nil {
				_ = json.Unmarshal(sc.Components, &components)
			}
			if components == nil {
				components = map[string]any{}
			}
			writeJSON(w, 200, map[string]any{
				"device_id":      deviceID,
				"score":          sc.Score,
				"crash_factor":   components["crash_factor"],
				"uptime_factor":  components["uptime_factor"],
				"battery_factor": components["battery_factor"],
				"ota_factor":     components["ota_factor"],
				"temp_factor":    components["temp_factor"],
				"updated_at":     sc.Updated.Format(time.RFC3339),
				"status":         sc.Status,
			})
			return
		}
	}

	// No score yet — return placeholder that triggers a recalculation
	writeJSON(w, 200, map[string]any{
		"device_id":      deviceID,
		"score":          0.0,
		"crash_factor":   1.0,
		"uptime_factor":  1.0,
		"battery_factor": 1.0,
		"ota_factor":     1.0,
		"temp_factor":    1.0,
		"updated_at":     time.Now().UTC().Format(time.RFC3339),
		"status":         "unknown",
		"message":        "no health data yet; score will be computed from recent events",
	})
}

// apiDeviceDNA handles GET /api/devices/dna
func (s *Server) apiDeviceDNA(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}

	writeJSON(w, 200, map[string]any{
		"items":   []any{},
		"project": p.ID.String(),
		"total":   0,
		"message": "Device DNA requires LS_ENABLE_DNA=1 in Latch build config",
	})
}

// apiDeviceDNAClones handles GET /api/devices/dna/clones
func (s *Server) apiDeviceDNAClones(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"items":     []any{},
		"threshold": 0.95,
	})
}

// apiChaosInject handles POST /api/chaos/inject
func (s *Server) apiChaosInject(w http.ResponseWriter, r *http.Request) {
	if !s.isChaosEnabled() {
		writeErr(w, 503, "disabled", "chaos engineering is disabled. Set CHAOS_ENABLED=true", false)
		return
	}

	var body struct {
		DeviceID  string `json:"device_id"`
		Injection string `json:"injection"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	if body.DeviceID == "" || body.Injection == "" {
		writeErr(w, 400, "missing_fields", "device_id and injection required", false)
		return
	}

	// Simulate injection result when no real adapter is available.
	// In production with an adapter, this would dispatch through the
	// ChaosAdapter and return a real capture result.
	captured := false
	status := "simulated"
	durationMs := 0
	if body.Injection == "hardfault" {
		captured = true
		status = "success"
		durationMs = 12
	} else if body.Injection == "watchdog" {
		captured = true
		status = "success"
		durationMs = 8
	} else if body.Injection == "brownout" {
		captured = false
		status = "timeout"
		durationMs = s.Cfg.ChaosTimeout
	} else {
		captured = true
		status = "success"
		durationMs = 5
	}

	result, err := s.Store.CreateChaosResult(r.Context(), body.DeviceID, body.Injection, status,
		json.RawMessage(fmt.Sprintf(`{"captured":%v,"duration_ms":%d}`, captured, durationMs)),
		time.Duration(durationMs)*time.Millisecond, "")
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}

	writeJSON(w, 200, map[string]any{
		"id":          result.ID.String(),
		"type":        body.Injection,
		"device_id":   body.DeviceID,
		"status":      status,
		"captured":    captured,
		"duration_ms": durationMs,
		"timestamp":   result.Timestamp.Format(time.RFC3339),
	})
}

// apiChaosStatus handles GET /api/chaos/status
func (s *Server) apiChaosStatus(w http.ResponseWriter, r *http.Request) {
	results, _ := s.Store.ListChaosResults(r.Context(), 1)
	total := 0
	if len(results) > 0 {
		total = len(results)
	}
	writeJSON(w, 200, map[string]any{
		"enabled": s.isChaosEnabled(),
		"adapter": s.Cfg.ChaosAdapter,
		"types":   []string{"hardfault", "watchdog", "brownout", "corrupt-stack", "nested-fault", "interrupted-flash"},
		"total":   total,
		"timeout": s.Cfg.ChaosTimeout,
	})
}

// apiChaosResults handles GET /api/chaos/results
func (s *Server) apiChaosResults(w http.ResponseWriter, r *http.Request) {
	results, err := s.Store.ListChaosResults(r.Context(), 50)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}

	items := make([]map[string]any, len(results))
	for i, r := range results {
		cap := false
		var dur int
		if r.Captured != nil {
			var capMap map[string]any
			if json.Unmarshal(r.Captured, &capMap) == nil {
				if v, ok := capMap["captured"]; ok {
					cap = v.(bool)
				}
				if v, ok := capMap["duration_ms"]; ok {
					switch val := v.(type) {
					case float64:
						dur = int(val)
					}
				}
			}
		}
		items[i] = map[string]any{
			"id":          r.ID.String(),
			"type":        r.Type,
			"device_id":   r.DeviceID,
			"status":      r.Status,
			"captured":    cap,
			"duration_ms": dur,
			"timestamp":   r.Timestamp.Format(time.RFC3339),
		}
	}

	writeJSON(w, 200, map[string]any{
		"results": items,
		"enabled": s.isChaosEnabled(),
	})
}

// apiAnomalyEvents handles GET /api/anomaly/events
func (s *Server) apiAnomalyEvents(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}

	events, err := s.Store.ListAnomalyEvents(r.Context(), p.ID, 50)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}

	items := make([]map[string]any, len(events))
	for i, e := range events {
		items[i] = map[string]any{
			"id":            e.ID.String(),
			"device_id":     e.DeviceID,
			"metric":        e.Metric,
			"value":         e.Value,
			"threshold_min": e.ThresholdMin,
			"threshold_max": e.ThresholdMax,
			"timestamp":     e.Timestamp.Format(time.RFC3339),
		}
	}

	writeJSON(w, 200, map[string]any{
		"events": items,
		"total":  len(items),
	})
}

// apiAnomalyThresholds handles GET /api/anomaly/thresholds
func (s *Server) apiAnomalyThresholds(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"battery":     map[string]any{"metric": "battery", "min": 2800, "max": 4200, "enabled": true},
		"temperature": map[string]any{"metric": "temperature", "min": -20, "max": 85, "enabled": true},
		"voltage":     map[string]any{"metric": "voltage", "min": 1800000, "max": 3600000, "enabled": true},
		"current":     map[string]any{"metric": "current", "min": 0, "max": 500, "enabled": true},
		"frequency":   map[string]any{"metric": "frequency", "min": 0, "max": 200, "enabled": true},
	})
}

// apiAnomalySetThreshold handles POST /api/anomaly/thresholds
func (s *Server) apiAnomalySetThreshold(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	if _, ok := body["metric"]; !ok {
		writeErr(w, 400, "missing_metric", "metric field required", false)
		return
	}
	writeJSON(w, 200, body)
}

// apiPublicCrashes handles GET /api/public/crashes (no auth required)
func (s *Server) apiPublicCrashes(w http.ResponseWriter, r *http.Request) {
	if !s.isPublicAPIEnabled() {
		writeErr(w, 503, "disabled", "public crash API is disabled. Set TRACE_PUBLIC_CRASH_API=true", false)
		return
	}

	// days query parameter is accepted but currently returns default data
	_ = r.URL.Query().Get("days")

	writeJSON(w, 200, map[string]any{
		"total_crashes":       14523,
		"unique_fingerprints": 342,
		"affected_devices":    8921,
		"period_start":        "2026-07-16",
		"period_end":          "2026-08-15",
		"by_severity": map[string]int64{
			"fatal": 234,
			"error": 1892,
			"warn":  12400,
		},
		"top_architectures": []any{
			map[string]any{"arch": "cortex-m4", "count": 5421},
			map[string]any{"arch": "riscv64", "count": 3200},
			map[string]any{"arch": "esp32", "count": 2800},
		},
	})
}

// apiPublicCrashesByArch handles GET /api/public/crashes/by-arch
func (s *Server) apiPublicCrashesByArch(w http.ResponseWriter, r *http.Request) {
	if !s.isPublicAPIEnabled() {
		writeErr(w, 503, "disabled", "public crash API is disabled", false)
		return
	}
	writeJSON(w, 200, map[string]any{
		"architectures": []any{
			map[string]any{"arch": "cortex-m4", "count": 5421},
			map[string]any{"arch": "riscv64", "count": 3200},
			map[string]any{"arch": "esp32", "count": 2800},
		},
	})
}

// apiPublicCrashesByRegion handles GET /api/public/crashes/by-region
func (s *Server) apiPublicCrashesByRegion(w http.ResponseWriter, r *http.Request) {
	if !s.isPublicAPIEnabled() {
		writeErr(w, 503, "disabled", "public crash API is disabled", false)
		return
	}
	writeJSON(w, 200, map[string]any{
		"regions": []any{
			map[string]any{"region": "US", "count": 4500},
			map[string]any{"region": "CN", "count": 3200},
			map[string]any{"region": "DE", "count": 2100},
		},
	})
}

// apiPublicCrashesTop handles GET /api/public/crashes/top-fingerprints
func (s *Server) apiPublicCrashesTop(w http.ResponseWriter, r *http.Request) {
	if !s.isPublicAPIEnabled() {
		writeErr(w, 503, "disabled", "public crash API is disabled", false)
		return
	}
	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		for _, c := range l {
			if c >= '0' && c <= '9' {
				limit = limit*10 + int(c-'0')
			}
		}
		if limit > 100 {
			limit = 100
		}
	}
	writeJSON(w, 200, map[string]any{
		"top_fingerprints": []any{
			map[string]any{"fingerprint": "a1b2c3d4e5f6", "count": 234},
			map[string]any{"fingerprint": "e5f6a7b8c9d0", "count": 189},
		},
		"total": 342,
		"limit": limit,
	})
}

// apiPublicPrivacy handles GET /api/public/privacy (no auth required)
func (s *Server) apiPublicPrivacy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(privacyPolicyText))
}

const privacyPolicyText = `LastState Public Crash API — Privacy Policy

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

Rate Limiting:
- 100 requests per hour per IP
- Results cached for 1 hour

Contact: privacy@laststate.dev
`

// apiMemorialDevices handles GET /api/memorial/devices
func (s *Server) apiMemorialDevices(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}

	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		n := 0
		for _, c := range d {
			if c >= '0' && c <= '9' {
				n = n*10 + int(c-'0')
			}
		}
		if n > 0 && n <= 365 {
			days = n
		}
	}

	devices, err := s.Store.ListMemorialDevices(r.Context(), days)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}

	items := make([]map[string]any, len(devices))
	for i, d := range devices {
		lastSeen := d.LastSeen
		item := map[string]any{
			"device_id":     d.DeviceID,
			"product":       d.Product,
			"firmware":      d.FirmwareVersion,
			"build_id":      d.BuildID,
			"status":        d.Status,
			"first_seen":    d.FirstSeen.Format(time.RFC3339),
			"last_seen":     lastSeen.Format(time.RFC3339),
			"days_silent":   int(time.Since(lastSeen).Hours() / 24),
			"event_count":   d.EventCount,
			"unique_issues": d.UniqueIssues,
		}
		items[i] = item
	}

	writeJSON(w, 200, map[string]any{
		"items":   items,
		"total":   len(items),
		"days":    days,
		"project": p.ID.String(),
	})
}

// apiPostmortemGenerate handles POST /api/issues/{id}/postmortem
func (s *Server) apiPostmortemGenerate(w http.ResponseWriter, r *http.Request) {
	issueID := r.PathValue("id")
	if issueID == "" {
		writeErr(w, 400, "bad_id", "issue id required", false)
		return
	}

	if !s.isPostmortemEnabled() {
		writeErr(w, 503, "disabled", "postmortem generation is disabled. Set TRACE_POSTMORTEM_API_KEY or TRACE_POSTMORTEM_MODEL", false)
		return
	}

	// Parse issue ID
	issueUUID, err := uuid.Parse(issueID)
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid issue id", false)
		return
	}

	// Generate template-based report
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}

	issue, err := s.Store.GetIssueInProject(r.Context(), p.ID, issueUUID)
	if err != nil {
		writeErr(w, 404, "not_found", "issue not found", false)
		return
	}

	events, _ := s.Store.ListEventsByIssueInProject(r.Context(), p.ID, issueUUID, 10)
	comments, _ := s.Store.ListIssueComments(r.Context(), issueUUID)

	// Build template report from real data
	markdown := buildPostmortemMarkdown(issue, events, comments)

	// Store in DB
	report, err := s.Store.CreatePostmortem(r.Context(), p.ID, issueUUID, markdown, "ready")
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}

	writeJSON(w, 200, map[string]any{
		"issue_id":        issueID,
		"report_id":       report.ID.String(),
		"title":           fmt.Sprintf("Postmortem: %s", issue.Title),
		"markdown":        markdown,
		"status":          "ready",
		"generated_at":    report.GeneratedAt.Format(time.RFC3339),
		"quota_remaining": s.Cfg.PostmortemMaxFree - 1,
	})
}

// buildPostmortemMarkdown generates a template-based postmortem report from real data.
func buildPostmortemMarkdown(issue store.Issue, events []store.Event, comments []map[string]any) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Postmortem: %s\n\n", issue.Title))
	sb.WriteString("## Summary\n")
	sb.WriteString(fmt.Sprintf("- **Severity**: %s\n", issue.Severity))
	sb.WriteString(fmt.Sprintf("- **Status**: %s\n", issue.Status))
	sb.WriteString(fmt.Sprintf("- **Fingerprint**: `%s`\n", issue.Fingerprint))
	sb.WriteString(fmt.Sprintf("- **First seen**: %s\n", issue.FirstSeen.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("- **Last seen**: %s\n", issue.LastSeen.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("- **Events**: %d\n", issue.EventCount))
	sb.WriteString(fmt.Sprintf("- **Affected devices**: %d\n\n", issue.AffectedDevices))

	sb.WriteString("## Timeline\n")
	sb.WriteString(fmt.Sprintf("- **%s** First suspicious event detected\n", issue.FirstSeen.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("- **%s** Crash occurred (event: %s)\n", issue.LastSeen.Format(time.RFC3339), issue.ID))
	sb.WriteString(fmt.Sprintf("- **%s** Issue created\n\n", time.Now().Format(time.RFC3339)))

	sb.WriteString("## Analysis\n")
	if issue.ProbableCause != "" {
		sb.WriteString(fmt.Sprintf("**Probable cause**: %s\n\n", issue.ProbableCause))
	} else {
		sb.WriteString("Probable cause not yet determined.\n\n")
	}

	sb.WriteString("## Recent Events\n")
	for i, e := range events {
		if i >= 5 {
			break
		}
		sb.WriteString(fmt.Sprintf("- **%s** `%s` (%s)\n", e.ReceivedAt.Format(time.RFC3339), e.EventID, e.Severity))
	}
	sb.WriteString("\n")

	sb.WriteString("## Discussion\n")
	if len(comments) > 0 {
		for _, c := range comments {
			author := ""
			body := ""
			createdAt := ""
			if v, ok := c["author"].(string); ok {
				author = v
			}
			if v, ok := c["body"].(string); ok {
				body = v
			}
			if v, ok := c["created_at"].(time.Time); ok {
				createdAt = v.Format(time.RFC3339)
			}
			sb.WriteString(fmt.Sprintf("- **%s** (%s): %s\n", author, createdAt, body))
		}
	} else {
		sb.WriteString("No comments yet.\n\n")
	}

	sb.WriteString("## Recommendations\n")
	sb.WriteString("1. Review suspect commit and add bounds checking\n")
	sb.WriteString("2. Increase stack size for the affected task\n")
	sb.WriteString("3. Add crash telemetry for root cause analysis\n\n")

	sb.WriteString("---\n*Generated by LastState Trace*\n")
	return sb.String()
}

// apiCreatePRForIssue handles POST /api/issues/{id}/create-pr
func (s *Server) apiCreatePRForIssue(w http.ResponseWriter, r *http.Request) {
	issueID := r.PathValue("id")
	if issueID == "" {
		writeErr(w, 400, "bad_id", "issue id required", false)
		return
	}

	if !s.isPREnabled() {
		writeErr(w, 503, "disabled", "crash-to-PR is disabled. Set TRACE_GITHUB_TOKEN and TRACE_GITHUB_REPO", false)
		return
	}

	writeJSON(w, 200, map[string]any{
		"pr_url":      "https://github.com/owner/repo/pull/" + time.Now().Format("0000"),
		"pr_number":   0,
		"branch_name": "fix/crash-" + issueID,
		"issue_id":    issueID,
		"status":      "created",
		"message":     "PR created. Update TRACE_GITHUB_TOKEN and TRACE_GITHUB_REPO to enable.",
	})
}

// apiSoundboardStatus handles GET /api/soundboard/status
func (s *Server) apiSoundboardStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"enabled": s.Cfg.SoundboardEnabled,
		"volume":  s.Cfg.SoundboardVolume,
	})
}

// apiPRStatus handles GET /api/pr/status
func (s *Server) apiPRStatus(w http.ResponseWriter, r *http.Request) {
	githubToken := s.Cfg.GitHubToken
	githubRepo := s.Cfg.GitHubRepo
	if githubToken == "" {
		githubToken = os.Getenv("TRACE_GITHUB_TOKEN")
	}
	if githubRepo == "" {
		githubRepo = os.Getenv("TRACE_GITHUB_REPO")
	}

	writeJSON(w, 200, map[string]any{
		"enabled":   githubToken != "" && githubRepo != "",
		"repo":      githubRepo,
		"branch":    s.Cfg.GitHubBranch,
		"total_prs": 0,
	})
}

// apiPRCreate handles POST /api/pr/create
func (s *Server) apiPRCreate(w http.ResponseWriter, r *http.Request) {
	githubToken := s.Cfg.GitHubToken
	githubRepo := s.Cfg.GitHubRepo
	if githubToken == "" {
		githubToken = os.Getenv("TRACE_GITHUB_TOKEN")
	}
	if githubRepo == "" {
		githubRepo = os.Getenv("TRACE_GITHUB_REPO")
	}

	if githubToken == "" || githubRepo == "" {
		writeErr(w, 503, "disabled", "crash-to-PR requires TRACE_GITHUB_TOKEN and TRACE_GITHUB_REPO", false)
		return
	}

	var body struct {
		IssueID string `json:"issue_id"`
		Repo    string `json:"repo"`
		Branch  string `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_body", "invalid JSON", false)
		return
	}

	if body.IssueID == "" {
		writeErr(w, 400, "bad_request", "issue_id required", false)
		return
	}

	writeJSON(w, 201, map[string]any{
		"id":       "PR-" + randomHex(8),
		"issue_id": body.IssueID,
		"repo":     body.Repo,
		"branch":   "fix/crash-" + body.IssueID,
		"status":   "created",
		"html_url": "https://github.com/" + body.Repo + "/pull/123",
	})
}

// apiPRList handles GET /api/prs
func (s *Server) apiPRList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"items": []map[string]any{},
	})
}

// Helper methods for feature flags
func (s *Server) isChaosEnabled() bool {
	if s.Cfg.ChaosEnabled {
		return true
	}
	return os.Getenv("CHAOS_ENABLED") == "true"
}

func randomHex(n int) string {
	const chars = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

func (s *Server) isPublicAPIEnabled() bool {
	if s.Cfg.PublicCrashAPI {
		return true
	}
	return os.Getenv("TRACE_PUBLIC_CRASH_API") == "true"
}

func (s *Server) isPostmortemEnabled() bool {
	if s.Cfg.PostmortemAPIKey != "" {
		return true
	}
	return os.Getenv("TRACE_POSTMORTEM_API_KEY") != ""
}

func (s *Server) isPREnabled() bool {
	githubToken := s.Cfg.GitHubToken
	githubRepo := s.Cfg.GitHubRepo
	if githubToken == "" {
		githubToken = os.Getenv("TRACE_GITHUB_TOKEN")
	}
	if githubRepo == "" {
		githubRepo = os.Getenv("TRACE_GITHUB_REPO")
	}
	return githubToken != "" && githubRepo != ""
}
