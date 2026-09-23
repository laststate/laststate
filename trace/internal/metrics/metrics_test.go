package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/laststate/trace/internal/metrics"
)

func TestMetricsHandler(t *testing.T) {
	metrics.IngestTotal.Add(1)
	metrics.JobsCompleted.Add(2)
	rr := httptest.NewRecorder()
	metrics.Handler(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rr.Code != 200 {
		t.Fatal(rr.Code)
	}
	body := rr.Body.String()
	for _, s := range []string{
		"trace_ingest_total",
		"trace_jobs_completed_total",
		"trace_up 1",
		"trace_start_time_seconds",
	} {
		if !strings.Contains(body, s) {
			t.Fatalf("missing %s in %s", s, body)
		}
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Fatal(ct)
	}
}
