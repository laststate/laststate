// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

// Package store is the durable spool: content-addressed objects plus SQLite metadata.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/laststate/relay/internal/lep"
)

var (
	ErrSpoolFull = errors.New("relay spool is full")
	ErrNotFound  = errors.New("not found")
)

type Options struct {
	MaxSpoolBytes      int64
	MinFreeBytes       int64
	DeliveredRetention time.Duration
	PressurePolicy     string
	FsyncMode          string
	DeliveryMode       string
}

type Store struct {
	db      *sql.DB
	dataDir string
	options Options
}

type Event struct {
	ID, PayloadHash, SourceID, State, RawObjectPath string
	Envelope                                        lep.Envelope
	ReceivedAt                                      time.Time
	Size                                            int64
	DuplicateCount                                  int
}

type Result struct {
	Event     Event
	Duplicate bool
}

type Destination struct {
	ID, URL, AuthType, TokenRef string
	Priority                    int
	Required                    bool
}

type DestinationStatus struct {
	ID          string     `json:"id"`
	URL         string     `json:"url"`
	Enabled     bool       `json:"enabled"`
	Paused      bool       `json:"paused"`
	Required    bool       `json:"required"`
	Priority    int        `json:"priority"`
	State       string     `json:"state"`
	Pending     int        `json:"pending"`
	Delivered   int        `json:"delivered"`
	DeadLetter  int        `json:"dead_letter"`
	LastSuccess *time.Time `json:"last_success,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
}

type PendingDelivery struct {
	EventID, DestinationID, URL, AuthType, TokenRef, RawObjectPath string
	AttemptCount, Priority                                         int
	Required                                                       bool
	LeaseToken                                                     string
}

type Status struct {
	RelayID       string        `json:"relay_id"`
	Events        int           `json:"events"`
	Pending       int           `json:"pending"`
	Delivering    int           `json:"delivering"`
	Delivered     int           `json:"delivered"`
	Partial       int           `json:"partially_delivered"`
	DeadLetter    int           `json:"dead_letter"`
	Quarantined   int           `json:"quarantined"`
	SpoolBytes    int64         `json:"spool_bytes"`
	FreeBytes     int64         `json:"free_bytes"`
	OldestPending time.Duration `json:"oldest_pending"`
}

type ReconcileReport struct {
	RemovedPendingFiles int      `json:"removed_pending_files"`
	MissingObjects      []string `json:"missing_objects"`
	CorruptObjects      []string `json:"corrupt_objects"`
	OrphanObjects       []string `json:"orphan_objects"`
}

type PruneResult struct {
	Events  int   `json:"events"`
	Objects int   `json:"objects"`
	Bytes   int64 `json:"bytes"`
}

func Open(dataDir string) (*Store, error) {
	return OpenWithOptions(dataDir, Options{
		MaxSpoolBytes:      10 << 30,
		MinFreeBytes:       256 << 20,
		DeliveredRetention: 30 * 24 * time.Hour,
		PressurePolicy:     "reject-new",
		FsyncMode:          "full",
		DeliveryMode:       "mirror",
	})
}

func OpenWithOptions(dataDir string, options Options) (*Store, error) {
	absolute, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	dataDir = filepath.Clean(absolute)
	for _, directory := range []string{"events", "artifacts", "quarantine", "exports", "reports"} {
		if err := os.MkdirAll(filepath.Join(dataDir, directory), 0700); err != nil {
			return nil, err
		}
	}
	if options.MaxSpoolBytes <= 0 {
		options.MaxSpoolBytes = 10 << 30
	}
	if options.MinFreeBytes <= 0 {
		options.MinFreeBytes = 256 << 20
	}
	if options.DeliveredRetention <= 0 {
		options.DeliveredRetention = 30 * 24 * time.Hour
	}
	if options.PressurePolicy == "" {
		options.PressurePolicy = "reject-new"
	}
	if options.FsyncMode == "" {
		options.FsyncMode = "full"
	}
	if options.DeliveryMode == "" {
		options.DeliveryMode = "mirror"
	}

	dbPath := filepath.Join(dataDir, "relay.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	// A single SQLite connection gives deterministic PRAGMA behavior and a
	// single writer. WAL still permits concurrent readers inside that connection.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;`); err != nil {
		db.Close()
		return nil, err
	}
	store := &Store{db: db, dataDir: dataDir, options: options}
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	if err := store.ensureInstance(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	// A process can die after claiming a delivery. Stale claims are made ready
	// again at startup; idempotency protects the remote side from duplicates.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`UPDATE event_destinations SET state='RETRY', lease_token=NULL, lease_until=NULL, next_attempt_at=? WHERE state='DELIVERING'`, now); err != nil {
		db.Close()
		return nil, err
	}
	// Crash recovery: event row without destination rows (pre-atomic Put+queue).
	if store.options.DeliveryMode != "local-only" {
		if _, err := db.Exec(`INSERT OR IGNORE INTO event_destinations(event_id,destination_id,state,attempt_count,next_attempt_at)
			SELECT e.id, d.id, 'READY', 0, ?
			FROM events e
			CROSS JOIN destinations d
			WHERE d.enabled=1
			  AND e.state IN ('PERSISTED','PARTIALLY_DELIVERED')
			  AND NOT EXISTS (SELECT 1 FROM event_destinations ed WHERE ed.event_id=e.id)`, now); err != nil {
			db.Close()
			return nil, err
		}
	}
	return store, nil
}

func (store *Store) migrate(ctx context.Context) error {
	schema := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS relay_instance (id TEXT PRIMARY KEY, name TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, first_version TEXT NOT NULL DEFAULT '', current_version TEXT NOT NULL DEFAULT '');`,
		`CREATE TABLE IF NOT EXISTS events (
			id TEXT PRIMARY KEY,
			payload_hash TEXT NOT NULL UNIQUE,
			raw_object_path TEXT NOT NULL,
			protocol_version INTEGER NOT NULL,
			event_type INTEGER NOT NULL,
			architecture INTEGER NOT NULL,
			flags INTEGER NOT NULL,
			sequence INTEGER NOT NULL,
			source_event_id INTEGER NOT NULL,
			source_id TEXT NOT NULL,
			received_at TEXT NOT NULL,
			state TEXT NOT NULL,
			size_bytes INTEGER NOT NULL,
			duplicate_count INTEGER NOT NULL DEFAULT 0,
			last_seen_at TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE INDEX IF NOT EXISTS events_received_at ON events(received_at DESC);`,
		`CREATE INDEX IF NOT EXISTS events_state ON events(state);`,
		`CREATE TABLE IF NOT EXISTS event_observations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
			source_id TEXT NOT NULL,
			observed_at TEXT NOT NULL,
			is_duplicate INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS destinations (
			id TEXT PRIMARY KEY,
			url TEXT NOT NULL,
			auth_type TEXT NOT NULL DEFAULT 'none',
			token TEXT NOT NULL DEFAULT '',
			token_ref TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL,
			paused INTEGER NOT NULL DEFAULT 0,
			required INTEGER NOT NULL DEFAULT 0,
			priority INTEGER NOT NULL DEFAULT 0,
			state TEXT NOT NULL DEFAULT 'UNKNOWN',
			last_success_at TEXT,
			last_error TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS event_destinations (
			event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
			destination_id TEXT NOT NULL REFERENCES destinations(id) ON DELETE CASCADE,
			state TEXT NOT NULL,
			attempt_count INTEGER NOT NULL DEFAULT 0,
			next_attempt_at TEXT NOT NULL,
			last_attempt_at TEXT,
			remote_receipt TEXT,
			last_status_code INTEGER,
			last_error TEXT,
			delivered_at TEXT,
			lease_token TEXT,
			lease_until TEXT,
			PRIMARY KEY(event_id,destination_id)
		);`,
		`CREATE INDEX IF NOT EXISTS event_destinations_pending ON event_destinations(state,next_attempt_at);`,
		`CREATE INDEX IF NOT EXISTS event_destinations_destination ON event_destinations(destination_id,state);`,
		`CREATE TABLE IF NOT EXISTS delivery_attempts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id TEXT NOT NULL,
			destination_id TEXT NOT NULL,
			attempted_at TEXT NOT NULL,
			status_code INTEGER,
			error TEXT,
			duration_ms INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE TABLE IF NOT EXISTS sources (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			enabled INTEGER NOT NULL,
			state TEXT NOT NULL DEFAULT 'UNKNOWN',
			last_error TEXT,
			last_seen_at TEXT,
			bytes_received INTEGER NOT NULL DEFAULT 0,
			frames_received INTEGER NOT NULL DEFAULT 0,
			frames_rejected INTEGER NOT NULL DEFAULT 0
		);`,
	}
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	for _, statement := range schema {
		if _, err := transaction.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if err := ensureColumn(ctx, transaction, "events", "duplicate_count", `INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := ensureColumn(ctx, transaction, "events", "last_seen_at", `TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	for name, declaration := range map[string]string{
		"auth_type": "TEXT NOT NULL DEFAULT 'none'", "token": "TEXT NOT NULL DEFAULT ''", "token_ref": "TEXT NOT NULL DEFAULT ''",
		"required": "INTEGER NOT NULL DEFAULT 0", "state": "TEXT NOT NULL DEFAULT 'UNKNOWN'",
		"last_success_at": "TEXT", "last_error": "TEXT",
	} {
		if err := ensureColumn(ctx, transaction, "destinations", name, declaration); err != nil {
			return err
		}
	}
	for name, declaration := range map[string]string{"lease_token": "TEXT", "lease_until": "TEXT"} {
		if err := ensureColumn(ctx, transaction, "event_destinations", name, declaration); err != nil {
			return err
		}
	}
	_, err = transaction.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(1,?)`, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return transaction.Commit()
}

func ensureColumn(ctx context.Context, tx *sql.Tx, table, column, declaration string) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = tx.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+declaration)
	return err
}

func (store *Store) ensureInstance(ctx context.Context) error {
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM relay_instance`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	id, err := randomID("relay_", 16)
	if err != nil {
		return err
	}
	_, err = store.db.ExecContext(ctx, `INSERT INTO relay_instance(id,created_at) VALUES(?,?)`, id, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func randomID(prefix string, bytesCount int) (string, error) {
	value := make([]byte, bytesCount)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(value), nil
}

func (store *Store) ConfigureInstance(ctx context.Context, requestedID, name, version string) (string, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	var currentID, firstVersion string
	if err := tx.QueryRowContext(ctx, `SELECT id,first_version FROM relay_instance LIMIT 1`).Scan(&currentID, &firstVersion); err != nil {
		return "", err
	}
	requestedID = strings.TrimSpace(requestedID)
	if requestedID != "" && requestedID != "auto" && requestedID != currentID {
		if firstVersion != "" {
			return "", fmt.Errorf("configured relay ID %q does not match persistent ID %q; reset the data directory explicitly to change identity", requestedID, currentID)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE relay_instance SET id=? WHERE id=?`, requestedID, currentID); err != nil {
			return "", err
		}
		currentID = requestedID
	}
	if version == "" {
		version = "dev"
	}
	if firstVersion == "" {
		firstVersion = version
	}
	if _, err := tx.ExecContext(ctx, `UPDATE relay_instance SET name=?,first_version=?,current_version=? WHERE id=?`, name, firstVersion, version, currentID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return currentID, nil
}

func (store *Store) Close() error                { return store.db.Close() }
func (store *Store) DataDir() string             { return store.dataDir }
func (store *Store) SetDeliveryMode(mode string) { store.options.DeliveryMode = mode }

func (store *Store) ConfigureSources(ctx context.Context, sources map[string]string) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE sources SET enabled=0`); err != nil {
		return err
	}
	for id, kind := range sources {
		if _, err := tx.ExecContext(ctx, `INSERT INTO sources(id,type,enabled,state) VALUES(?,?,1,'UNKNOWN') ON CONFLICT(id) DO UPDATE SET type=excluded.type,enabled=1`, id, kind); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (store *Store) SourceActivity(ctx context.Context, sourceID string, bytes, frames, rejected int64, state, failure string) error {
	_, err := store.db.ExecContext(ctx, `UPDATE sources SET state=?,last_error=?,last_seen_at=?,bytes_received=bytes_received+?,frames_received=frames_received+?,frames_rejected=frames_rejected+? WHERE id=?`, state, failure, time.Now().UTC().Format(time.RFC3339Nano), bytes, frames, rejected, sourceID)
	return err
}

func (store *Store) ConfigureDestinations(ctx context.Context, destinations []Destination, mode string) error {
	store.options.DeliveryMode = mode
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Removed destinations are disabled before the current configuration is
	// applied. This prevents accidental delivery to an endpoint removed from YAML.
	if _, err := tx.ExecContext(ctx, `UPDATE destinations SET enabled=0,state='DISABLED'`); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, destination := range destinations {
		if _, err = tx.ExecContext(ctx, `INSERT INTO destinations(id,url,auth_type,token,token_ref,enabled,paused,required,priority,state) VALUES(?,?,?,'',?,1,0,?,?,'UNKNOWN') ON CONFLICT(id) DO UPDATE SET url=excluded.url,auth_type=excluded.auth_type,token_ref=excluded.token_ref,enabled=1,required=excluded.required,priority=excluded.priority,state=CASE WHEN destinations.paused=1 THEN 'PAUSED' ELSE 'UNKNOWN' END`, destination.ID, destination.URL, destination.AuthType, destination.TokenRef, destination.Required, destination.Priority); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO event_destinations(event_id,destination_id,state,attempt_count,next_attempt_at) SELECT id,?,'READY',0,? FROM events WHERE state NOT IN ('DELIVERED','EXPIRED')`, destination.ID, now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE event_destinations SET state='READY',next_attempt_at=? WHERE destination_id=? AND state='DISABLED'`, now, destination.ID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE event_destinations SET state='DISABLED',lease_token=NULL,lease_until=NULL WHERE destination_id IN (SELECT id FROM destinations WHERE enabled=0) AND state NOT IN ('DELIVERED','SKIPPED')`); err != nil {
		return err
	}
	return tx.Commit()
}

func (store *Store) QueueDestinations(ctx context.Context, eventID string, targets []string) error {
	if store.options.DeliveryMode == "local-only" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return store.queueDestinations(ctx, store.db, eventID, targets, now)
}

type sqlExec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (store *Store) queueDestinations(ctx context.Context, exec sqlExec, eventID string, targets []string, now string) error {
	if len(targets) > 0 {
		for _, destinationID := range targets {
			if _, err := exec.ExecContext(ctx, `INSERT OR IGNORE INTO event_destinations(event_id,destination_id,state,attempt_count,next_attempt_at) SELECT ?,id,'READY',0,? FROM destinations WHERE id=? AND enabled=1`, eventID, now, destinationID); err != nil {
				return err
			}
		}
		return nil
	}
	_, err := exec.ExecContext(ctx, `INSERT OR IGNORE INTO event_destinations(event_id,destination_id,state,attempt_count,next_attempt_at) SELECT ?,id,'READY',0,? FROM destinations WHERE enabled=1`, eventID, now)
	return err
}

// Put stores raw LEP bytes and queues destinations in the same SQLite transaction.
// targets empty means every enabled destination (mirror default).
func (store *Store) Put(ctx context.Context, sourceID string, raw []byte, envelope lep.Envelope, targets ...string) (Result, error) {
	if err := store.ensureCapacity(ctx, int64(len(raw))); err != nil {
		return Result{}, err
	}
	hash := sha256.Sum256(raw)
	payloadHash := hex.EncodeToString(hash[:])
	event := Event{
		ID: "evt_" + payloadHash[:26], PayloadHash: payloadHash, SourceID: sourceID,
		State: "PERSISTED", RawObjectPath: filepath.Join(store.dataDir, "events", payloadHash[:2], payloadHash+".lep"),
		Envelope: envelope, ReceivedAt: time.Now().UTC(), Size: int64(len(raw)),
	}
	var existing Event
	var received string
	err := store.db.QueryRowContext(ctx, `SELECT id,raw_object_path,received_at,state,size_bytes,duplicate_count FROM events WHERE payload_hash=?`, payloadHash).Scan(&existing.ID, &existing.RawObjectPath, &received, &existing.State, &existing.Size, &existing.DuplicateCount)
	if err == nil {
		existing.PayloadHash = payloadHash
		existing.SourceID = sourceID
		existing.Envelope = envelope
		existing.ReceivedAt, _ = time.Parse(time.RFC3339Nano, received)
		now := time.Now().UTC().Format(time.RFC3339Nano)
		_, updateErr := store.db.ExecContext(ctx, `UPDATE events SET duplicate_count=duplicate_count+1,last_seen_at=? WHERE id=?`, now, existing.ID)
		if updateErr != nil {
			return Result{}, updateErr
		}
		if _, err := store.db.ExecContext(ctx, `INSERT INTO event_observations(event_id,source_id,observed_at,is_duplicate) VALUES(?,?,?,1)`, existing.ID, sourceID, now); err != nil {
			return Result{}, err
		}
		return Result{Event: existing, Duplicate: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Result{}, err
	}
	if err = writeDurable(event.RawObjectPath, raw, store.options.FsyncMode); err != nil {
		return Result{}, err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = tx.Rollback() }()
	now := event.ReceivedAt.Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO events(id,payload_hash,raw_object_path,protocol_version,event_type,architecture,flags,sequence,source_event_id,source_id,received_at,state,size_bytes,last_seen_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, event.ID, event.PayloadHash, event.RawObjectPath, event.Envelope.Version, event.Envelope.Type, event.Envelope.Architecture, event.Envelope.Flags, event.Envelope.Sequence, event.Envelope.EventID, event.SourceID, now, event.State, event.Size, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return store.Put(ctx, sourceID, raw, envelope, targets...)
		}
		return Result{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO event_observations(event_id,source_id,observed_at,is_duplicate) VALUES(?,?,?,0)`, event.ID, sourceID, now); err != nil {
		return Result{}, err
	}
	if store.options.DeliveryMode != "local-only" {
		if err := store.queueDestinations(ctx, tx, event.ID, targets, now); err != nil {
			return Result{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Result{}, err
	}
	return Result{Event: event}, nil
}

func (store *Store) ensureCapacity(ctx context.Context, incoming int64) error {
	var used int64
	if err := store.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(size_bytes),0) FROM events`).Scan(&used); err != nil {
		return err
	}
	free, _ := diskFree(store.dataDir)
	if used+incoming <= store.options.MaxSpoolBytes && (free == 0 || free-incoming >= store.options.MinFreeBytes) {
		return nil
	}
	if store.options.PressurePolicy == "drop-oldest-delivered" {
		if _, err := store.Prune(ctx, time.Now().UTC(), true); err != nil {
			return err
		}
		if err := store.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(size_bytes),0) FROM events`).Scan(&used); err != nil {
			return err
		}
		free, _ = diskFree(store.dataDir)
		if used+incoming <= store.options.MaxSpoolBytes && (free == 0 || free-incoming >= store.options.MinFreeBytes) {
			return nil
		}
	}
	return fmt.Errorf("%w: used=%d incoming=%d limit=%d free=%d minimum_free=%d", ErrSpoolFull, used, incoming, store.options.MaxSpoolBytes, free, store.options.MinFreeBytes)
}

func (store *Store) List(ctx context.Context, limit, offset int) ([]Event, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := store.db.QueryContext(ctx, `SELECT id,payload_hash,raw_object_path,protocol_version,event_type,architecture,flags,sequence,source_event_id,source_id,received_at,state,size_bytes,duplicate_count FROM events ORDER BY received_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		var event Event
		var received string
		if err := rows.Scan(&event.ID, &event.PayloadHash, &event.RawObjectPath, &event.Envelope.Version, &event.Envelope.Type, &event.Envelope.Architecture, &event.Envelope.Flags, &event.Envelope.Sequence, &event.Envelope.EventID, &event.SourceID, &received, &event.State, &event.Size, &event.DuplicateCount); err != nil {
			return nil, err
		}
		event.ReceivedAt, _ = time.Parse(time.RFC3339Nano, received)
		events = append(events, event)
	}
	return events, rows.Err()
}

// ClaimPendingDeliveries atomically leases work. A crashed worker's lease is
// recovered on restart, and remote idempotency makes a repeated POST safe.
func (store *Store) ClaimPendingDeliveries(ctx context.Context, limit int, leaseFor time.Duration) ([]PendingDelivery, error) {
	if limit <= 0 {
		return nil, nil
	}
	if leaseFor <= 0 {
		leaseFor = 30 * time.Second
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE event_destinations SET state='RETRY',lease_token=NULL,lease_until=NULL,next_attempt_at=? WHERE state='DELIVERING' AND lease_until<=?`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return nil, err
	}

	query := `SELECT ed.event_id,ed.destination_id,d.url,d.auth_type,d.token_ref,e.raw_object_path,ed.attempt_count,d.priority,d.required
		FROM event_destinations ed
		JOIN destinations d ON d.id=ed.destination_id
		JOIN events e ON e.id=ed.event_id
		WHERE d.enabled=1 AND d.paused=0 AND ed.state IN ('READY','RETRY') AND ed.next_attempt_at<=?`
	if store.options.DeliveryMode == "priority" {
		query += ` AND NOT EXISTS (
			SELECT 1 FROM event_destinations lower_ed JOIN destinations lower_d ON lower_d.id=lower_ed.destination_id
			WHERE lower_ed.event_id=ed.event_id AND lower_d.enabled=1 AND lower_d.paused=0
			AND lower_ed.state IN ('READY','RETRY','DELIVERING') AND lower_d.priority<d.priority
			AND (lower_ed.state='DELIVERING' OR lower_ed.next_attempt_at<=?)
		)`
	}
	query += ` ORDER BY d.priority,ed.next_attempt_at,ed.event_id LIMIT ?`
	args := []any{now.Format(time.RFC3339Nano)}
	if store.options.DeliveryMode == "priority" {
		args = append(args, now.Format(time.RFC3339Nano))
	}
	args = append(args, limit)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var deliveries []PendingDelivery
	for rows.Next() {
		var d PendingDelivery
		if err := rows.Scan(&d.EventID, &d.DestinationID, &d.URL, &d.AuthType, &d.TokenRef, &d.RawObjectPath, &d.AttemptCount, &d.Priority, &d.Required); err != nil {
			rows.Close()
			return nil, err
		}
		deliveries = append(deliveries, d)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range deliveries {
		lease, err := randomID("lease_", 12)
		if err != nil {
			return nil, err
		}
		deliveries[i].LeaseToken = lease
		result, err := tx.ExecContext(ctx, `UPDATE event_destinations SET state='DELIVERING',lease_token=?,lease_until=? WHERE event_id=? AND destination_id=? AND state IN ('READY','RETRY')`, lease, now.Add(leaseFor).Format(time.RFC3339Nano), deliveries[i].EventID, deliveries[i].DestinationID)
		if err != nil {
			return nil, err
		}
		changed, _ := result.RowsAffected()
		if changed == 0 {
			deliveries[i].LeaseToken = ""
		}
	}
	filtered := deliveries[:0]
	for _, d := range deliveries {
		if d.LeaseToken != "" {
			filtered = append(filtered, d)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return filtered, nil
}

// PendingDeliveries is retained for callers/tests and delegates to the lease API.
func (store *Store) PendingDeliveries(ctx context.Context, limit int) ([]PendingDelivery, error) {
	return store.ClaimPendingDeliveries(ctx, limit, 30*time.Second)
}

func (store *Store) RecordDelivery(ctx context.Context, delivery PendingDelivery, statusCode int, receipt, failure string, retryAt time.Time, permanent bool) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, `INSERT INTO delivery_attempts(event_id,destination_id,attempted_at,status_code,error) VALUES(?,?,?,?,?)`, delivery.EventID, delivery.DestinationID, now, statusCode, failure); err != nil {
		return err
	}
	leaseWhere := ""
	argsSuffix := []any{delivery.EventID, delivery.DestinationID}
	if delivery.LeaseToken != "" {
		leaseWhere = " AND lease_token=?"
		argsSuffix = append(argsSuffix, delivery.LeaseToken)
	}
	if failure == "" {
		args := []any{now, receipt, statusCode, now}
		args = append(args, argsSuffix...)
		result, err := tx.ExecContext(ctx, `UPDATE event_destinations SET state='DELIVERED',attempt_count=attempt_count+1,last_attempt_at=?,remote_receipt=?,last_status_code=?,last_error=NULL,delivered_at=?,lease_token=NULL,lease_until=NULL WHERE event_id=? AND destination_id=?`+leaseWhere, args...)
		if err != nil {
			return err
		}
		if changed, _ := result.RowsAffected(); changed == 0 {
			return fmt.Errorf("delivery lease is no longer valid")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE destinations SET state='HEALTHY',last_success_at=?,last_error=NULL WHERE id=?`, now, delivery.DestinationID); err != nil {
			return err
		}
		if store.options.DeliveryMode == "priority" {
			if _, err := tx.ExecContext(ctx, `UPDATE event_destinations SET state='SKIPPED',lease_token=NULL,lease_until=NULL WHERE event_id=? AND destination_id<>? AND state NOT IN ('DELIVERED','DEAD_LETTER')`, delivery.EventID, delivery.DestinationID); err != nil {
				return err
			}
		}
	} else {
		state := "RETRY"
		if permanent {
			state = "DEAD_LETTER"
		}
		args := []any{state, now, retryAt.UTC().Format(time.RFC3339Nano), statusCode, failure}
		args = append(args, argsSuffix...)
		result, err := tx.ExecContext(ctx, `UPDATE event_destinations SET state=?,attempt_count=attempt_count+1,last_attempt_at=?,next_attempt_at=?,last_status_code=?,last_error=?,lease_token=NULL,lease_until=NULL WHERE event_id=? AND destination_id=?`+leaseWhere, args...)
		if err != nil {
			return err
		}
		if changed, _ := result.RowsAffected(); changed == 0 {
			return fmt.Errorf("delivery lease is no longer valid")
		}
		destinationState := "DEGRADED"
		if permanent {
			destinationState = "FAILED"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE destinations SET state=?,last_error=? WHERE id=?`, destinationState, failure, delivery.DestinationID); err != nil {
			return err
		}
	}
	if err := store.updateEventStateTx(ctx, tx, delivery.EventID); err != nil {
		return err
	}
	return tx.Commit()
}

func (store *Store) ReleaseDelivery(ctx context.Context, delivery PendingDelivery, retryAt time.Time, failure string) error {
	_, err := store.db.ExecContext(ctx, `UPDATE event_destinations SET state='RETRY',next_attempt_at=?,last_error=?,lease_token=NULL,lease_until=NULL WHERE event_id=? AND destination_id=? AND lease_token=?`, retryAt.UTC().Format(time.RFC3339Nano), failure, delivery.EventID, delivery.DestinationID, delivery.LeaseToken)
	return err
}

func (store *Store) updateEventStateTx(ctx context.Context, tx *sql.Tx, eventID string) error {
	var pending, dead, delivered, requiredPending int
	if err := tx.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN ed.state IN ('READY','RETRY','DELIVERING') AND d.enabled=1 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN ed.state='DEAD_LETTER' AND d.enabled=1 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN ed.state='DELIVERED' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN d.required=1 AND d.enabled=1 AND ed.state<>'DELIVERED' THEN 1 ELSE 0 END),0)
		FROM event_destinations ed JOIN destinations d ON d.id=ed.destination_id WHERE ed.event_id=?`, eventID).Scan(&pending, &dead, &delivered, &requiredPending); err != nil {
		return err
	}
	state := "READY"
	switch {
	case requiredPending == 0 && delivered > 0:
		state = "DELIVERED"
	case delivered > 0 && (pending > 0 || dead > 0):
		state = "PARTIALLY_DELIVERED"
	case pending == 0 && dead > 0:
		state = "DEAD_LETTER"
	}
	_, err := tx.ExecContext(ctx, `UPDATE events SET state=? WHERE id=?`, state, eventID)
	return err
}

func (store *Store) Replay(ctx context.Context, eventID, destinationID string) error {
	query := `UPDATE event_destinations SET state='READY',next_attempt_at=?,last_error=NULL,lease_token=NULL,lease_until=NULL WHERE state IN ('DEAD_LETTER','RETRY','SKIPPED')`
	args := []any{time.Now().UTC().Format(time.RFC3339Nano)}
	if eventID != "" {
		query += " AND event_id=?"
		args = append(args, eventID)
	}
	if destinationID != "" {
		query += " AND destination_id=?"
		args = append(args, destinationID)
	}
	result, err := store.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		return ErrNotFound
	}
	if eventID != "" {
		if _, err := store.db.ExecContext(ctx, `UPDATE events SET state='READY' WHERE id=?`, eventID); err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) PauseDestination(ctx context.Context, id string, paused bool) error {
	state := "UNKNOWN"
	if paused {
		state = "PAUSED"
	}
	result, err := store.db.ExecContext(ctx, `UPDATE destinations SET paused=?,state=? WHERE id=? AND enabled=1`, paused, state, id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return fmt.Errorf("%w: destination %q", ErrNotFound, id)
	}
	return nil
}

func (store *Store) DestinationStatuses(ctx context.Context) ([]DestinationStatus, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT d.id,d.url,d.enabled,d.paused,d.required,d.priority,d.state,d.last_success_at,d.last_error,
		SUM(CASE WHEN ed.state IN ('READY','RETRY','DELIVERING') THEN 1 ELSE 0 END),
		SUM(CASE WHEN ed.state='DELIVERED' THEN 1 ELSE 0 END),
		SUM(CASE WHEN ed.state='DEAD_LETTER' THEN 1 ELSE 0 END)
		FROM destinations d LEFT JOIN event_destinations ed ON ed.destination_id=d.id GROUP BY d.id ORDER BY d.priority,d.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []DestinationStatus
	for rows.Next() {
		var item DestinationStatus
		var enabled, paused, required int
		var lastSuccess, lastError sql.NullString
		if err := rows.Scan(&item.ID, &item.URL, &enabled, &paused, &required, &item.Priority, &item.State, &lastSuccess, &lastError, &item.Pending, &item.Delivered, &item.DeadLetter); err != nil {
			return nil, err
		}
		item.Enabled, item.Paused, item.Required = enabled != 0, paused != 0, required != 0
		if lastSuccess.Valid {
			if parsed, err := time.Parse(time.RFC3339Nano, lastSuccess.String); err == nil {
				item.LastSuccess = &parsed
			}
		}
		if lastError.Valid {
			item.LastError = lastError.String
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (store *Store) Status(ctx context.Context) (Status, error) {
	var status Status
	_ = store.db.QueryRowContext(ctx, `SELECT id FROM relay_instance LIMIT 1`).Scan(&status.RelayID)
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(size_bytes),0),
		COALESCE(SUM(CASE WHEN state='PARTIALLY_DELIVERED' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN state='QUARANTINED' THEN 1 ELSE 0 END),0)
		FROM events`).Scan(&status.Events, &status.SpoolBytes, &status.Partial, &status.Quarantined); err != nil {
		return status, err
	}
	queries := []struct {
		state string
		dest  *int
	}{{"READY','RETRY", &status.Pending}, {"DELIVERING", &status.Delivering}, {"DELIVERED", &status.Delivered}, {"DEAD_LETTER", &status.DeadLetter}}
	for _, q := range queries {
		if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_destinations WHERE state IN ('`+q.state+`')`).Scan(q.dest); err != nil {
			return status, err
		}
	}
	var oldest sql.NullString
	_ = store.db.QueryRowContext(ctx, `SELECT MIN(e.received_at) FROM events e JOIN event_destinations ed ON ed.event_id=e.id WHERE ed.state IN ('READY','RETRY','DELIVERING')`).Scan(&oldest)
	if oldest.Valid {
		if parsed, err := time.Parse(time.RFC3339Nano, oldest.String); err == nil {
			status.OldestPending = time.Since(parsed)
		}
	}
	status.FreeBytes, _ = diskFree(store.dataDir)
	return status, nil
}

func (store *Store) RawEvent(ctx context.Context, id string) ([]byte, error) {
	var path, expectedHash string
	if err := store.db.QueryRowContext(ctx, `SELECT raw_object_path,payload_hash FROM events WHERE id=?`, id).Scan(&path, &expectedHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != expectedHash {
		return nil, fmt.Errorf("event object checksum mismatch")
	}
	return raw, nil
}

func (store *Store) Reconcile(ctx context.Context) (ReconcileReport, error) {
	var report ReconcileReport
	_ = filepath.WalkDir(filepath.Join(store.dataDir, "events"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), ".pending-") {
			if removeErr := os.Remove(path); removeErr == nil {
				report.RemovedPendingFiles++
			}
		}
		return nil
	})

	rows, err := store.db.QueryContext(ctx, `SELECT id,raw_object_path,payload_hash FROM events`)
	if err != nil {
		return report, err
	}
	known := map[string]bool{}
	for rows.Next() {
		var id, path, expected string
		if err := rows.Scan(&id, &path, &expected); err != nil {
			rows.Close()
			return report, err
		}
		known[filepath.Clean(path)] = true
		raw, err := os.ReadFile(path)
		if err != nil {
			report.MissingObjects = append(report.MissingObjects, id)
			continue
		}
		hash := sha256.Sum256(raw)
		if hex.EncodeToString(hash[:]) != expected {
			report.CorruptObjects = append(report.CorruptObjects, id)
		}
	}
	rows.Close()
	_ = filepath.WalkDir(filepath.Join(store.dataDir, "events"), func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && strings.HasSuffix(entry.Name(), ".lep") && !known[filepath.Clean(path)] {
			report.OrphanObjects = append(report.OrphanObjects, path)
		}
		return nil
	})
	sort.Strings(report.MissingObjects)
	sort.Strings(report.CorruptObjects)
	sort.Strings(report.OrphanObjects)
	return report, nil
}

func (store *Store) Prune(ctx context.Context, before time.Time, force bool) (PruneResult, error) {
	if before.IsZero() {
		before = time.Now().UTC().Add(-store.options.DeliveredRetention)
	}
	rows, err := store.db.QueryContext(ctx, `SELECT id,raw_object_path,size_bytes FROM events WHERE received_at<? AND state='DELIVERED'`, before.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return PruneResult{}, err
	}
	type candidate struct {
		id, path string
		size     int64
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.path, &c.size); err != nil {
			rows.Close()
			return PruneResult{}, err
		}
		candidates = append(candidates, c)
	}
	rows.Close()
	if !force && len(candidates) == 0 {
		return PruneResult{}, nil
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return PruneResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result := PruneResult{}
	for _, c := range candidates {
		if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE id=?`, c.id); err != nil {
			return result, err
		}
		if err := os.Remove(c.path); err == nil || os.IsNotExist(err) {
			result.Objects++
			result.Bytes += c.size
		}
		result.Events++
	}
	return result, tx.Commit()
}

func writeDurable(path string, data []byte, fsyncMode string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if fsyncMode != "none" {
		if err := temporary.Sync(); err != nil {
			temporary.Close()
			return err
		}
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	if fsyncMode == "full" {
		// Directory fsync is best-effort: on Windows opening/syncing a directory
		// frequently returns access denied even after a successful rename.
		if directory, err := os.Open(filepath.Dir(path)); err == nil {
			_ = directory.Sync()
			_ = directory.Close()
		}
	}
	return nil
}
