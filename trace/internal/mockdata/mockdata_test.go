package mockdata

import (
	"testing"
)

func TestOverview(t *testing.T) {
	data := Overview()

	// Check required fields
	if data["open_issues"] == nil {
		t.Error("missing open_issues")
	}
	if data["resolved_issues"] == nil {
		t.Error("missing resolved_issues")
	}
	if data["events_total"] == nil {
		t.Error("missing events_total")
	}
	if data["devices"] == nil {
		t.Error("missing devices")
	}

	// Check trend data
	if trend, ok := data["events_trend"].([]map[string]any); !ok || len(trend) != 14 {
		t.Errorf("expected 14 days of events_trend, got %d", len(trend))
	}

	// Check hourly data
	if hourly, ok := data["events_hourly"].([]map[string]any); !ok || len(hourly) != 24 {
		t.Errorf("expected 24 hours of events_hourly, got %d", len(hourly))
	}

	// Check top issues
	if topIssues, ok := data["top_issues"].([]map[string]any); !ok || len(topIssues) == 0 {
		t.Error("expected non-empty top_issues")
	}
}

func TestIssuesPage(t *testing.T) {
	data := IssuesPage(0, 25)
	items := data["items"].([]map[string]any)
	if len(items) == 0 {
		t.Error("expected non-empty items")
	}

	total, ok := data["total"].(int)
	if !ok || total <= 0 {
		t.Error("expected positive total")
	}
}

func TestIssuesPage_Pagination(t *testing.T) {
	data := IssuesPage(100, 25)
	items := data["items"].([]map[string]any)
	// Should not crash with out-of-range offset
	if len(items) > 25 {
		t.Errorf("expected at most 25 items, got %d", len(items))
	}
}

func TestIssueDetail(t *testing.T) {
	data := IssueDetail("EVT-a1b2c3d4e5f6")

	if data["issue"] == nil {
		t.Error("missing issue")
	}
	if data["events"] == nil {
		t.Error("missing events")
	}
	if data["comments"] == nil {
		t.Error("missing comments")
	}
	if data["activity"] == nil {
		t.Error("missing activity")
	}
}

func TestEventsPage(t *testing.T) {
	data := EventsPage(0, 25)
	items := data["items"].([]map[string]any)
	if len(items) != 25 {
		t.Errorf("expected 25 events, got %d", len(items))
	}

	for _, e := range items {
		if e["severity"] == nil {
			t.Error("missing severity in event")
		}
		if e["state"] == nil {
			t.Error("missing state in event")
		}
	}
}

func TestDevicesPage(t *testing.T) {
	data := DevicesPage(0, 12)
	items := data["items"].([]map[string]any)
	if len(items) != 12 {
		t.Errorf("expected 12 devices, got %d", len(items))
	}

	for _, d := range items {
		if d["device_id"] == nil {
			t.Error("missing device_id")
		}
		if d["status"] == nil {
			t.Error("missing status")
		}
	}
}

func TestReleases(t *testing.T) {
	data := Releases()
	items := data["items"].([]map[string]any)
	if len(items) == 0 {
		t.Error("expected non-empty releases")
	}

	for _, r := range items {
		if r["version"] == nil {
			t.Error("missing version")
		}
		if r["build_id"] == nil {
			t.Error("missing build_id")
		}
	}
}

func TestReleaseStats(t *testing.T) {
	data := ReleaseStats("REL-001")
	if data["crash_free_rate"] == nil {
		t.Error("missing crash_free_rate")
	}
	if data["sessions"] == nil {
		t.Error("missing sessions")
	}
}

func TestArtifacts(t *testing.T) {
	data := Artifacts()
	items := data["items"].([]map[string]any)
	if len(items) == 0 {
		t.Error("expected non-empty artifacts")
	}
}

func TestAlerts(t *testing.T) {
	data := Alerts()
	items := data["items"].([]map[string]any)
	if len(items) == 0 {
		t.Error("expected non-empty alerts")
	}
}

func TestChannels(t *testing.T) {
	data := Channels()
	items := data["items"].([]map[string]any)
	if len(items) == 0 {
		t.Error("expected non-empty channels")
	}
}

func TestRelays(t *testing.T) {
	data := Relays()
	items := data["items"].([]map[string]any)
	if len(items) == 0 {
		t.Error("expected non-empty relays")
	}
}

func TestProjects(t *testing.T) {
	data := Projects()
	items := data["items"].([]map[string]any)
	if len(items) == 0 {
		t.Error("expected non-empty projects")
	}
}

func TestHardware(t *testing.T) {
	data := Hardware()
	items := data["items"].([]map[string]any)
	if len(items) == 0 {
		t.Error("expected non-empty hardware")
	}
}

func TestBootSessions(t *testing.T) {
	data := BootSessions(0, 15)
	items := data["items"].([]map[string]any)
	if len(items) != 15 {
		t.Errorf("expected 15 boot sessions, got %d", len(items))
	}
}

func TestDeadJobs(t *testing.T) {
	data := DeadJobs()
	items := data["items"].([]map[string]any)
	if len(items) == 0 {
		t.Error("expected non-empty dead jobs")
	}
}

func TestAudit(t *testing.T) {
	data := Audit(0, 100)
	items := data["items"].([]map[string]any)
	if len(items) == 0 {
		t.Error("expected non-empty audit")
	}
}

func TestBootstrap(t *testing.T) {
	data := Bootstrap()
	if data["bootstrapped"] != true {
		t.Error("expected bootstrapped to be true")
	}
	if data["open_ui"] != true {
		t.Error("expected open_ui to be true")
	}
}

func TestMe(t *testing.T) {
	data := Me()
	if data["email"] != "admin@fleet.io" {
		t.Errorf("expected admin@fleet.io, got %v", data["email"])
	}
	if data["role"] != "admin" {
		t.Errorf("expected role admin, got %v", data["role"])
	}
}

func TestLogin(t *testing.T) {
	data := Login("admin@fleet.io", "password")
	if data["token"] == nil || data["token"] == "" {
		t.Error("expected non-empty token")
	}
	if data["user"] == nil {
		t.Error("expected user in login response")
	}
}

func TestSearch(t *testing.T) {
	data := Search("sensor")
	if data["issues"] == nil {
		t.Error("expected issues in search")
	}
	if data["events"] == nil {
		t.Error("expected events in search")
	}
	if data["devices"] == nil {
		t.Error("expected devices in search")
	}
	if data["artifacts"] == nil {
		t.Error("expected artifacts in search")
	}
}

func TestFleetHealth(t *testing.T) {
	data := FleetHealth()

	if data["average_score"] == nil {
		t.Error("missing average_score")
	}
	if data["healthy_count"] == nil {
		t.Error("missing healthy_count")
	}
	if data["degraded_count"] == nil {
		t.Error("missing degraded_count")
	}
	if data["critical_count"] == nil {
		t.Error("missing critical_count")
	}
	if data["top_healthy"] == nil {
		t.Error("missing top_healthy")
	}
	if data["bottom_dead"] == nil {
		t.Error("missing bottom_dead")
	}
	if data["trend"] == nil {
		t.Error("missing trend")
	}
}

func TestDeviceHealth(t *testing.T) {
	data := DeviceHealth("dev-1")

	if data["device_id"] != "dev-1" {
		t.Errorf("expected device_id dev-1, got %v", data["device_id"])
	}
	if data["score"] == nil {
		t.Error("missing score")
	}
	if data["status"] == nil {
		t.Error("missing status")
	}
}

func TestMemorialDevices(t *testing.T) {
	data := MemorialDevices()
	items := data["items"].([]map[string]any)
	if len(items) == 0 {
		t.Error("expected non-empty memorial devices")
	}

	for _, d := range items {
		if d["arch"] == nil {
			t.Error("missing arch")
		}
		if d["firmwareVersion"] == nil {
			t.Error("missing firmwareVersion")
		}
		if d["daysSinceLastSeen"] == nil {
			t.Error("missing daysSinceLastSeen")
		}
		if d["candlesLit"] == nil {
			t.Error("missing candlesLit")
		}
	}
}

func TestPublicCrashes(t *testing.T) {
	data := PublicCrashes(30)

	if data["total_crashes"] == nil {
		t.Error("missing total_crashes")
	}
	if data["unique_fingerprints"] == nil {
		t.Error("missing unique_fingerprints")
	}
	if data["affected_devices"] == nil {
		t.Error("missing affected_devices")
	}
	if data["by_severity"] == nil {
		t.Error("missing by_severity")
	}
	if data["top_architectures"] == nil {
		t.Error("missing top_architectures")
	}
}

func TestPublicCrashesByArch(t *testing.T) {
	data := PublicCrashesByArch()
	items := data["items"].([]map[string]any)
	if len(items) == 0 {
		t.Error("expected non-empty architectures")
	}
}

func TestPublicCrashesByRegion(t *testing.T) {
	data := PublicCrashesByRegion()
	items := data["items"].([]map[string]any)
	if len(items) == 0 {
		t.Error("expected non-empty regions")
	}
}

func TestDeviceDNA(t *testing.T) {
	data := DeviceDNA()
	items := data["items"].([]map[string]any)
	if len(items) == 0 {
		t.Error("expected non-empty device DNA")
	}

	for _, d := range items {
		if d["device_id"] == nil {
			t.Error("missing device_id")
		}
		if d["mcu_uuid"] == nil {
			t.Error("missing mcu_uuid")
		}
		if d["fingerprint"] == nil {
			t.Error("missing fingerprint")
		}
	}
}

func TestBaseInt(t *testing.T) {
	for i := 0; i < 100; i++ {
		v := baseInt(10, 20)
		if v < 10 || v > 20 {
			t.Errorf("baseInt(10, 20) returned %d, out of range", v)
		}
	}
}

func TestFmtHex(t *testing.T) {
	if got := fmtHex(0xabcdef, 6); got != "abcdef" {
		t.Errorf("fmtHex(0xabcdef, 6) = %q, want %q", got, "abcdef")
	}
	if got := fmtHex(0, 4); got != "0000" {
		t.Errorf("fmtHex(0, 4) = %q, want %q", got, "0000")
	}
}
