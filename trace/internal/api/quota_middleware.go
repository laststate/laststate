package api

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/billing"
)

// quotaMiddleware is the middleware that enforces billing quotas on the ingest
// path. It checks the org's subscription state and event count before allowing
// the request through.
//
// If the org is suspended or past due (without grace period), the middleware
// returns 429 with a Retry-After header. If the org has exceeded its monthly
// event quota, the middleware returns 429 with the remaining headroom in the
// response body.
//
// The middleware is installed on /v1/ingest and /v1/events but NOT on
// /v1/artifacts (which has separate storage quotas) or /v1/relay/heartbeat
// (which is control-plane traffic).
func (s *Server) quotaMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Local deployment mode: everything unlocked — no quotas, no billing.
		if s.Cfg.IsLocal() {
			next.ServeHTTP(w, r)
			return
		}
		// Require a session to enforce quotas. Unauthenticated ingest (API
		// token based) skips quota checks — this is a known limitation;
		// proper enforcement requires per-token quota tracking.
		if _, ok := sessionFrom(s, r); !ok {
			next.ServeHTTP(w, r)
			return
		}
		tenant, ok := orgFrom(r)
		if !ok || tenant.OrgID == uuid.Nil {
			next.ServeHTTP(w, r)
			return
		}
		if err := billing.CheckEvents(r.Context(), s.Store, tenant.OrgID, time.Now()); err != nil {
			if billing.IsQuotaExceeded(err) {
				writeErr(w, 429, "quota_exceeded", err.Error(), false)
				return
			}
			// Other billing errors (e.g. unknown plan tier) are treated as
			// 500 to surface operational issues.
			writeErr(w, 500, "internal", err.Error(), true)
			return
		}
		next.ServeHTTP(w, r)
	})
}
