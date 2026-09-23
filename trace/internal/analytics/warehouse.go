// Package analytics provides a lightweight "warehouse-shaped" export path
// (NDJSON batches) so fleets can pipe into ClickHouse, BigQuery, or S3 without
// requiring those systems inside Trace.
package analytics

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/objects"
	"github.com/laststate/trace/internal/store"
)

type Sink interface {
	WriteNDJSON(ctx context.Context, projectID uuid.UUID, rows []map[string]any) (objectKey string, n int, err error)
}

// FileSink writes under ObjectDir/analytics/
type FileSink struct {
	Root string
}

func (f FileSink) WriteNDJSON(ctx context.Context, projectID uuid.UUID, rows []map[string]any) (string, int, error) {
	dir := filepath.Join(f.Root, "analytics", projectID.String())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, err
	}
	name := fmt.Sprintf("events-%s.ndjson", time.Now().UTC().Format("20060102T150405"))
	path := filepath.Join(dir, name)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			return "", 0, err
		}
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return "", 0, err
	}
	key := filepath.ToSlash(filepath.Join("analytics", projectID.String(), name))
	return key, len(rows), nil
}

// ObjectSink stores NDJSON via objects.Store (content-addressed Put).
type ObjectSink struct {
	Objects *objects.Store
}

func (o ObjectSink) WriteNDJSON(ctx context.Context, projectID uuid.UUID, rows []map[string]any) (string, int, error) {
	if o.Objects == nil {
		return "", 0, fmt.Errorf("no object store")
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, r := range rows {
		_ = enc.Encode(r)
	}
	key, _, err := o.Objects.Put(buf.Bytes())
	if err != nil {
		return "", 0, err
	}
	return key, len(rows), nil
}

// ClickHouseSink writes NDJSON to ClickHouse via its native protocol.
// Requires TRACE_CLICKHOUSE_URL=clickhouse://host:9000 and TRACE_CLICKHOUSE_DB.
// Table schema: events (id UUID, event_id String, severity String, state String,
// pipeline String, received_at DateTime64(3), fingerprint String,
// architecture Int16, device_id String, release String)
type ClickHouseSink struct {
	DSN string
	// DB is the database name (defaults to "trace")
	DB string
	// Table is the table name (defaults to "events")
	Table string
	// BatchSize controls how many rows are sent per INSERT (default 1000)
	BatchSize int
}

func (c *ClickHouseSink) WriteNDJSON(ctx context.Context, projectID uuid.UUID, rows []map[string]any) (string, int, error) {
	if c.DSN == "" {
		return "", 0, fmt.Errorf("clickhouse: TRACE_CLICKHOUSE_URL not configured")
	}
	db, err := sql.Open("clickhouse", c.DSN)
	if err != nil {
		return "", 0, fmt.Errorf("clickhouse connect: %w", err)
	}
	defer db.Close()

	// Ensure table exists
	table := c.Table
	if table == "" {
		table = "events"
	}
	createTable := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s.%s (
			id UUID,
			event_id String,
			severity String,
			state String,
			pipeline String,
			received_at DateTime64(3),
			fingerprint String,
			architecture Int16,
			device_id String,
			release String,
			analysis String
		) ENGINE = ReplacingMergeTree(received_at)
		ORDER BY (project_id, event_id)`,
		c.DB, table)
	if _, err := db.ExecContext(ctx, createTable); err != nil {
		// Table might not exist or schema mismatch — log and continue
		// In production, migrations should handle this
	}

	batchSize := c.BatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	n := 0
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			return "", 0, err
		}
		n++
		if n%batchSize == 0 {
			if err := c.flush(ctx, db, buf.Bytes()); err != nil {
				return "", 0, fmt.Errorf("clickhouse flush: %w", err)
			}
			buf.Reset()
		}
	}
	if buf.Len() > 0 {
		if err := c.flush(ctx, db, buf.Bytes()); err != nil {
			return "", 0, fmt.Errorf("clickhouse flush: %w", err)
		}
	}

	return fmt.Sprintf("clickhouse://%s/%s/%s", c.DB, table, time.Now().UTC().Format("20060102T150405")), n, nil
}

func (c *ClickHouseSink) flush(ctx context.Context, db *sql.DB, data []byte) error {
	table := c.Table
	if table == "" {
		table = "events"
	}
	// ClickHouse INSERT with FORMAT JSONEachRow
	query := fmt.Sprintf("INSERT INTO %s.%s FORMAT JSONEachRow", c.DB, table)
	_, err := db.ExecContext(ctx, query, data)
	return err
}

// ExportRecent dumps project events since `since` through sink and logs the export.
func ExportRecent(ctx context.Context, st *store.Store, sink Sink, projectID uuid.UUID, since time.Time, limit int) (string, int, error) {
	rows, err := st.ExportEventsNDJSON(ctx, projectID, since, limit)
	if err != nil {
		return "", 0, err
	}
	if len(rows) == 0 {
		return "", 0, nil
	}
	key, n, err := sink.WriteNDJSON(ctx, projectID, rows)
	if err != nil {
		return "", 0, err
	}
	_ = st.LogAnalyticsExport(ctx, projectID, "events_ndjson", key, n)
	return key, n, nil
}

// ExportEventsToClickHouse is a convenience wrapper for ClickHouse export.
// It reads events from the store and writes them to ClickHouse.
func ExportEventsToClickHouse(ctx context.Context, st *store.Store, ch *ClickHouseSink, projectID uuid.UUID, since time.Time, limit int) (string, int, error) {
	if ch == nil {
		return "", 0, fmt.Errorf("clickhouse sink not configured")
	}
	return ExportRecent(ctx, st, ch, projectID, since, limit)
}

// FormatClickHouseDSN formats a ClickHouse connection string from components.
func FormatClickHouseDSN(host, port, user, password, database string, secure bool) string {
	scheme := "clickhouse"
	if secure {
		scheme = "clickhouse+tls"
	}
	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "9000"
	}
	dsn := fmt.Sprintf("%s://%s:%s@%s:%s", scheme, user, password, host, port)
	if database != "" {
		dsn += "?database=" + database
	}
	return dsn
}

// ValidateClickHouseDSN checks if the ClickHouse DSN is valid and accessible.
func ValidateClickHouseDSN(ctx context.Context, dsn string) error {
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return fmt.Errorf("clickhouse connect: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("clickhouse ping: %w", err)
	}
	return nil
}

// ClickHouseSchema returns the DDL for the Trace events table.
func ClickHouseSchema(database, table string) string {
	if database == "" {
		database = "trace"
	}
	if table == "" {
		table = "events"
	}
	return fmt.Sprintf(`
CREATE DATABASE IF NOT EXISTS %s;

CREATE TABLE IF NOT EXISTS %s.%s (
    id UUID,
    event_id String,
    severity String,
    state String,
    pipeline String,
    received_at DateTime64(3),
    fingerprint String,
    architecture Int16,
    device_id String,
    release String,
    analysis String,
    project_id UUID
) ENGINE = ReplacingMergeTree(received_at)
ORDER BY (project_id, event_id);

-- Materialized view for hourly aggregation
CREATE MATERIALIZED VIEW IF NOT EXISTS %s.events_hourly
ENGINE = SummingMergeTree()
ORDER BY (project_id, hour)
AS SELECT
    project_id,
    toStartOfHour(received_at) AS hour,
    count() AS total,
    sum(severity = 'fatal') AS fatal,
    sum(severity = 'error') AS error,
    sum(severity = 'warning') AS warning
FROM %s.%s
GROUP BY project_id, hour;
`, database, database, table, database, database, table)
}

// ClickHouseExportJob represents a scheduled ClickHouse export job.
type ClickHouseExportJob struct {
	ProjectID uuid.UUID
	Database  string
	Table     string
	Hours     int
	Cron      string // e.g., "0 */6 * * *" for every 6 hours
	LastRun   *time.Time
	LastRows  int
}

// ParseClickHouseDSN parses a ClickHouse DSN into components.
func ParseClickHouseDSN(dsn string) (host, port, user, password, database string, err error) {
	dsn = strings.TrimPrefix(dsn, "clickhouse://")
	dsn = strings.TrimPrefix(dsn, "clickhouse+tls://")
	dsn = strings.TrimPrefix(dsn, "clickhouse://")

	// Parse user:password@host:port
	atIdx := strings.Index(dsn, "@")
	if atIdx >= 0 {
		auth := dsn[:atIdx]
		hostPart := dsn[atIdx+1:]

		// Check for user:password
		colonIdx := strings.Index(auth, ":")
		if colonIdx >= 0 {
			user = auth[:colonIdx]
			password = auth[colonIdx+1:]
		} else {
			user = auth
		}

		// Parse host:port
		hostPort := strings.Split(hostPart, ":")
		host = hostPort[0]
		if len(hostPort) > 1 {
			port = hostPort[1]
		}
	} else {
		host = dsn
	}

	// Parse ?database=xxx
	if idx := strings.Index(dsn, "?database="); idx >= 0 {
		database = dsn[idx+10:]
	}

	return host, port, user, password, database, nil
}
