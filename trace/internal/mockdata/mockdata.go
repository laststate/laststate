// Package mockdata provides realistic sample data for the Trace web UI
// preview/development mode. No database, S3, or queue is required — everything
// is in-memory and generated once at startup.
//
// To use: set TRACE_MOCK=true when launching the server. The API handlers
// return these payloads instead of hitting the store.
package mockdata

import (
	"math/rand"
	"time"
)

// now returns a deterministic "now" anchored to 2026-08-15 so charts render
// consistently across restarts.
func now() time.Time {
	return time.Date(2026, 8, 15, 10, 30, 0, 0, time.UTC)
}

// Overview returns a full dashboard payload matching the shape consumed by
// the React overview component.
func Overview() map[string]any {
	n := now()

	eventsHourly := make([]map[string]any, 24)
	severityHourly := make([]map[string]any, 24)
	exceptionHourly := make([]map[string]any, 24)
	for i := 0; i < 24; i++ {
		h := n.Add(-time.Duration(23-i) * time.Hour)
		ts := h.Format(time.RFC3339)
		count := baseInt(30, 120)
		eventsHourly[i] = map[string]any{"hour": ts, "count": count}
		severityHourly[i] = map[string]any{
			"hour": ts,
			"ok":   baseInt(count*80/100, count*90/100),
			"warn": baseInt(count*5/100, count*12/100),
			"err":  baseInt(count*3/100, count*7/100),
		}
		exceptionHourly[i] = map[string]any{
			"hour":      ts,
			"handled":   baseInt(count*60/100, count*75/100),
			"unhandled": baseInt(count*5/100, count*12/100),
		}
	}

	// Generate trend data for 14 days
	eventsTrend := make([]map[string]any, 14)
	fatalTrend := make([]map[string]any, 14)
	issuesTrend := make([]map[string]any, 14)
	for i := 0; i < 14; i++ {
		d := n.Add(-time.Duration(13-i) * 24 * time.Hour).Format("2006-01-02")
		c := baseInt(200, 600)
		eventsTrend[i] = map[string]any{"date": d, "count": c}
		ft := baseInt(c*2/100, c*5/100)
		fatalTrend[i] = map[string]any{"date": d, "count": ft}
		it := baseInt(c/100, c/30)
		issuesTrend[i] = map[string]any{"date": d, "count": it}
	}

	bySeverity := []map[string]any{
		{"severity": "ok", "count": 4521},
		{"severity": "warn", "count": 892},
		{"severity": "err", "count": 312},
		{"severity": "fatal", "count": 47},
	}
	byArch := []map[string]any{
		{"name": "x86_64", "count": 3201},
		{"name": "arm64", "count": 1892},
		{"name": "riscv64", "count": 420},
		{"name": "mipsel", "count": 180},
	}
	byPipeline := []map[string]any{
		{"pipeline": "issue", "count": 2847},
		{"pipeline": "health", "count": 1930},
		{"pipeline": "log", "count": 1240},
		{"pipeline": "metric", "count": 620},
		{"pipeline": "boot", "count": 210},
	}

	// Top issues
	topIssues := []map[string]any{
		{
			"id": "EVT-a1b2c3d4e5f6", "title": "Null pointer dereference in sensor_driver",
			"severity": "fatal", "status": "open",
			"fingerprint": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
			"event_count": 142, "affected_devices": 23,
			"last_seen": n.Add(-2 * time.Hour).Format(time.RFC3339),
		},
		{
			"id": "EVT-f6e5d4c3b2a1", "title": "Watchdog timeout on CAN bus module",
			"severity": "fatal", "status": "investigating",
			"fingerprint": "f6e5d4c3b2a1f6e5d4c3b2a1f6e5d4c3",
			"event_count": 87, "affected_devices": 12,
			"last_seen": n.Add(-45 * time.Minute).Format(time.RFC3339),
		},
		{
			"id": "EVT-112233445566", "title": "Stack overflow in BLE stack handler",
			"severity": "error", "status": "open",
			"fingerprint": "11223344556611223344556611223344",
			"event_count": 64, "affected_devices": 8,
			"last_seen": n.Add(-3 * time.Hour).Format(time.RFC3339),
		},
		{
			"id": "EVT-aabbccddeeff", "title": "I2C bus deadlock on PMIC",
			"severity": "error", "status": "investigating",
			"fingerprint": "aabbccddeeffaabbccddeeffaabbccdd",
			"event_count": 41, "affected_devices": 6,
			"last_seen": n.Add(-6 * time.Hour).Format(time.RFC3339),
		},
		{
			"id": "EVT-998877665544", "title": "Memory leak in TCP stack (reconnect loop)",
			"severity": "warn", "status": "open",
			"fingerprint": "99887766554499887766554499887766",
			"event_count": 128, "affected_devices": 31,
			"last_seen": n.Add(-1 * time.Hour).Format(time.RFC3339),
		},
	}

	// Seed alert history
	seedAlerts()

	return map[string]any{
		"project":           map[string]any{"name": "Fleet Alpha", "slug": "fleet-alpha"},
		"open_issues":       18,
		"resolved_issues":   342,
		"events_total":      7409,
		"events_today":      612,
		"fatal_open":        7,
		"devices":           156,
		"unhealthy_devices": 12,
		"regressions":       3,
		"events_hourly":     eventsHourly,
		"events_trend":      eventsTrend,
		"fatal_trend":       fatalTrend,
		"issues_trend":      issuesTrend,
		"severity_24h":      map[string]any{"ok": 4521, "warn": 892, "err": 312},
		"severity_hourly":   severityHourly,
		"exception_hourly":  exceptionHourly,
		"by_severity":       bySeverity,
		"by_architecture":   byArch,
		"by_pipeline":       byPipeline,
		"top_issues":        topIssues,
	}
}

// IssuesPage returns a paginated list of issues.
func IssuesPage(offset, limit int) map[string]any {
	issues := []map[string]any{
		{"id": "EVT-a1b2c3d4e5f6", "title": "Null pointer dereference in sensor_driver", "severity": "fatal", "status": "open", "fingerprint": "a1b2c3d4e5f6a1b2c3d4", "event_count": 142, "affected_devices": 23, "last_seen": "2026-08-15T08:30:00Z", "first_seen": "2026-08-12T14:22:00Z", "regression_count": 2},
		{"id": "EVT-f6e5d4c3b2a1", "title": "Watchdog timeout on CAN bus module", "severity": "fatal", "status": "investigating", "fingerprint": "f6e5d4c3b2a1f6e5d4c3", "event_count": 87, "affected_devices": 12, "last_seen": "2026-08-15T09:45:00Z", "first_seen": "2026-08-13T06:11:00Z", "regression_count": 0},
		{"id": "EVT-112233445566", "title": "Stack overflow in BLE stack handler", "severity": "error", "status": "open", "fingerprint": "11223344556611223344", "event_count": 64, "affected_devices": 8, "last_seen": "2026-08-15T07:15:00Z", "first_seen": "2026-08-14T22:05:00Z", "regression_count": 1},
		{"id": "EVT-aabbccddeeff", "title": "I2C bus deadlock on PMIC", "severity": "error", "status": "investigating", "fingerprint": "aabbccddeeffaabbccdd", "event_count": 41, "affected_devices": 6, "last_seen": "2026-08-15T04:30:00Z", "first_seen": "2026-08-11T18:45:00Z", "regression_count": 0},
		{"id": "EVT-998877665544", "title": "Memory leak in TCP stack (reconnect loop)", "severity": "warn", "status": "open", "fingerprint": "99887766554499887766", "event_count": 128, "affected_devices": 31, "last_seen": "2026-08-15T09:10:00Z", "first_seen": "2026-08-10T11:00:00Z", "regression_count": 4},
		{"id": "EVT-1234abcd5678", "title": "ADC sampling jitter above threshold", "severity": "warn", "status": "open", "fingerprint": "1234abcd56781234abcd", "event_count": 55, "affected_devices": 15, "last_seen": "2026-08-15T08:00:00Z", "first_seen": "2026-08-13T09:30:00Z", "regression_count": 0},
		{"id": "EVT-feedface0000", "title": "Flash write corruption on sector 7", "severity": "fatal", "status": "ignored", "fingerprint": "feedface0000feedface", "event_count": 19, "affected_devices": 3, "last_seen": "2026-08-14T16:20:00Z", "first_seen": "2026-08-14T16:20:00Z", "regression_count": 0},
		{"id": "EVT-deadbeef1234", "title": "SPI clock skew exceeds spec on expansion board", "severity": "warn", "status": "resolved", "fingerprint": "deadbeef1234deadbeef", "event_count": 22, "affected_devices": 4, "last_seen": "2026-08-13T21:00:00Z", "first_seen": "2026-08-12T08:15:00Z", "regression_count": 0},
	}
	total := len(issues)
	start := offset * limit
	if start >= total {
		start = total - 1
		if start < 0 {
			start = 0
		}
	}
	end := start + limit
	if end > total {
		end = total
	}
	return map[string]any{"items": issues[start:end], "total": total}
}

// IssueDetail returns a full issue view with events, comments, activity.
func IssueDetail(id string) map[string]any {
	paged := IssuesPage(0, 10)
	items := paged["items"].([]map[string]any)
	i := items[0]
	for _, issue := range items {
		if issue["id"] == id {
			i = issue
			break
		}
	}
	n := now()
	idPart := func(s string) string {
		if len(s) < 8 {
			return s
		}
		return s[4:8]
	}
	events := []map[string]any{
		{
			"id": "EVT-" + idPart(id) + "a", "event_id": "evt_" + idPart(id) + "a1b2c3d4", "severity": i["severity"],
			"state": "processed", "pipeline": "issue",
			"frames":      `[{"function":"sensor_read","file":"sensor_driver.c","line":142,"address":4198720},{"function":"main_loop","file":"main.c","line":88,"address":4197440}]`,
			"analysis":    `{"summary":"Dereference at sensor_driver.c:142 during initialization","architecture_name":"arm64","fault_details":{"pc":"0x00401280","sp":"0x2001FF00"},"unwind_method":"dwarf"}`,
			"received_at": n.Add(-2 * time.Hour).Format(time.RFC3339),
		},
		{
			"id": "EVT-" + idPart(id) + "b", "event_id": "evt_" + idPart(id) + "b2c3d4e5", "severity": i["severity"],
			"state": "processed", "pipeline": "issue",
			"frames":      `[{"function":"sensor_read","file":"sensor_driver.c","line":142,"address":4198720},{"function":"process_sample","file":"pipeline.c","line":201,"address":4199100}]`,
			"analysis":    `{"summary":"Same fingerprint","architecture_name":"arm64","unwind_method":"dwarf"}`,
			"received_at": n.Add(-5 * time.Hour).Format(time.RFC3339),
		},
		{
			"id": "EVT-" + idPart(id) + "c", "event_id": "evt_" + idPart(id) + "c3d4e5f6", "severity": i["severity"],
			"state": "processed", "pipeline": "issue",
			"frames":      `[{"function":"sensor_init","file":"sensor_driver.c","line":87,"address":4198560}]`,
			"analysis":    `{"summary":"First occurrence","architecture_name":"x86_64","unwind_method":"dwarf"}`,
			"received_at": n.Add(-72 * time.Hour).Format(time.RFC3339),
		},
	}
	comments := []map[string]any{
		{"id": "c1", "author": "alice@fleet.io", "body": "Looks like a race condition in the init path — adding a mutex."},
		{"id": "c2", "author": "bob@fleet.io", "body": "Confirmed on hardware rev C. Reproducible with cold boot + rapid sensor poll."},
	}
	activity := []map[string]any{
		{"id": "a1", "created_at": n.Add(-2 * time.Hour).Format(time.RFC3339), "action": "status_changed", "body": "→ investigating"},
		{"id": "a2", "created_at": n.Add(-48 * time.Hour).Format(time.RFC3339), "action": "assigned", "body": "alice@fleet.io"},
		{"id": "a3", "created_at": n.Add(-72 * time.Hour).Format(time.RFC3339), "action": "created", "body": "first seen"},
	}
	return map[string]any{"issue": i, "events": events, "comments": comments, "activity": activity}
}

// EventsPage returns a paginated events list.
func EventsPage(offset, limit int) map[string]any {
	items := make([]map[string]any, 25)
	n := now()
	for i := 0; i < 25; i++ {
		ev := n.Add(-time.Duration(i) * 11 * time.Minute)
		sev := []string{"ok", "warn", "err", "fatal"}[rand.Intn(4)]
		items[i] = map[string]any{
			"id":          "EVT-" + fmtHex(i, 12),
			"event_id":    "evt_" + fmtHex(i, 12) + "abcdef01",
			"severity":    sev,
			"state":       "processed",
			"pipeline":    "issue",
			"received_at": ev.Format(time.RFC3339),
		}
	}
	return map[string]any{"items": items, "total": 7409}
}

// DevicesPage returns a paginated devices list.
func DevicesPage(offset, limit int) map[string]any {
	items := make([]map[string]any, 12)
	n := now()
	for i := 0; i < 12; i++ {
		items[i] = map[string]any{
			"id":               "DEV-" + fmtHex(i, 3),
			"device_id":        "FW-" + fmtHex(i, 6) + "-" + fmtHex(rand.Intn(9999), 4),
			"status":           []string{"online", "offline", "degraded", "unknown"}[rand.Intn(4)],
			"product":          []string{"Fleet Node A", "Fleet Node B", "Edge Gateway", "Sensor Pod"}[rand.Intn(4)],
			"firmware_version": "v" + fmtHex(rand.Intn(3)+1, 1) + "." + fmtHex(rand.Intn(8), 1) + "." + fmtHex(rand.Intn(12), 2),
			"last_seen":        n.Add(-time.Duration(rand.Intn(360)) * time.Minute).Format(time.RFC3339),
		}
	}
	return map[string]any{"items": items, "total": 156}
}

// Releases returns a list of releases.
func Releases() map[string]any {
	n := now()
	items := []map[string]any{
		{"id": "REL-001", "version": "v1.8.11", "build_id": "build-2026081501", "git_commit": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2", "status": "stable", "created_at": n.Add(-1 * time.Hour).Format(time.RFC3339)},
		{"id": "REL-002", "version": "v1.8.10", "build_id": "build-2026081401", "git_commit": "b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3", "status": "stable", "created_at": n.Add(-48 * time.Hour).Format(time.RFC3339)},
		{"id": "REL-003", "version": "v1.8.9", "build_id": "build-2026081201", "git_commit": "c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4", "status": "stable", "created_at": n.Add(-72 * time.Hour).Format(time.RFC3339)},
		{"id": "REL-004", "version": "v1.8.8", "build_id": "build-2026081001", "git_commit": "d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5", "status": "deprecated", "created_at": n.Add(-120 * time.Hour).Format(time.RFC3339)},
		{"id": "REL-005", "version": "v1.9.0-rc1", "build_id": "build-2026081509", "git_commit": "e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6", "status": "prerelease", "created_at": n.Add(-30 * time.Minute).Format(time.RFC3339)},
	}
	return map[string]any{"items": items}
}

// ReleaseStats returns crash stats for a release.
func ReleaseStats(id string) map[string]any {
	return map[string]any{
		"crash_free_rate": 97.3 + float64(rand.Intn(20))/100.0,
		"sessions":        2400 + rand.Intn(600),
		"crash_sessions":  30 + rand.Intn(80),
	}
}

// Artifacts returns a list of build artifacts.
func Artifacts() map[string]any {
	items := []map[string]any{
		{"id": "ART-001", "build_id": "build-2026081501", "architecture": "arm64", "status": "ready", "sha256": fmtHex(0xa1b2c3d4, 8) + fmtHex(0xe5f6a1b2, 8)},
		{"id": "ART-002", "build_id": "build-2026081501", "architecture": "x86_64", "status": "ready", "sha256": fmtHex(0xc3d4e5f6, 8) + fmtHex(0xa1b2c3d4, 8)},
		{"id": "ART-003", "build_id": "build-2026081401", "architecture": "arm64", "status": "ready", "sha256": fmtHex(0xd4e5f6a1, 8) + fmtHex(0xb2c3d4e5, 8)},
		{"id": "ART-004", "build_id": "build-2026081401", "architecture": "riscv64", "status": "quarantine", "sha256": fmtHex(0xe5f6a1b2, 8) + fmtHex(0xc3d4e5f6, 8)},
		{"id": "ART-005", "build_id": "build-2026081201", "architecture": "arm64", "status": "ready", "sha256": fmtHex(0xf6a1b2c3, 8) + fmtHex(0xd4e5f6a1, 8)},
	}
	return map[string]any{"items": items}
}

// Alerts returns a list of alert rules.
func Alerts() map[string]any {
	items := []map[string]any{
		{"id": "ALT-001", "name": "Fatal error rate", "kind": "threshold", "channel": "slack", "cooldown_sec": 300},
		{"id": "ALT-002", "name": "Device offline > 1h", "kind": "threshold", "channel": "slack", "cooldown_sec": 3600},
		{"id": "ALT-003", "name": "New fatal fingerprint", "kind": "fingerprint", "channel": "pagerduty", "cooldown_sec": 0},
		{"id": "ALT-004", "name": "Regression detected", "kind": "regression", "channel": "slack", "cooldown_sec": 600},
	}
	return map[string]any{"items": items}
}

// Channels returns a list of notification channels.
func Channels() map[string]any {
	items := []map[string]any{
		{"id": "CH-001", "name": "Fleet Alpha Slack", "kind": "slack", "enabled": true},
		{"id": "CH-002", "name": "PagerDuty OnCall", "kind": "webhook", "enabled": true},
		{"id": "CH-003", "name": "Dev Discord", "kind": "discord", "enabled": false},
	}
	return map[string]any{"items": items}
}

// Relays returns a list of relays.
func Relays() map[string]any {
	items := []map[string]any{
		{"id": "RLY-001", "relay_id": "relay-alpha-01", "version": "2.4.1", "status": "online", "last_heartbeat": now().Add(-2 * time.Minute).Format(time.RFC3339)},
		{"id": "RLY-002", "relay_id": "relay-alpha-02", "version": "2.4.1", "status": "online", "last_heartbeat": now().Add(-5 * time.Minute).Format(time.RFC3339)},
		{"id": "RLY-003", "relay_id": "relay-beta-01", "version": "2.3.8", "status": "degraded", "last_heartbeat": now().Add(-25 * time.Minute).Format(time.RFC3339)},
		{"id": "RLY-004", "relay_id": "relay-edge-01", "version": "2.4.0", "status": "offline", "last_heartbeat": now().Add(-4 * time.Hour).Format(time.RFC3339)},
	}
	return map[string]any{"items": items}
}

// Projects returns a list of projects.
func Projects() map[string]any {
	items := []map[string]any{
		{"id": "PRJ-001", "name": "Fleet Alpha", "slug": "fleet-alpha", "description": "Primary production fleet"},
		{"id": "PRJ-002", "name": "Fleet Beta", "slug": "fleet-beta", "description": "Secondary fleet for Region EU"},
		{"id": "PRJ-003", "name": "Lab Environment", "slug": "lab", "description": "Internal test/dev lab"},
	}
	return map[string]any{"items": items}
}

// Hardware returns hardware revision data.
func Hardware() map[string]any {
	items := []map[string]any{
		{"id": "HW-001", "revision": "A1", "fatal_events": 12, "events": 842},
		{"id": "HW-002", "revision": "A2", "fatal_events": 8, "events": 1203},
		{"id": "HW-003", "revision": "B1", "fatal_events": 3, "events": 2100},
		{"id": "HW-004", "revision": "B2", "fatal_events": 1, "events": 3400},
	}
	return map[string]any{"items": items}
}

// BootSessions returns boot session history.
func BootSessions(offset, limit int) map[string]any {
	items := make([]map[string]any, 15)
	n := now()
	for i := 0; i < 15; i++ {
		items[i] = map[string]any{
			"id":            "BOOT-" + fmtHex(i, 3),
			"boot_id":       "boot-" + fmtHex(i, 8),
			"device_id":     "FW-" + fmtHex(i, 6) + "-" + fmtHex(rand.Intn(9999), 4),
			"event_count":   5 + rand.Intn(20),
			"last_event_at": n.Add(-time.Duration(i) * 90 * time.Minute).Format(time.RFC3339),
		}
	}
	return map[string]any{"items": items, "total": 487}
}

// DeadJobs returns failed jobs.
func DeadJobs() map[string]any {
	items := []map[string]any{
		{"id": "JOB-001", "type": "process_event", "attempts": 5, "last_error": "object store timeout", "created_at": now().Add(-6 * time.Hour).Format(time.RFC3339)},
		{"id": "JOB-002", "type": "notify_issue", "attempts": 3, "last_error": "slack webhook 502", "created_at": now().Add(-3 * time.Hour).Format(time.RFC3339)},
		{"id": "JOB-003", "type": "reprocess_event", "attempts": 5, "last_error": "symbolicator unreachable", "created_at": now().Add(-1 * time.Hour).Format(time.RFC3339)},
	}
	return map[string]any{"items": items, "total": 3}
}

// Audit returns audit log entries.
func Audit(offset, limit int) map[string]any {
	n := now()
	items := []map[string]any{
		{"id": "AUD-001", "created_at": n.Add(-5 * time.Minute).Format(time.RFC3339), "actor": "alice@fleet.io", "action": "issue.status", "target_type": "issue", "target_id": "EVT-a1b2c3d4e5f6"},
		{"id": "AUD-002", "created_at": n.Add(-1 * time.Hour).Format(time.RFC3339), "actor": "bob@fleet.io", "action": "artifact.upload", "target_type": "artifact", "target_id": "ART-005"},
		{"id": "AUD-003", "created_at": n.Add(-3 * time.Hour).Format(time.RFC3339), "actor": "system", "action": "issue.created", "target_type": "issue", "target_id": "EVT-deadbeef1234"},
		{"id": "AUD-004", "created_at": n.Add(-6 * time.Hour).Format(time.RFC3339), "actor": "alice@fleet.io", "action": "project.update", "target_type": "project", "target_id": "PRJ-001"},
		{"id": "AUD-005", "created_at": n.Add(-12 * time.Hour).Format(time.RFC3339), "actor": "system", "action": "relay.heartbeat", "target_type": "relay", "target_id": "RLY-001"},
	}
	return map[string]any{"items": items, "total": 1842}
}

// Bootstrap returns bootstrap info for the UI settings page.
func Bootstrap() map[string]any {
	return map[string]any{
		"bootstrapped":    true,
		"open_ui":         true,
		"deployment":      "local",
		"billing_enabled": false,
		"project":         map[string]any{"name": "Fleet Alpha", "slug": "fleet-alpha"},
	}
}

// Me returns the current user info (auto-logged-in in mock mode).
func Me() map[string]any {
	return map[string]any{
		"id":              "USR-001",
		"email":           "admin@fleet.io",
		"name":            "Alice Dev",
		"role":            "admin",
		"organization_id": "ORG-001",
	}
}

// Login returns a mock session token.
func Login(email, password string) map[string]any {
	return map[string]any{
		"token": "mock-session-token-abc123def456",
		"user":  Me(),
	}
}

// Search returns mock search results.
func Search(q string) map[string]any {
	return map[string]any{
		"issues":    []map[string]any{{"id": "EVT-a1b2c3d4e5f6", "title": "Null pointer dereference in sensor_driver"}},
		"events":    []map[string]any{{"id": "EVT-a1b2c3d4e5f6a", "event_id": "evt_a1b2c3d4a1b2c3d4"}},
		"devices":   []map[string]any{{"id": "DEV-001", "device_id": "FW-00a1b2-3c4d"}},
		"artifacts": []map[string]any{{"id": "ART-001", "build_id": "build-2026081501"}},
	}
}

// ---- helpers ----

func baseInt(lo, hi int) int {
	return lo + rand.Intn(hi-lo+1)
}

func fmtHex(v int, n int) string {
	hex := "0123456789abcdef"
	b := make([]byte, n)
	for i := n - 1; i >= 0; i-- {
		b[i] = hex[v&0xf]
		v >>= 4
	}
	return string(b)
}

// FleetHealth returns fleet health summary data.
func FleetHealth() map[string]any {
	n := now()
	avgScore := 72.3
	healthyCount := 98
	degradedCount := 42
	criticalCount := 16

	topHealthy := []map[string]any{
		{"device_id": "FW-00a1b2-3c4d", "score": 98.5, "status": "healthy"},
		{"device_id": "FW-00b2c3-4d5e", "score": 97.2, "status": "healthy"},
		{"device_id": "FW-00c3d4-5e6f", "score": 96.8, "status": "healthy"},
		{"device_id": "FW-00d4e5-6f70", "score": 95.1, "status": "healthy"},
		{"device_id": "FW-00e5f6-7081", "score": 94.7, "status": "healthy"},
	}

	bottomDead := []map[string]any{
		{"device_id": "FW-00f6a1-8192", "score": 12.3, "status": "critical"},
		{"device_id": "FW-00a1b2-9203", "score": 18.7, "status": "critical"},
		{"device_id": "FW-00b2c3-0314", "score": 24.1, "status": "critical"},
		{"device_id": "FW-00c3d4-1425", "score": 28.9, "status": "degraded"},
		{"device_id": "FW-00d4e5-2536", "score": 31.2, "status": "degraded"},
	}

	trend := []map[string]any{}
	for i := 0; i < 14; i++ {
		d := n.Add(-time.Duration(13-i) * 24 * time.Hour).Format("2006-01-02")
		score := 60 + float64(rand.Intn(30))
		trend = append(trend, map[string]any{"date": d, "score": score})
	}

	return map[string]any{
		"average_score":  avgScore,
		"median_score":   71.5,
		"healthy_count":  healthyCount,
		"degraded_count": degradedCount,
		"critical_count": criticalCount,
		"top_healthy":    topHealthy,
		"bottom_dead":    bottomDead,
		"trend":          trend,
		"updated_at":     n.Format(time.RFC3339),
	}
}

// DeviceHealth returns health data for a single device.
func DeviceHealth(deviceID string) map[string]any {
	n := now()
	return map[string]any{
		"device_id":      deviceID,
		"score":          72.5,
		"crash_factor":   0.6,
		"uptime_factor":  0.85,
		"battery_factor": 0.9,
		"ota_factor":     0.95,
		"temp_factor":    0.8,
		"updated_at":     n.Format(time.RFC3339),
		"status":         "degraded",
	}
}

// MemorialDevices returns anonymized memorial wall data.
func MemorialDevices() map[string]any {
	items := []map[string]any{
		{"id": "MEM-001", "arch": "cortex-m4", "mfr": "ST", "batch": "B2026-A",
			"firmwareVersion": "v1.8.11", "firstSeen": "2026-01-15T10:00:00Z",
			"lastSeen": "2026-07-10T14:30:00Z", "lastCrash": "EVT-deadbeef",
			"daysSinceLastSeen": 36, "candlesLit": 3},
		{"id": "MEM-002", "arch": "esp32", "mfr": "Espressif", "batch": "B2026-B",
			"firmwareVersion": "v1.8.10", "firstSeen": "2026-02-01T08:00:00Z",
			"lastSeen": "2026-07-12T09:15:00Z", "lastCrash": "EVT-cafebabe",
			"daysSinceLastSeen": 34, "candlesLit": 7},
		{"id": "MEM-003", "arch": "riscv64", "mfr": "SiFive", "batch": "B2026-C",
			"firmwareVersion": "v1.7.5", "firstSeen": "2026-03-10T12:00:00Z",
			"lastSeen": "2026-07-14T16:45:00Z", "lastCrash": "EVT-fedcba98",
			"daysSinceLastSeen": 32, "candlesLit": 1},
		{"id": "MEM-004", "arch": "xtensa", "mfr": "Espressif", "batch": "B2026-D",
			"firmwareVersion": "v1.6.2", "firstSeen": "2025-11-20T06:00:00Z",
			"lastSeen": "2026-06-28T11:20:00Z", "lastCrash": "EVT-01234567",
			"daysSinceLastSeen": 48, "candlesLit": 12},
		{"id": "MEM-005", "arch": "cortex-m33", "mfr": "Nordic", "batch": "B2026-E",
			"firmwareVersion": "v2.0.1", "firstSeen": "2026-04-05T14:00:00Z",
			"lastSeen": "2026-07-08T08:00:00Z", "lastCrash": "EVT-89abcdef",
			"daysSinceLastSeen": 38, "candlesLit": 5},
	}
	return map[string]any{"items": items}
}

// PublicCrashes returns aggregate crash statistics.
func PublicCrashes(days int) map[string]any {
	n := now()
	startDate := n.Add(-time.Duration(days) * 24 * time.Hour).Format("2006-01-02")
	endDate := n.Format("2006-01-02")
	return map[string]any{
		"total_crashes":       14523,
		"unique_fingerprints": 342,
		"affected_devices":    8921,
		"period":              startDate + " to " + endDate,
		"by_severity": map[string]int{
			"fatal": 234,
			"error": 1892,
			"warn":  12400,
		},
		"top_architectures": []map[string]any{
			{"arch": "cortex-m4", "count": 5421},
			{"arch": "riscv64", "count": 3200},
			{"arch": "esp32", "count": 2800},
			{"arch": "cortex-m33", "count": 1800},
			{"arch": "xtensa", "count": 1302},
		},
	}
}

// PublicCrashesByArch returns crash counts by architecture.
func PublicCrashesByArch() map[string]any {
	return map[string]any{
		"items": []map[string]any{
			{"arch": "cortex-m4", "count": 5421},
			{"arch": "riscv64", "count": 3200},
			{"arch": "esp32", "count": 2800},
			{"arch": "cortex-m33", "count": 1800},
			{"arch": "xtensa", "count": 1302},
		},
	}
}

// PublicCrashesByRegion returns crash counts by region.
func PublicCrashesByRegion() map[string]any {
	return map[string]any{
		"items": []map[string]any{
			{"region": "US", "count": 4200},
			{"region": "CN", "count": 3100},
			{"region": "DE", "count": 2400},
			{"region": "JP", "count": 1800},
			{"region": "BR", "count": 1523},
			{"region": "IN", "count": 1200},
			{"region": "KR", "count": 800},
			{"region": "GB", "count": 700},
			{"region": "FR", "count": 500},
			{"region": "OTHER", "count": 800},
		},
	}
}

// DeviceDNA returns device DNA data.
func DeviceDNA() map[string]any {
	items := []map[string]any{
		{"device_id": "FW-00a1b2-3c4d", "mcu_uuid": "mcu-001-abc", "boot_time_avg": 45.2, "clock_freq": 64.0, "flash_wear": 12, "bootloader_sig": "sig-001", "fingerprint": "fp-001"},
		{"device_id": "FW-00b2c3-4d5e", "mcu_uuid": "mcu-002-def", "boot_time_avg": 44.8, "clock_freq": 64.0, "flash_wear": 8, "bootloader_sig": "sig-001", "fingerprint": "fp-002"},
		{"device_id": "FW-00c3d4-5e6f", "mcu_uuid": "mcu-003-ghi", "boot_time_avg": 46.1, "clock_freq": 63.9, "flash_wear": 15, "bootloader_sig": "sig-002", "fingerprint": "fp-003"},
	}
	return map[string]any{"items": items}
}

// Alert history seeded once at package init.
var alertHistory []map[string]any

func seedAlerts() {
	n := now()
	alertHistory = []map[string]any{
		{"id": "ALH-001", "title": "🚨 Novo Erro Fatal no Trace", "body": "1 novo(s) incidente(s) crítico(s) detectado(s).", "severity": "fatal", "timestamp": n.Add(-30 * time.Minute).Format(time.RFC3339), "read": false, "url": "/issues?severity=fatal"},
		{"id": "ALH-002", "title": "⚠️ Novo Incidente Registrado", "body": "Identificados novos problemas abertos (18 total).", "severity": "warning", "timestamp": n.Add(-2 * time.Hour).Format(time.RFC3339), "read": true, "url": "/issues"},
		{"id": "ALH-003", "title": "💀 Dead Job na Fila", "body": "Um job de background atingiu o limite máximo de tentativas.", "severity": "error", "timestamp": n.Add(-6 * time.Hour).Format(time.RFC3339), "read": true, "url": "/dead"},
	}
}
