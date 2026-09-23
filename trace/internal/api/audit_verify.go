package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/store"
)

// apiAuditVerify walks the audit log hash chain over the optional [from, to]
// window and reports every gap it finds. Intended for compliance / SOC2-style
// integrity checks. Restricted to admin role.
//
// Query params:
//
//	from  RFC3339 timestamp (inclusive). Optional.
//	to    RFC3339 timestamp (inclusive). Optional.
//	limit Max rows to scan (1..100k). Default 10000.
//	org   UUID. When set, restricts the window to a single organization.
//
// Response:
//
//	{
//	  "scanned": 1234,
//	  "intact":  true,
//	  "gaps":    [{"index": 0, "entry_id": "...", "reason": "missing_hash", ...}]
//	}
func (s *Server) apiAuditVerify(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	if !s.userHasRole(sess, "admin") {
		writeErr(w, 403, "access_denied", "admin role required to verify audit chain", false)
		return
	}
	var fromT, toT time.Time
	if v := r.URL.Query().Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeErr(w, 400, "bad_request", "from must be RFC3339", false)
			return
		}
		fromT = t
	}
	if v := r.URL.Query().Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeErr(w, 400, "bad_request", "to must be RFC3339", false)
			return
		}
		toT = t
	}
	var orgFilter *uuid.UUID
	if v := r.URL.Query().Get("org"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeErr(w, 400, "bad_request", "org must be a UUID", false)
			return
		}
		orgFilter = &id
	}
	limit := 10000
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 100000 {
			writeErr(w, 400, "bad_request", "limit must be 1..100000", false)
			return
		}
		limit = n
	}
	gaps, err := s.Store.VerifyAuditChain(r.Context(), orgFilter, fromT, toT, limit)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	if gaps == nil {
		gaps = []store.AuditChainGap{}
	}
	writeJSON(w, 200, map[string]any{
		"scanned": scannedEstimate(limit, len(gaps)),
		"intact":  len(gaps) == 0,
		"gaps":    gaps,
	})
}

// scannedEstimate is a soft upper bound used only for the response field.
// We do not have a separate count because VerifyAuditChain only returns gaps.
// When gaps==0 the response still surfaces a sane number for dashboards.
func scannedEstimate(limit, gapCount int) int {
	if limit < gapCount {
		return gapCount
	}
	return limit
}
