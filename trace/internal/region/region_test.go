package region

import (
	"context"
	"testing"
	"time"
)

func TestNewManager(t *testing.T) {
	regions := []*Region{
		{ID: "us-east-1", Name: "US East", Endpoint: "https://trace-us.example.com"},
		{ID: "eu-west-1", Name: "EU West", Endpoint: "https://trace-eu.example.com"},
	}
	m := NewManager("us-east-1", regions)
	if m.GetPrimaryRegion().ID != "us-east-1" {
		t.Errorf("expected primary us-east-1, got %s", m.GetPrimaryRegion().ID)
	}
	if !m.GetPrimaryRegion().Active {
		t.Error("primary region should be active")
	}
	if m.GetPrimaryRegion().Primary != true {
		t.Error("primary region should be marked as primary")
	}
	eu, _ := m.GetRegion("eu-west-1")
	if eu.Active {
		t.Error("secondary region should not be active")
	}
}

func TestGetRegion(t *testing.T) {
	regions := []*Region{{ID: "us-east-1", Name: "US East"}}
	m := NewManager("us-east-1", regions)

	r, err := m.GetRegion("us-east-1")
	if err != nil || r.ID != "us-east-1" {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = m.GetRegion("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent region")
	}
}

func TestSelectRegion(t *testing.T) {
	regions := []*Region{
		{ID: "us-east-1", Name: "US East", Active: true},
		{ID: "eu-west-1", Name: "EU West", Active: true},
	}
	m := NewManager("us-east-1", regions)

	// Request from existing active region
	selected := m.SelectRegion(context.Background(), "us-east-1")
	if selected.ID != "us-east-1" {
		t.Errorf("expected us-east-1, got %s", selected.ID)
	}

	// Request from non-existing region falls back to primary
	selected = m.SelectRegion(context.Background(), "nonexistent")
	if selected.ID != "us-east-1" {
		t.Errorf("expected fallback to primary us-east-1, got %s", selected.ID)
	}
}

func TestHeartbeat(t *testing.T) {
	regions := []*Region{{ID: "us-east-1", Name: "US East"}}
	m := NewManager("us-east-1", regions)

	before := time.Now()
	m.Heartbeat("us-east-1")
	r, _ := m.GetRegion("us-east-1")
	if r.LastSync.Before(before) {
		t.Error("heartbeat should update LastSync")
	}
}

func TestSyncRegion(t *testing.T) {
	regions := []*Region{
		{ID: "us-east-1", Name: "US East"},
		{ID: "eu-west-1", Name: "EU West"},
	}
	m := NewManager("us-east-1", regions)
	m.SetReplicator(fakeReplicator{})

	err := m.SyncRegion(context.Background(), "us-east-1", "eu-west-1")
	if err != nil {
		t.Fatalf("sync should not error: %v", err)
	}

	status, err := m.GetReplicationStatus("us-east-1", "eu-west-1")
	if err != nil {
		t.Fatalf("should have replication status: %v", err)
	}
	if status.Status != "synced" {
		t.Errorf("expected synced, got %s", status.Status)
	}
}

type fakeReplicator struct{}

func (fakeReplicator) Sync(_ context.Context, source, dest string) (ReplicationStatus, error) {
	return ReplicationStatus{SourceRegion: source, DestRegion: dest, LastSyncAt: time.Now(), Status: "synced", EventsReplicated: 1}, nil
}

func TestSyncRegionErrors(t *testing.T) {
	regions := []*Region{{ID: "us-east-1", Name: "US East"}}
	m := NewManager("us-east-1", regions)
	m.SetReplicator(fakeReplicator{})

	err := m.SyncRegion(context.Background(), "nonexistent", "us-east-1")
	if err == nil {
		t.Error("expected error for nonexistent source")
	}

	err = m.SyncRegion(context.Background(), "us-east-1", "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent dest")
	}
}

func TestTriggerFailover(t *testing.T) {
	regions := []*Region{{ID: "us-east-1", Name: "US East", Active: true}}
	m := NewManager("us-east-1", regions)

	fs, err := m.TriggerFailover("us-east-1", "maintenance")
	if err != nil {
		t.Fatalf("failover should not error: %v", err)
	}
	if !fs.IsPrimary {
		t.Error("failed-over region should be primary")
	}
	if fs.Reason != "maintenance" {
		t.Errorf("expected reason maintenance, got %s", fs.Reason)
	}
}

func TestTriggerFailoverNonActive(t *testing.T) {
	regions := []*Region{{ID: "us-east-1", Name: "US East", Active: false}}
	m := NewManager("us-east-1", regions)

	// Source code does not check Active flag - any region can failover
	fs, err := m.TriggerFailover("us-east-1", "maintenance")
	if err != nil {
		t.Fatalf("failover should work even for non-active region: %v", err)
	}
	if fs == nil {
		t.Error("failover status should not be nil")
	}
}

func TestIsRegionHealthy(t *testing.T) {
	regions := []*Region{{ID: "us-east-1", Name: "US East", LastSync: time.Now()}}
	m := NewManager("us-east-1", regions)

	if !m.IsRegionHealthy("us-east-1") {
		t.Error("recent heartbeat should be healthy")
	}

	if m.IsRegionHealthy("nonexistent") {
		t.Error("nonexistent region should not be healthy")
	}
}

func TestGeoShardKey(t *testing.T) {
	key := GeoShardKey("us-east-1", "proj-123")
	if key != "us-east-1:proj-123" {
		t.Errorf("unexpected shard key: %s", key)
	}
}

func TestListReplicationPairs(t *testing.T) {
	regions := []*Region{
		{ID: "us-east-1"},
		{ID: "eu-west-1"},
	}
	m := NewManager("us-east-1", regions)
	m.SetReplicator(fakeReplicator{})

	if err := m.SyncRegion(context.Background(), "us-east-1", "eu-west-1"); err != nil {
		t.Fatalf("sync should use configured replicator: %v", err)
	}
	pairs := m.ListReplicationPairs()
	if len(pairs) != 1 {
		t.Errorf("expected 1 pair, got %d", len(pairs))
	}
}

func TestListFailovers(t *testing.T) {
	regions := []*Region{{ID: "us-east-1", Active: true}}
	m := NewManager("us-east-1", regions)

	_, _ = m.TriggerFailover("us-east-1", "test")
	failovers := m.ListFailovers()
	if len(failovers) != 1 {
		t.Errorf("expected 1 failover, got %d", len(failovers))
	}
}
