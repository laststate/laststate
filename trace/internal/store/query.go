package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// EventQuery is a tiny Sentry-like filter language.
// Examples:
//
//	severity:fatal device:abc* has:stack age:<24h
//	status:open pipeline:issue
type EventQuery struct {
	Raw        string
	Severity   string
	Status     string
	Pipeline   string
	Device     string
	Release    string
	HasStack   bool
	HasAssert  bool
	Text       string
	Age        time.Duration // max age
	SampleRate float64       // 0..1, applied in app after fetch when >0 and <1
	Limit      int
}

// ParseEventQuery parses key:value tokens and free text.
func ParseEventQuery(q string) EventQuery {
	eq := EventQuery{Raw: q, Limit: 50}
	parts := strings.Fields(q)
	var text []string
	for _, p := range parts {
		if k, v, ok := strings.Cut(p, ":"); ok {
			switch strings.ToLower(k) {
			case "severity", "sev":
				eq.Severity = v
			case "status":
				eq.Status = v
			case "pipeline":
				eq.Pipeline = v
			case "device":
				eq.Device = strings.Trim(v, "*")
			case "release", "version":
				eq.Release = v
			case "has":
				switch strings.ToLower(v) {
				case "stack":
					eq.HasStack = true
				case "assert":
					eq.HasAssert = true
				}
			case "age":
				eq.Age = parseAge(v)
			case "sample":
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					eq.SampleRate = f
				}
			case "limit":
				if n, err := strconv.Atoi(v); err == nil && n > 0 {
					eq.Limit = n
				}
			default:
				text = append(text, p)
			}
			continue
		}
		text = append(text, p)
	}
	eq.Text = strings.Join(text, " ")
	return eq
}

func parseAge(v string) time.Duration {
	v = strings.TrimPrefix(v, "<")
	v = strings.TrimPrefix(v, ">")
	if len(v) < 2 {
		return 0
	}
	unit := v[len(v)-1]
	n, err := strconv.Atoi(v[:len(v)-1])
	if err != nil || n <= 0 {
		return 0
	}
	switch unit {
	case 'h', 'H':
		return time.Duration(n) * time.Hour
	case 'd', 'D':
		return time.Duration(n) * 24 * time.Hour
	case 'm', 'M':
		return time.Duration(n) * time.Minute
	default:
		return 0
	}
}

// QueryEvents runs EventQuery against project events.
func (s *Store) QueryEvents(ctx context.Context, projectID uuid.UUID, q EventQuery) ([]map[string]any, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	if q.Limit > 200 {
		q.Limit = 200
	}
	var conds []string
	args := []any{projectID}
	n := 2
	conds = append(conds, "e.project_id=$1")
	if q.Severity != "" {
		conds = append(conds, fmt.Sprintf("e.severity=$%d", n))
		args = append(args, q.Severity)
		n++
	}
	if q.Pipeline != "" {
		conds = append(conds, fmt.Sprintf("COALESCE(e.pipeline,'issue')=$%d", n))
		args = append(args, q.Pipeline)
		n++
	}
	if q.Device != "" {
		conds = append(conds, fmt.Sprintf("d.device_id ILIKE $%d", n))
		args = append(args, "%"+q.Device+"%")
		n++
	}
	if q.Release != "" {
		conds = append(conds, fmt.Sprintf("(r.version ILIKE $%d OR r.build_id ILIKE $%d)", n, n))
		args = append(args, "%"+q.Release+"%")
		n++
	}
	if q.HasStack {
		conds = append(conds, `e.frames IS NOT NULL AND e.frames::text NOT IN ('null','[]')`)
	}
	if q.HasAssert {
		conds = append(conds, `(e.decoded::text ILIKE '%assert%' OR e.analysis::text ILIKE '%assert%')`)
	}
	if q.Age > 0 {
		conds = append(conds, fmt.Sprintf("e.received_at > now() - ($%d || ' seconds')::interval", n))
		args = append(args, int(q.Age.Seconds()))
		n++
	}
	if q.Text != "" {
		conds = append(conds, fmt.Sprintf("(e.event_id ILIKE $%d OR e.analysis::text ILIKE $%d)", n, n))
		args = append(args, "%"+q.Text+"%")
		n++
	}
	args = append(args, q.Limit)
	sql := fmt.Sprintf(`
SELECT e.id, e.event_id, e.severity, e.state, e.pipeline, e.received_at,
  d.device_id, r.version
FROM events e
LEFT JOIN devices d ON d.id = e.device_id
LEFT JOIN releases r ON r.id = e.release_id
WHERE %s
ORDER BY e.received_at DESC
LIMIT $%d`, strings.Join(conds, " AND "), n)

	rows, err := s.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var eventID, sev, state string
		var pipeline *string
		var ts time.Time
		var deviceID, version *string
		if err := rows.Scan(&id, &eventID, &sev, &state, &pipeline, &ts, &deviceID, &version); err != nil {
			return nil, err
		}
		pipe := "issue"
		if pipeline != nil {
			pipe = *pipeline
		}
		item := map[string]any{
			"id": id, "event_id": eventID, "severity": sev, "state": state,
			"pipeline": pipe, "received_at": ts,
		}
		if deviceID != nil {
			item["device_id"] = *deviceID
		}
		if version != nil {
			item["release"] = *version
		}
		// client-side sample if requested
		if q.SampleRate > 0 && q.SampleRate < 1 {
			// deterministic-ish sample by id bytes
			if float64(id[15])/255.0 > q.SampleRate {
				continue
			}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
