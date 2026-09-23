package metrics

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// Process-local counters (Prometheus text format).
var (
	IngestTotal     atomic.Int64
	IngestAccepted  atomic.Int64
	IngestDuplicate atomic.Int64
	IngestRejected  atomic.Int64
	JobsCompleted   atomic.Int64
	JobsFailed      atomic.Int64
	JobsActive      atomic.Int64
	ArtifactUploads atomic.Int64
	WebhookOK       atomic.Int64
	WebhookFail     atomic.Int64
	GCDeleted       atomic.Int64
	AuthFail        atomic.Int64
	startUnix       = time.Now().Unix()
)

func Handler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	write := func(name, help string, v int64) {
		_, _ = fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n%s %d\n", name, help, name, name, v)
	}
	write("trace_ingest_total", "Ingest requests", IngestTotal.Load())
	write("trace_ingest_accepted_total", "Accepted events", IngestAccepted.Load())
	write("trace_ingest_duplicate_total", "Duplicate events", IngestDuplicate.Load())
	write("trace_ingest_rejected_total", "Rejected events", IngestRejected.Load())
	write("trace_jobs_completed_total", "Completed jobs", JobsCompleted.Load())
	write("trace_jobs_failed_total", "Failed jobs", JobsFailed.Load())
	write("trace_artifact_uploads_total", "Artifact uploads", ArtifactUploads.Load())
	write("trace_webhook_ok_total", "Successful webhook deliveries", WebhookOK.Load())
	write("trace_webhook_fail_total", "Failed webhook deliveries", WebhookFail.Load())
	write("trace_gc_deleted_total", "GC deleted objects", GCDeleted.Load())
	write("trace_auth_fail_total", "Auth failures", AuthFail.Load())
	_, _ = fmt.Fprintf(w, "# HELP trace_up 1 if process is up\n# TYPE trace_up gauge\ntrace_up 1\n")
	_, _ = fmt.Fprintf(w, "# HELP trace_start_time_seconds Process start time\n# TYPE trace_start_time_seconds gauge\ntrace_start_time_seconds %d\n", startUnix)
	_, _ = fmt.Fprintf(w, "# HELP process_start_time_seconds Process start time\n# TYPE process_start_time_seconds gauge\nprocess_start_time_seconds %d\n", startUnix)
}
