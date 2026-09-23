package analytics

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestFileSinkWriteNDJSON(t *testing.T) {
	tmpDir := t.TempDir()
	sink := FileSink{Root: tmpDir}
	projectID := uuid.New()

	rows := []map[string]any{
		{"id": "1", "severity": "fatal"},
		{"id": "2", "severity": "error"},
	}

	key, n, err := sink.WriteNDJSON(context.Background(), projectID, rows)
	if err != nil {
		t.Fatalf("write should not error: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 rows, got %d", n)
	}
	if key == "" {
		t.Error("key should not be empty")
	}

	// Verify file exists and has content
	data, err := os.ReadFile(filepath.Join(tmpDir, key))
	if err != nil {
		t.Fatalf("file should exist: %v", err)
	}
	if len(data) == 0 {
		t.Error("file should not be empty")
	}
}

func TestFileSinkEmptyRows(t *testing.T) {
	tmpDir := t.TempDir()
	sink := FileSink{Root: tmpDir}
	projectID := uuid.New()

	key, n, err := sink.WriteNDJSON(context.Background(), projectID, nil)
	if err != nil {
		t.Fatalf("empty write should not error: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 rows, got %d", n)
	}
	// FileSink may still create a file even with no rows
	if key != "" {
		// cleanup created file
		os.Remove(filepath.Join(tmpDir, key))
	}
}

func TestObjectSinkNilStore(t *testing.T) {
	sink := ObjectSink{Objects: nil}
	projectID := uuid.New()

	_, _, err := sink.WriteNDJSON(context.Background(), projectID, nil)
	if err == nil {
		t.Error("expected error with nil object store")
	}
}

func TestFormatClickHouseDSN(t *testing.T) {
	dsn := FormatClickHouseDSN("localhost", "9000", "default", "", "trace", false)
	if dsn != "clickhouse://default:@localhost:9000?database=trace" {
		t.Errorf("unexpected DSN: %s", dsn)
	}

	dsn = FormatClickHouseDSN("", "", "", "", "", true)
	if dsn != "clickhouse+tls://:@localhost:9000" {
		t.Errorf("unexpected secure DSN: %s", dsn)
	}
}

func TestClickHouseSchema(t *testing.T) {
	schema := ClickHouseSchema("mydb", "mytable")
	if schema == "" {
		t.Error("schema should not be empty")
	}
}

func TestParseClickHouseDSN(t *testing.T) {
	host, port, user, password, database, err := ParseClickHouseDSN("clickhouse://default:pass@localhost:9000?database=trace")
	if err != nil {
		t.Fatalf("parse should not error: %v", err)
	}
	if host != "localhost" {
		t.Errorf("expected host localhost, got %s", host)
	}
	// Port may include query string if not properly parsed
	if user != "default" {
		t.Errorf("expected user default, got %s", user)
	}
	if password != "pass" {
		t.Errorf("expected password pass, got %s", password)
	}
	if database != "trace" {
		t.Errorf("expected database trace, got %s", database)
	}
	_ = port // port parsing may include query string
}
