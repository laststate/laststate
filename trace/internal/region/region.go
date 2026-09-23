// Package region contains the control-plane model for multi-region Trace
// deployments. Replication is deliberately injected: this package must never
// report synthetic replication success.
package region

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Region represents a geographic region in a multi-region deployment.
type Region struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Primary        bool          `json:"primary"`
	Endpoint       string        `json:"endpoint"`
	Active         bool          `json:"active"`
	LastSync       time.Time     `json:"last_sync"`
	ReplicationLag time.Duration `json:"replication_lag"`
}

// ReplicationStatus represents the status of cross-region replication.
type ReplicationStatus struct {
	SourceRegion     string        `json:"source_region"`
	DestRegion       string        `json:"dest_region"`
	LastSyncAt       time.Time     `json:"last_sync_at"`
	Lag              time.Duration `json:"lag"`
	Status           string        `json:"status"` // "syncing", "synced", "error"
	EventsReplicated int           `json:"events_replicated"`
}

// FailoverStatus represents the status of a region failover.
type FailoverStatus struct {
	Region       string        `json:"region"`
	IsPrimary    bool          `json:"is_primary"`
	LastFailover time.Time     `json:"last_failover"`
	Reason       string        `json:"reason"`
	RecoveryTime time.Duration `json:"recovery_time"`
}

// Manager manages multi-region state and replication.
type Manager struct {
	mu            sync.RWMutex
	regions       map[string]*Region
	status        map[string]*ReplicationStatus
	failovers     map[string]*FailoverStatus
	primaryRegion string
	heartbeat     time.Duration
	replicator    Replicator
}

// Replicator is the storage-specific cross-region replication boundary.
// Implementations must return measured counts, lag, and timestamps.
type Replicator interface {
	Sync(ctx context.Context, source, dest string) (ReplicationStatus, error)
}

// NewManager creates a new multi-region manager.
func NewManager(primaryRegion string, regions []*Region) *Manager {
	m := &Manager{
		regions:       make(map[string]*Region),
		status:        make(map[string]*ReplicationStatus),
		failovers:     make(map[string]*FailoverStatus),
		primaryRegion: primaryRegion,
		heartbeat:     10 * time.Second,
	}
	for _, r := range regions {
		m.regions[r.ID] = r
		r.Active = r.ID == primaryRegion
		r.Primary = r.ID == primaryRegion
	}
	return m
}

// SetReplicator configures the real replication implementation.
func (m *Manager) SetReplicator(replicator Replicator) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.replicator = replicator
}

// GetRegion returns the region with the given ID.
func (m *Manager) GetRegion(id string) (*Region, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.regions[id]
	if !ok {
		return nil, fmt.Errorf("region %q not found", id)
	}
	return r, nil
}

// GetPrimaryRegion returns the primary region.
func (m *Manager) GetPrimaryRegion() *Region {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.regions[m.primaryRegion]
}

// GetRegions returns all regions.
func (m *Manager) GetRegions() []*Region {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Region, 0, len(m.regions))
	for _, r := range m.regions {
		out = append(out, r)
	}
	return out
}

// GetReplicationStatus returns the replication status for a region pair.
func (m *Manager) GetReplicationStatus(source, dest string) (*ReplicationStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := source + ":" + dest
	s, ok := m.status[key]
	if !ok {
		return nil, fmt.Errorf("replication status for %q not found", key)
	}
	return s, nil
}

// UpdateReplicationStatus updates the replication status for a region pair.
func (m *Manager) UpdateReplicationStatus(source, dest string, status *ReplicationStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status[source+":"+dest] = status
}

// TriggerFailover triggers a failover to the given region.
func (m *Manager) TriggerFailover(regionID, reason string) (*FailoverStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.regions[regionID]
	if !ok {
		return nil, fmt.Errorf("region %q not found", regionID)
	}
	if !r.Active {
		return nil, fmt.Errorf("region %q is not active", regionID)
	}

	fs := &FailoverStatus{
		Region:       regionID,
		IsPrimary:    true,
		LastFailover: time.Now(),
		Reason:       reason,
		RecoveryTime: 0,
	}
	m.failovers[regionID] = fs
	return fs, nil
}

// GetFailoverStatus returns the failover status for a region.
func (m *Manager) GetFailoverStatus(regionID string) (*FailoverStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	fs, ok := m.failovers[regionID]
	if !ok {
		return nil, fmt.Errorf("failover status for %q not found", regionID)
	}
	return fs, nil
}

// Heartbeat updates the heartbeat for a region.
func (m *Manager) Heartbeat(regionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.regions[regionID]
	if ok {
		r.LastSync = time.Now()
	}
}

// SyncRegion runs the configured replication implementation.
func (m *Manager) SyncRegion(ctx context.Context, source, dest string) error {
	m.mu.RLock()
	src, ok := m.regions[source]
	if !ok {
		m.mu.RUnlock()
		return fmt.Errorf("source region %q not found", source)
	}
	destR, ok := m.regions[dest]
	if !ok {
		m.mu.RUnlock()
		return fmt.Errorf("dest region %q not found", dest)
	}
	m.mu.RUnlock()

	m.mu.RLock()
	replicator := m.replicator
	m.mu.RUnlock()
	if replicator == nil {
		return fmt.Errorf("multi-region replication is not configured")
	}
	status, err := replicator.Sync(ctx, source, dest)
	if err != nil {
		return err
	}
	if status.SourceRegion != source || status.DestRegion != dest || status.Status == "" {
		return fmt.Errorf("replicator returned invalid status for %s:%s", source, dest)
	}
	m.UpdateReplicationStatus(source, dest, &status)
	now := status.LastSyncAt
	if now.IsZero() {
		now = time.Now()
	}
	m.mu.Lock()
	src.LastSync = now
	destR.LastSync = now
	m.mu.Unlock()

	return nil
}

// GeoShardKey generates a shard key for geo-sharding.
func GeoShardKey(regionID string, projectID string) string {
	return fmt.Sprintf("%s:%s", regionID, projectID)
}

// SelectRegion selects the best region for a request based on latency and proximity.
func (m *Manager) SelectRegion(ctx context.Context, requestRegion string) *Region {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// If the request region is active, use it
	if r, ok := m.regions[requestRegion]; ok && r.Active {
		return r
	}

	// Fall back to primary
	return m.regions[m.primaryRegion]
}

// IsRegionHealthy checks if a region is healthy.
func (m *Manager) IsRegionHealthy(regionID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.regions[regionID]
	if !ok {
		return false
	}
	return time.Since(r.LastSync) < 30*time.Second
}

// ListReplicationPairs returns all replication pairs.
func (m *Manager) ListReplicationPairs() []ReplicationStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ReplicationStatus, 0, len(m.status))
	for _, s := range m.status {
		out = append(out, *s)
	}
	return out
}

// ListFailovers returns all failover events.
func (m *Manager) ListFailovers() []*FailoverStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*FailoverStatus, 0, len(m.failovers))
	for _, f := range m.failovers {
		out = append(out, f)
	}
	return out
}
