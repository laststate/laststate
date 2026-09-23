package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// AuditChainGap is one place where the hash chain breaks or an entry_hash is
// missing. Returned by VerifyAuditChain in order (chronological).
type AuditChainGap struct {
	Index        int       `json:"index"` // 0-based position in the selected window
	EntryID      uuid.UUID `json:"entry_id"`
	CreatedAt    time.Time `json:"created_at"`
	Reason       string    `json:"reason"` // "missing_prev", "missing_hash", "broken_link", "invalid_hash"
	StoredHash   string    `json:"stored_hash,omitempty"`
	ExpectedHash string    `json:"expected_hash,omitempty"`
	PrevHash     string    `json:"prev_hash,omitempty"`
}

// VerifyAuditChain walks the audit log in chronological order over the window
// [from, to] and returns every gap it finds. An empty slice means the chain
// is intact in that window. limit caps the number of rows scanned; 0 means 10k.
//
// The verifier is tolerant of legacy rows that lack hash columns (they appear
// as "missing_prev" gaps but do not invalidate later rows because their
// prev_hash is empty).
func (s *Store) VerifyAuditChain(ctx context.Context, orgID *uuid.UUID, from, to time.Time, limit int) ([]AuditChainGap, error) {
	if limit <= 0 {
		limit = 10000
	}
	q := `
SELECT id, created_at, COALESCE(prev_hash,''), COALESCE(entry_hash,''), COALESCE(action,''), COALESCE(target_type,''), COALESCE(target_id,''), COALESCE(metadata::text,'{}')
FROM audit_logs
WHERE ($1::uuid IS NULL OR organization_id=$1)
  AND ($2::timestamptz IS NULL OR created_at >= $2)
  AND ($3::timestamptz IS NULL OR created_at <= $3)
ORDER BY created_at ASC, id ASC
LIMIT $4`
	rows, err := s.Pool.Query(ctx, q, orgID, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var gaps []AuditChainGap
	idx := 0
	var lastEntryHash string
	for rows.Next() {
		var id uuid.UUID
		var createdAt time.Time
		var prev, entry, action, targetType, targetID, meta string
		if err := rows.Scan(&id, &createdAt, &prev, &entry, &action, &targetType, &targetID, &meta); err != nil {
			return nil, err
		}
		if entry == "" {
			gaps = append(gaps, AuditChainGap{Index: idx, EntryID: id, CreatedAt: createdAt, Reason: "missing_hash"})
		} else {
			want := computeEntryHash(prev, action, targetType, targetID, meta)
			if want != entry {
				gaps = append(gaps, AuditChainGap{
					Index: idx, EntryID: id, CreatedAt: createdAt,
					Reason: "invalid_hash", StoredHash: entry, ExpectedHash: want, PrevHash: prev,
				})
			}
			if prev != lastEntryHash {
				gaps = append(gaps, AuditChainGap{
					Index: idx, EntryID: id, CreatedAt: createdAt,
					Reason: "broken_link", StoredHash: entry, ExpectedHash: lastEntryHash, PrevHash: prev,
				})
			}
		}
		// Walk the chain only if this row has a hash; otherwise leave
		// lastEntryHash untouched so a legacy block does not poison later
		// rows that were written correctly.
		if entry != "" {
			lastEntryHash = entry
		}
		idx++
	}
	return gaps, rows.Err()
}

// computeEntryHash mirrors the formula used in AuditLog (phase2.go):
//
//	sha256_hex(prev + "|" + action + "|" + targetType + "|" + targetID + "|" + metadata)
func computeEntryHash(prev, action, targetType, targetID, metadata string) string {
	h := sha256.New()
	h.Write([]byte(prev))
	h.Write([]byte{'|'})
	h.Write([]byte(action))
	h.Write([]byte{'|'})
	h.Write([]byte(targetType))
	h.Write([]byte{'|'})
	h.Write([]byte(targetID))
	h.Write([]byte{'|'})
	h.Write([]byte(metadata))
	return hex.EncodeToString(h.Sum(nil))
}

// String helper for the verifier. Keeps error messages stable in tests.
func gapSummary(g AuditChainGap) string {
	return fmt.Sprintf("entry=%s reason=%s", g.EntryID, g.Reason)
}

// ExportAuditLogs exports audit logs as NDJSON for SIEM/S3 ingestion.
// Returns a slice of audit log entries as maps.
func (s *Store) ExportAuditLogs(ctx context.Context, orgID uuid.UUID, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 10000
	}
	if limit > 50000 {
		limit = 50000
	}

	rows, err := s.Pool.Query(ctx, `
SELECT id, created_at, actor_id, action, target_type, target_id,
       COALESCE(metadata::text, '{}') as metadata,
       COALESCE(ip_address, ''),
       COALESCE(user_agent, '')
FROM audit_logs
WHERE organization_id = $1
ORDER BY created_at DESC
LIMIT $2`, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var createdAt time.Time
		var actorID, action, targetType, targetID, metadata, ip, ua *string
		if err := rows.Scan(&id, &createdAt, &actorID, &action, &targetType, &targetID, &metadata, &ip, &ua); err != nil {
			return nil, err
		}
		item := map[string]any{
			"id":          id,
			"created_at":  createdAt.Format(time.RFC3339),
			"action":      action,
			"target_type": targetType,
			"target_id":   targetID,
			"metadata":    metadata,
			"ip_address":  ip,
			"user_agent":  ua,
		}
		if actorID != nil {
			item["actor_id"] = *actorID
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// ExportAuditLogsToNDJSON exports audit logs as NDJSON bytes for direct S3 upload.
func (s *Store) ExportAuditLogsToNDJSON(ctx context.Context, orgID uuid.UUID, limit int) ([]byte, error) {
	entries, err := s.ExportAuditLogs(ctx, orgID, limit)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	// Simple NDJSON encoding
	var buf []byte
	for _, entry := range entries {
		line, err := json.Marshal(entry)
		if err != nil {
			return nil, err
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	return buf, nil
}
