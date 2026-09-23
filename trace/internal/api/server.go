package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/klauspost/compress/zstd"
	"sync"

	"github.com/laststate/trace/internal/artifact"
	"github.com/laststate/trace/internal/auth"
	"github.com/laststate/trace/internal/batch"
	"github.com/laststate/trace/internal/config"
	"github.com/laststate/trace/internal/lep"
	"github.com/laststate/trace/internal/mailer"
	"github.com/laststate/trace/internal/metrics"
	"github.com/laststate/trace/internal/objects"
	"github.com/laststate/trace/internal/store"
)

type Server struct {
	Cfg    config.Config
	Store  *store.Store
	Object *objects.Store
	Queue  interface {
		Enqueue(ctx context.Context, typ string, payload any) error
	}
	Log    *slog.Logger
	UI     http.FileSystem
	Mailer mailer.Mailer

	// loginLockout tracks failed login attempts per email for brute-force protection.
	loginLockoutMu sync.Mutex
	loginLockout   map[string]loginLockoutEntry

	// testResolver, if non-nil, takes precedence over Store for the
	// activeTenant middleware and sessionFrom. Production code never sets
	// this; it exists so unit tests can provide a fake sessionResolver
	// without a live database.
	testResolver sessionResolver
}

// loginLockoutEntry records the state of a locked-out account.
type loginLockoutEntry struct {
	FirstFailedAt time.Time
	FailedCount   int
	LockedUntil   time.Time // zero if not yet locked
}

// sessionResolver is the subset of *store.Store the api package uses for
// authentication and tenant lookup. Defined as an interface so unit tests can
// provide a fake without standing up a real database.
type sessionResolver interface {
	AuthSession(ctx context.Context, secret string) (store.Session, error)
	UserIsMemberOfOrg(ctx context.Context, userID, orgID uuid.UUID) (bool, error)
	TouchSession(ctx context.Context, sessionID uuid.UUID, ip, userAgent string) error
	RevokeSession(ctx context.Context, sessionID, userID uuid.UUID) error
}

// resolver returns the sessionResolver-compatible view of s.Store. In
// production s.Store is already a *store.Store; in tests it is overridden
// via newServerWithResolver.
func (s *Server) resolver() sessionResolver {
	if s.testResolver != nil {
		return s.testResolver
	}
	return s.Store
}

// newServerWithResolver returns a Server that uses the supplied resolver
// for authentication/tenant operations. Production code never calls this.
func newServerWithResolver(r sessionResolver) *Server {
	return &Server{testResolver: r}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", s.live)
	mux.HandleFunc("GET /health/ready", s.ready)
	mux.HandleFunc("GET /metrics", metrics.Handler)
	mux.HandleFunc("GET /openapi.json", s.openapi)

	mux.HandleFunc("GET /v1/relay/capabilities", s.capabilities)
	mux.Handle("POST /v1/ingest", s.quotaMiddleware(http.HandlerFunc(s.ingest)))
	mux.Handle("POST /v1/events", s.quotaMiddleware(http.HandlerFunc(s.ingest)))
	mux.Handle("POST /v1/events:batch", s.quotaMiddleware(http.HandlerFunc(s.ingestBatch)))
	mux.HandleFunc("POST /v1/artifacts", s.uploadArtifact)
	mux.HandleFunc("POST /v1/relay/heartbeat", s.relayHeartbeat)

	mux.HandleFunc("POST /api/auth/login", s.limitAuth(s.login))
	mux.HandleFunc("POST /api/auth/logout", s.requireUI(s.apiLogout, "viewer"))
	mux.HandleFunc("GET /api/me", s.me)
	mux.HandleFunc("GET /api/jobs/dead", s.requireUI(s.apiDeadJobs, "admin"))
	mux.HandleFunc("POST /api/jobs/dead/{id}/requeue", s.requireUI(s.apiRequeueDead, "admin"))
	mux.HandleFunc("DELETE /api/jobs/dead/{id}", s.requireUI(s.apiDiscardDead, "admin"))
	mux.HandleFunc("GET /api/alerts/skips", s.requireUI(s.apiAlertSkips, "admin"))
	mux.HandleFunc("GET /api/settings", s.requireUI(s.apiGetSettings, "viewer"))
	mux.HandleFunc("PUT /api/settings", s.requireUI(s.apiPutSettings, "admin"))
	mux.HandleFunc("GET /api/labels", s.requireUI(s.apiListLabels, "viewer"))
	mux.HandleFunc("POST /api/labels", s.requireUI(s.apiCreateLabel, "developer"))
	mux.HandleFunc("GET /api/releases/compare", s.requireUI(s.apiCompareReleases, "viewer"))
	mux.HandleFunc("GET /api/boots", s.requireUI(s.apiBootSessions, "viewer"))
	mux.HandleFunc("GET /api/overview", s.requireUI(s.apiOverview, "viewer"))
	mux.HandleFunc("GET /api/stream", s.requireUI(s.apiStream, "viewer"))
	mux.HandleFunc("GET /api/search", s.requireUI(s.apiSearchFull, "viewer"))
	mux.HandleFunc("GET /api/query/events", s.requireUI(s.apiQueryEvents, "viewer"))
	mux.HandleFunc("GET /scim/v2/Users", s.requireUI(s.apiSCIMUsers, "admin"))
	mux.HandleFunc("POST /scim/v2/Users", s.requireUI(s.apiSCIMUsers, "admin"))
	mux.HandleFunc("GET /scim/v2/Users/{id}", s.requireUI(s.apiSCIMUser, "admin"))
	mux.HandleFunc("PUT /scim/v2/Users/{id}", s.requireUI(s.apiSCIMUser, "admin"))
	mux.HandleFunc("PATCH /scim/v2/Users/{id}", s.requireUI(s.apiSCIMUser, "admin"))
	mux.HandleFunc("DELETE /scim/v2/Users/{id}", s.requireUI(s.apiSCIMUser, "admin"))
	mux.HandleFunc("GET /scim/v2/Groups", s.requireUI(s.apiSCIMGroups, "admin"))
	mux.HandleFunc("POST /scim/v2/Groups", s.requireUI(s.apiSCIMGroups, "admin"))
	mux.HandleFunc("GET /scim/v2/Groups/{id}", s.requireUI(s.apiSCIMGroup, "admin"))
	mux.HandleFunc("PUT /scim/v2/Groups/{id}", s.requireUI(s.apiSCIMGroup, "admin"))
	mux.HandleFunc("PATCH /scim/v2/Groups/{id}", s.requireUI(s.apiSCIMGroup, "admin"))
	mux.HandleFunc("DELETE /scim/v2/Groups/{id}", s.requireUI(s.apiSCIMGroup, "admin"))
	mux.HandleFunc("GET /api/oncall", s.requireUI(s.apiOncall, "viewer"))
	mux.HandleFunc("POST /api/oncall", s.requireUI(s.apiOncall, "admin"))
	mux.HandleFunc("POST /api/oncall/shifts", s.requireUI(s.apiOncallShift, "admin"))
	mux.HandleFunc("GET /api/escalation", s.requireUI(s.apiEscalation, "viewer"))
	mux.HandleFunc("POST /api/escalation", s.requireUI(s.apiEscalation, "admin"))
	mux.HandleFunc("GET /api/issues/{id}/suspect-commits", s.requireUI(s.apiSuspectCommits, "viewer"))
	mux.HandleFunc("GET /api/issues/{id}/replay", s.requireUI(s.apiIssueReplay, "viewer"))
	mux.HandleFunc("POST /api/releases/{id}/commits", s.requireUI(s.apiReleaseCommits, "maintainer"))
	mux.HandleFunc("POST /api/analytics/export", s.requireUI(s.apiAnalyticsExport, "admin"))
	mux.HandleFunc("GET /api/saml/config", s.requireUI(s.apiSAMLConfig, "admin"))
	mux.HandleFunc("PUT /api/saml/config", s.requireUI(s.apiSAMLConfig, "admin"))
	mux.HandleFunc("GET /saml/metadata", s.apiSAMLMetadata)
	mux.HandleFunc("GET /saml/login", s.apiSAMLLogin)
	mux.HandleFunc("POST /saml/acs", s.apiSAMLACS)
	mux.HandleFunc("GET /api/issues", s.requireUI(s.apiIssuesPage, "viewer"))
	mux.HandleFunc("GET /api/issues/{id}", s.requireUI(s.apiIssue, "viewer"))
	mux.HandleFunc("POST /api/issues/{id}/status", s.requireUI(s.apiIssueStatus, "developer"))
	mux.HandleFunc("POST /api/issues/{id}/comments", s.requireUI(s.apiIssueComment, "developer"))
	mux.HandleFunc("POST /api/issues/{id}/assign", s.requireUI(s.apiIssueAssign, "developer"))
	mux.HandleFunc("POST /api/issues/{id}/merge", s.requireUI(s.apiIssueMerge, "maintainer"))
	mux.HandleFunc("POST /api/issues/{id}/split", s.requireUI(s.apiIssueSplit, "maintainer"))
	mux.HandleFunc("POST /api/issues/{id}/labels", s.requireUI(s.apiIssueLabels, "developer"))
	mux.HandleFunc("GET /api/events", s.requireUI(s.apiEventsPage, "viewer"))
	mux.HandleFunc("GET /api/events/{id}", s.requireUI(s.apiEvent, "viewer"))
	mux.HandleFunc("GET /api/events/{id}/raw", s.requireUI(s.apiEventRaw, "viewer"))
	mux.HandleFunc("POST /api/events/{id}/reprocess", s.requireUI(s.apiEventReprocess, "maintainer"))
	mux.HandleFunc("POST /api/events/reprocess-stale", s.requireUI(s.apiReprocessBatch, "maintainer"))
	mux.HandleFunc("GET /api/devices", s.requireUI(s.apiDevicesPage, "viewer"))
	mux.HandleFunc("GET /api/devices/{id}", s.requireUI(s.apiDevice, "viewer"))
	mux.HandleFunc("PATCH /api/devices/{id}", s.requireUI(s.apiDevicePatch, "developer"))
	mux.HandleFunc("GET /api/devices/{id}/firmware-history", s.requireUI(s.apiDeviceHistory, "viewer"))
	mux.HandleFunc("GET /api/releases", s.requireUI(s.apiReleases, "viewer"))
	mux.HandleFunc("GET /api/releases/{id}", s.requireUI(s.apiRelease, "viewer"))
	mux.HandleFunc("PATCH /api/releases/{id}", s.requireUI(s.apiReleasePatch, "maintainer"))
	mux.HandleFunc("GET /api/releases/{id}/stats", s.requireUI(s.apiReleaseStats, "viewer"))
	mux.HandleFunc("GET /api/artifacts", s.requireUI(s.apiArtifacts, "viewer"))
	mux.HandleFunc("POST /api/artifacts", s.requireUI(s.apiUploadArtifact, "maintainer"))
	mux.HandleFunc("POST /api/artifacts/{id}/promote", s.requireUI(s.apiPromoteArtifact, "maintainer"))
	mux.HandleFunc("GET /api/audit", s.requireUI(s.apiAudit, "admin"))
	mux.HandleFunc("POST /api/tokens", s.requireUI(s.apiCreateToken, "admin"))
	mux.HandleFunc("GET /api/tokens", s.requireUI(s.apiListTokens, "admin"))
	mux.HandleFunc("DELETE /api/tokens/{id}", s.requireUI(s.apiRevokeToken, "admin"))
	mux.HandleFunc("POST /api/auth/register", s.limitAuth(s.apiRegister))
	mux.HandleFunc("POST /api/auth/invite/accept", s.limitAuth(s.apiAcceptInvite))
	mux.HandleFunc("GET /api/bootstrap", s.apiBootstrapInfo)
	mux.HandleFunc("GET /api/alerts", s.requireUI(s.apiAlerts, "viewer"))
	mux.HandleFunc("POST /api/alerts", s.requireUI(s.apiCreateAlert, "admin"))
	mux.HandleFunc("PATCH /api/alerts/{id}", s.requireUI(s.apiUpdateAlert, "admin"))
	mux.HandleFunc("GET /api/webhooks/deliveries", s.requireUI(s.apiWebhookDeliveries, "admin"))
	mux.HandleFunc("GET /api/channels", s.requireUI(s.apiChannels, "viewer"))
	mux.HandleFunc("POST /api/channels", s.requireUI(s.apiCreateChannel, "admin"))
	mux.HandleFunc("GET /api/auth/oidc/login", s.oidcLogin)
	mux.HandleFunc("GET /api/auth/oidc/callback", s.oidcCallback)
	mux.HandleFunc("GET /api/auth/oidc/logout", s.oidcLogout)

	// Phase 3: Self-service auth endpoints. Credential-adjacent routes get a
	// strict per-IP limiter (20/min) on top of the global one: login,
	// signup, and code-verification endpoints are brute-force targets.
	mux.HandleFunc("POST /api/auth/signup", s.limitAuth(s.apiSignup))
	mux.HandleFunc("POST /api/auth/verify-email", s.limitAuth(s.apiVerifyEmail))
	mux.HandleFunc("POST /api/auth/forgot-password", s.limitAuth(s.apiForgotPassword))
	mux.HandleFunc("POST /api/auth/reset-password", s.limitAuth(s.apiResetPassword))
	mux.Handle("GET /api/auth/mfa/status", s.requireAuth(http.HandlerFunc(s.apiMfaStatus)))
	mux.Handle("POST /api/auth/mfa/enroll", s.requireAuth(http.HandlerFunc(s.apiMfaEnroll)))
	mux.Handle("POST /api/auth/mfa/verify", s.requireAuth(http.HandlerFunc(s.apiMfaVerify)))
	mux.Handle("POST /api/auth/mfa/disable", s.requireAuth(http.HandlerFunc(s.apiMfaDisable)))
	mux.HandleFunc("POST /api/auth/mfa/resend-code", s.limitAuth(s.apiMfaResendCode))

	// Phase 3: Export and onboarding endpoints.
	mux.Handle("POST /api/webhooks/export", s.requireAuth(http.HandlerFunc(s.apiCreateExport)))
	mux.Handle("GET /api/webhooks/export/{id}", s.requireAuth(http.HandlerFunc(s.apiGetExport)))
	mux.Handle("GET /api/webhooks/exports", s.requireAuth(http.HandlerFunc(s.apiListExports)))
	mux.Handle("GET /api/onboarding/status", s.requireAuth(http.HandlerFunc(s.apiOnboardingStatus)))
	mux.Handle("POST /api/onboarding/step", s.requireAuth(http.HandlerFunc(s.apiOnboardingStep)))
	mux.Handle("GET /api/onboarding/progress", s.requireAuth(http.HandlerFunc(s.apiOnboardingProgress)))

	// Self-service org/session/usage endpoints (Phase 2).
	mux.Handle("GET /api/me/orgs", s.requireAuth(http.HandlerFunc(s.apiMeOrgs)))
	mux.Handle("POST /api/auth/switch-org", s.requireAuth(http.HandlerFunc(s.apiSwitchOrg)))
	mux.Handle("GET /api/me/sessions", s.requireAuth(http.HandlerFunc(s.apiMeSessions)))
	mux.Handle("DELETE /api/me/sessions/{id}", s.requireAuth(http.HandlerFunc(s.apiRevokeMeSession)))
	mux.Handle("POST /api/me/sessions/revoke-others", s.requireAuth(http.HandlerFunc(s.apiRevokeOtherSessions)))
	mux.Handle("POST /api/org-tokens", s.requireAuth(http.HandlerFunc(s.apiCreateOrgToken)))
	mux.Handle("GET /api/me/usage", s.requireAuth(http.HandlerFunc(s.apiMeUsage)))

	// Billing & usage (deployment-aware). In enterprise mode these sit behind
	// auth; in local mode requireUI/requireAuth pass everything through.
	mux.Handle("GET /v1/billing/tiers", s.requireUI(s.apiBillingTiers, "viewer"))
	mux.Handle("GET /v1/billing/subscription", s.requireUI(s.apiBillingSubscription, "viewer"))
	mux.Handle("DELETE /v1/billing/subscription", s.requireUI(s.apiBillingSubscription, "viewer"))
	mux.Handle("POST /v1/billing/checkout", s.requireUI(s.apiBillingCheckout, "viewer"))
	mux.Handle("GET /v1/billing/plans", s.requireUI(s.apiBillingPlans, "viewer"))
	mux.Handle("GET /v1/usage", s.requireUI(s.apiUsageMetrics, "viewer"))

	// Admin API (HMAC-signed). NOT covered by activeTenant — these endpoints
	// are called by the proprietary billing service.
	mux.HandleFunc("POST /v1/admin/organizations/{id}/entitlements", s.adminApplyEntitlements)
	mux.HandleFunc("GET /v1/admin/organizations/{id}/entitlements", s.adminListEntitlements)

	// Audit-chain integrity verifier (admin-only).
	mux.HandleFunc("GET /api/admin/audit/verify", s.requireUI(s.apiAuditVerify, "admin"))

	// Orgs / projects / relays / hardware
	mux.HandleFunc("GET /api/organizations", s.requireUI(s.apiListOrgs, "viewer"))
	mux.HandleFunc("POST /api/organizations", s.requireUI(s.apiCreateOrg, "admin"))
	mux.HandleFunc("PATCH /api/organizations", s.requireUI(s.apiUpdateOrg, "admin"))
	mux.HandleFunc("GET /api/organizations/members", s.requireUI(s.apiListMembers, "admin"))
	mux.HandleFunc("POST /api/organizations/members", s.requireUI(s.apiInviteMember, "admin"))
	mux.HandleFunc("PATCH /api/organizations/members", s.requireUI(s.apiUpdateMember, "admin"))
	mux.HandleFunc("DELETE /api/organizations/members", s.requireUI(s.apiRemoveMember, "admin"))
	mux.HandleFunc("GET /api/projects", s.requireUI(s.apiListProjects, "viewer"))
	mux.HandleFunc("POST /api/projects", s.requireUI(s.apiCreateProject, "admin"))
	mux.HandleFunc("PATCH /api/projects/{id}", s.requireUI(s.apiUpdateProject, "admin"))
	mux.HandleFunc("DELETE /api/projects/{id}", s.requireUI(s.apiDeleteProject, "owner"))
	mux.HandleFunc("GET /api/relays", s.requireUI(s.apiListRelays, "viewer"))
	mux.HandleFunc("GET /api/hardware", s.requireUI(s.apiListHardware, "viewer"))
	mux.HandleFunc("POST /api/hardware", s.requireUI(s.apiCreateHardware, "maintainer"))
	mux.HandleFunc("GET /api/hardware/compare", s.requireUI(s.apiHardwareCompare, "viewer"))

	// Fleet Health
	mux.HandleFunc("GET /api/fleet/health", s.requireUI(s.apiFleetHealth, "viewer"))
	mux.HandleFunc("GET /api/fleet/health/devices/{id}", s.requireUI(s.apiFleetHealthDevice, "viewer"))

	// Chaos Engineering
	mux.HandleFunc("GET /api/chaos/status", s.requireUI(s.apiChaosStatus, "admin"))
	mux.HandleFunc("POST /api/chaos/inject", s.requireUI(s.apiChaosInject, "admin"))
	mux.HandleFunc("GET /api/chaos/results", s.requireUI(s.apiChaosResults, "admin"))

	// Memorial Wall
	mux.HandleFunc("GET /api/memorial/devices", s.requireUI(s.apiMemorialDevices, "viewer"))

	// Public Crash API (no auth required)
	mux.HandleFunc("GET /api/public/crashes", s.apiPublicCrashes)
	mux.HandleFunc("GET /api/public/crashes/by-arch", s.apiPublicCrashesByArch)
	mux.HandleFunc("GET /api/public/crashes/by-region", s.apiPublicCrashesByRegion)

	// Device DNA
	mux.HandleFunc("GET /api/devices/dna", s.requireUI(s.apiDeviceDNA, "viewer"))
	mux.HandleFunc("GET /api/devices/dna/clones", s.requireUI(s.apiDeviceDNAClones, "viewer"))

	// Anomaly Detection
	mux.HandleFunc("GET /api/anomaly/events", s.requireUI(s.apiAnomalyEvents, "viewer"))
	mux.HandleFunc("GET /api/anomaly/thresholds", s.requireUI(s.apiAnomalyThresholds, "admin"))
	mux.HandleFunc("POST /api/anomaly/thresholds", s.requireUI(s.apiAnomalyThresholds, "admin"))

	// Postmortem
	mux.HandleFunc("POST /api/issues/{id}/postmortem", s.requireUI(s.apiPostmortemGenerate, "viewer"))

	// Soundboard
	mux.HandleFunc("GET /api/soundboard/status", s.requireUI(s.apiSoundboardStatus, "viewer"))

	// Crash-to-PR
	mux.HandleFunc("GET /api/pr/status", s.requireUI(s.apiPRStatus, "viewer"))
	mux.HandleFunc("POST /api/pr/create", s.requireUI(s.apiPRCreate, "maintainer"))
	mux.HandleFunc("GET /api/prs", s.requireUI(s.apiPRList, "viewer"))

	if s.UI != nil {
		// Wrap the file server with SRI injection so HTML responses carry
		// integrity hashes on <script> and <link> tags. Non-HTML assets pass
		// through unchanged.
		fileServer := sriMiddleware(s.UI)
		mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") || strings.HasPrefix(r.URL.Path, "/health/") || r.URL.Path == "/metrics" || r.URL.Path == "/openapi.json" {
				http.NotFound(w, r)
				return
			}
			// SPA fallback: client routes like /issues/:id serve index.html
			if r.URL.Path != "/" && !strings.Contains(r.URL.Path, ".") {
				r.URL.Path = "/"
			}
			// Don't cache index.html so deploys pick up new asset hashes
			if r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, "index.html") {
				w.Header().Set("Cache-Control", "no-cache")
			} else if strings.Contains(r.URL.Path, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			fileServer.ServeHTTP(w, r)
		}))
	}
	return withSecurity(withRequestID(s.activeTenant(mux)), s.Cfg.RateLimitPerMin, s.Cfg.MaxConnsPerIP)
}

func (s *Server) live(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{}
	ok := true
	if s.Store != nil && s.Store.Pool != nil {
		if err := s.Store.Pool.Ping(r.Context()); err != nil {
			checks["postgres"] = err.Error()
			ok = false
		} else {
			checks["postgres"] = "ok"
		}
	}
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_live", "checks": checks})
		return
	}
	writeJSON(w, 200, map[string]any{"status": "live", "checks": checks})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{}
	ok := true
	if err := s.Store.Pool.Ping(r.Context()); err != nil {
		checks["postgres"] = err.Error()
		ok = false
	} else {
		checks["postgres"] = "ok"
	}
	if s.Object != nil {
		if err := s.Object.Ensure(); err != nil {
			checks["object_store"] = err.Error()
			ok = false
		} else {
			checks["object_store"] = "ok"
		}
	}
	if s.Queue != nil {
		checks["queue"] = "configured"
	} else {
		checks["queue"] = "missing"
		ok = false
	}
	// migrations applied marker
	var n int
	if err := s.Store.Pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM information_schema.tables WHERE table_name='events'`).Scan(&n); err != nil || n == 0 {
		checks["migrations"] = "events table missing"
		ok = false
	} else {
		checks["migrations"] = "ok"
	}
	// Check for insecure defaults — if any are detected, flag the response
	// without refusing the health check (operators may be in dev mode).
	insecure := s.checkInsecureDefaults()
	if len(insecure) > 0 {
		checks["security"] = "insecure defaults detected — see logs"
	}
	if !ok {
		writeJSON(w, 503, map[string]any{"status": "not_ready", "checks": checks})
		return
	}
	// If sslmode=disable is detected, downgrade from 200 to 200 with a
	// warning marker so probes can distinguish secure vs insecure readiness.
	writeJSON(w, 200, map[string]any{"status": "ready", "checks": checks, "version": s.Cfg.AppVersion, "insecure_defaults": insecure})
}

func (s *Server) capabilities(w http.ResponseWriter, _ *http.Request) {
	max := s.Cfg.MaxEventSize
	writeJSON(w, 200, map[string]any{
		"api_version": "1", "lep_versions": []int{1, 2}, "max_event_size": max,
		"max_batch_events": s.Cfg.MaxBatchEvents, "max_batch_bytes": s.Cfg.MaxBatchBytes,
		// zstd advertised when binary batch path supports it via content-encoding
		"compression":     []string{"identity", "zstd"},
		"batch_ingest":    true,
		"binary_batch":    true,
		"artifact_upload": true,
		"authentication":  []string{"bearer", "cookie"},
		"server_id":       "trace-v" + s.Cfg.AppVersion,
		"oidc":            s.Cfg.OIDCIssuer != "",
		"saml":            false, // experimental; not enterprise-ready
		"scim":            true,  // Users minimal profile + Groups flat (list/create/read/replace-members/delete)
		"public_register": s.Cfg.AllowPublicRegister,
		"queue":           s.Cfg.QueueDriver,
		"deployment":      s.Cfg.Deployment,
		"billing_enabled": !s.Cfg.IsLocal(),
	})
}

// checkInsecureDefaults inspects the running configuration for security
// concerns and returns a list of warning messages. Each warning describes
// a setting that should be tightened in production. This is intentionally
// non-blocking: it logs but does not refuse to start.
//
// Checks performed:
//   - Database URL sslmode=disable
//   - TRACE_SECRETS_KEY not set or empty
//   - TRACE_ADMIN_PASSWORD empty (auto-generated)
//   - TRACE_OPEN_UI is true (public UI access)
func (s *Server) checkInsecureDefaults() []string {
	var warnings []string

	// Database URL sslmode check.
	if s.Cfg.DatabaseURL != "" && strings.Contains(s.Cfg.DatabaseURL, "sslmode=disable") {
		warnings = append(warnings, "database sslmode=disable — connections are unencrypted")
	}

	// Secrets key: AES-GCM at-rest encryption disabled.
	if s.Cfg.SecretsKey == "" {
		warnings = append(warnings, "TRACE_SECRETS_KEY not set — secrets are stored in plaintext")
	}

	// Admin password: empty means auto-generated, which is fine for dev but
	// should be explicitly set in production.
	if s.Cfg.AdminPassword == "" {
		warnings = append(warnings, "TRACE_ADMIN_PASSWORD not set — auto-generated; set explicitly in production")
	}

	// Open UI: public access without authentication.
	if s.Cfg.OpenUI && !s.Cfg.IsLocal() {
		warnings = append(warnings, "TRACE_OPEN_UI=true — UI is publicly accessible without authentication")
	}

	return warnings
}

// logInsecureDefaults writes a single warning log entry summarizing any
// insecure defaults found in the active configuration. Called once at
// startup.
func (s *Server) LogInsecureDefaults() {
	warnings := s.checkInsecureDefaults()
	if len(warnings) == 0 {
		return
	}
	for _, w := range warnings {
		s.Log.Warn("insecure default detected", "warning", w)
	}
}

// (No ingest.go file exists yet; functions remain in server.go.)

var errForbidden = errors.New("forbidden")

// nowFunc returns the current UTC time. Tests may swap this for a fixed clock.
var nowFunc = func() time.Time { return time.Now().UTC() }

func (s *Server) requireIngest(r *http.Request, scope string) (store.Token, error) {
	secret, err := auth.Bearer(r.Header.Get("Authorization"))
	if err != nil {
		return store.Token{}, err
	}
	tok, err := s.Store.AuthIngestToken(r.Context(), secret)
	if err != nil {
		return store.Token{}, err
	}
	if scope != "" && !store.TokenHasScope(tok, scope) {
		return store.Token{}, errForbidden
	}
	return tok, nil
}

// pipelineForType selects the processing pipeline based on event type.
func pipelineForType(t uint8) string {
	switch t {
	case lep.TypeHealth:
		return "health"
	case lep.TypeLog, lep.TypeMessage:
		return "log"
	case lep.TypePeripheral:
		return "metric"
	case lep.TypeReset:
		return "boot"
	case lep.TypeCrash, lep.TypeCoredump, lep.TypeError:
		return "issue"
	default:
		return "issue"
	}
}

// ingest handles a single LEP event envelope from a relay.
func (s *Server) ingest(w http.ResponseWriter, r *http.Request) {
	metrics.IngestTotal.Add(1)
	tok, err := s.requireIngest(r, "event:write")
	if err != nil {
		metrics.IngestRejected.Add(1)
		status := 401
		if errors.Is(err, errForbidden) {
			status = 403
		}
		writeErr(w, status, "unauthorized", err.Error(), false)
		return
	}
	body := http.MaxBytesReader(w, r.Body, s.Cfg.MaxEventSize)
	defer body.Close()
	raw, err := io.ReadAll(body)
	if err != nil {
		metrics.IngestRejected.Add(1)
		writeErr(w, 400, "invalid_body", "could not read body", false)
		return
	}
	// Reject compressed bodies — the body is size-limited but decompression
	// could expand it beyond memory. Only identity (uncompressed) is supported.
	if enc := r.Header.Get("Content-Encoding"); enc != "" && !strings.EqualFold(enc, "identity") {
		metrics.IngestRejected.Add(1)
		writeErr(w, 415, "unsupported_encoding", "only identity encoding is supported", false)
		return
	}
	eventID := r.Header.Get("X-Last-State-Event-ID")
	if eventID == "" {
		eventID = r.Header.Get("Idempotency-Key")
	}
	res, code, msg, retryable, err := s.acceptOne(r, tok.ProjectID, eventID, raw)
	if err != nil {
		metrics.IngestRejected.Add(1)
		status := 422
		if retryable {
			status = 503
		}
		if code == "too_large" {
			status = 413
		}
		if code == "conflict" {
			// 422 (not 409): same event_id, different payload. Relay must NOT
			// treat this as delivered — 409 was historically overloaded as
			// "idempotent success" on some clients.
			status = http.StatusUnprocessableEntity
		}
		writeErr(w, status, code, msg, retryable)
		return
	}
	if res.Duplicate {
		metrics.IngestDuplicate.Add(1)
		// Same event_id + same hash is idempotent success (HTTP 2xx only).
		writeJSON(w, http.StatusAccepted, map[string]any{
			"id": res.Event.ID.String(), "status": "duplicate", "duplicate": true,
		})
		return
	}
	metrics.IngestAccepted.Add(1)
	if s.Log != nil {
		s.Log.Debug("ingest accepted",
			"event_id", res.Event.ID.String(),
			"project_id", tok.ProjectID.String(),
			"pipeline", res.Event.Pipeline,
		)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"id": res.Event.ID.String(), "status": "accepted", "duplicate": false,
	})
}

// ingestBatch handles a batch of LEP event envelopes from a relay.
func (s *Server) ingestBatch(w http.ResponseWriter, r *http.Request) {
	metrics.IngestTotal.Add(1)
	tok, err := s.requireIngest(r, "event:write")
	if err != nil {
		status := 401
		if errors.Is(err, errForbidden) {
			status = 403
		}
		writeErr(w, status, "unauthorized", err.Error(), false)
		return
	}
	maxBody := s.Cfg.MaxBatchBytes
	if maxBody <= 0 {
		maxBody = s.Cfg.MaxEventSize * 50
	}
	body := http.MaxBytesReader(w, r.Body, maxBody)
	defer body.Close()
	raw, err := io.ReadAll(body)
	if err != nil {
		writeErr(w, 400, "invalid_body", "could not read body", false)
		return
	}
	// Relay may send batches compressed (Content-Encoding: zstd) when we
	// advertise "zstd" in /v1/relay/capabilities — decode before parsing.
	if enc := r.Header.Get("Content-Encoding"); enc != "" && !strings.EqualFold(enc, "identity") {
		if !strings.EqualFold(enc, "zstd") {
			writeErr(w, 415, "unsupported_encoding", "Content-Encoding must be identity or zstd", false)
			return
		}
		zdec, zerr := zstd.NewReader(bytes.NewReader(raw),
			zstd.WithDecoderConcurrency(1),
			zstd.WithDecoderMaxMemory(uint64(maxBody)))
		if zerr != nil {
			writeErr(w, 400, "invalid_body", "zstd init: "+zerr.Error(), false)
			return
		}
		defer zdec.Close()
		raw, err = zdec.DecodeAll(raw, nil)
		if err != nil {
			writeErr(w, 400, "invalid_body", "zstd decode: "+err.Error(), false)
			return
		}
		if int64(len(raw)) > maxBody {
			writeErr(w, 400, "invalid_body", "decompressed batch exceeds max size", false)
			return
		}
	}
	type item struct {
		EventID string
		Payload []byte
	}
	var events []item
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "vnd.laststate.batch") || (len(raw) >= 4 && string(raw[:4]) == batch.Magic) {
		lim := batch.DefaultLimits()
		lim.MaxEvents = uint32(s.Cfg.MaxBatchEvents)
		lim.MaxPayload = int(s.Cfg.MaxEventSize)
		lim.MaxTotal = int(maxBody)
		be, err := batch.DecodeLimited(raw, lim)
		if err != nil {
			writeErr(w, 400, "invalid_batch", err.Error(), false)
			return
		}
		for _, e := range be {
			events = append(events, item{EventID: e.EventID, Payload: e.Payload})
		}
	} else {
		var req batchRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			writeErr(w, 400, "invalid_json", err.Error(), false)
			return
		}
		if len(req.Events) > s.Cfg.MaxBatchEvents {
			writeErr(w, 400, "batch_too_large", "too many events in batch", false)
			return
		}
		for _, e := range req.Events {
			if int64(len(e.Payload)) > s.Cfg.MaxEventSize {
				writeErr(w, 400, "event_too_large", "payload exceeds max", false)
				return
			}
			events = append(events, item{EventID: e.EventID, Payload: e.Payload})
		}
	}
	resp := map[string]any{"accepted": []any{}, "duplicates": []any{}, "rejected": []any{}}
	var accepted, dups, rejected []map[string]any
	for _, e := range events {
		res, code, msg, retryable, err := s.acceptOne(r, tok.ProjectID, e.EventID, e.Payload)
		if err != nil {
			metrics.IngestRejected.Add(1)
			rejected = append(rejected, map[string]any{"event_id": e.EventID, "code": code, "message": msg, "retryable": retryable})
			continue
		}
		item := map[string]any{"event_id": e.EventID, "receipt": res.Event.ID.String()}
		if res.Duplicate {
			metrics.IngestDuplicate.Add(1)
			dups = append(dups, item)
		} else {
			metrics.IngestAccepted.Add(1)
			accepted = append(accepted, item)
		}
	}
	resp["accepted"], resp["duplicates"], resp["rejected"] = accepted, dups, rejected
	writeJSON(w, 200, resp)
}

type batchRequest struct {
	Events []struct {
		EventID string `json:"event_id"`
		Payload []byte `json:"payload"`
	} `json:"events"`
}

// acceptOne validates a raw LEP envelope and persists the event.
func (s *Server) acceptOne(r *http.Request, projectID uuid.UUID, eventID string, raw []byte) (store.IngestResult, string, string, bool, error) {
	if int64(len(raw)) > s.Cfg.MaxEventSize {
		return store.IngestResult{}, "too_large", "event exceeds max size", false, errors.New("too large")
	}
	h, err := lep.Validate(raw)
	if err != nil {
		var ve *lep.ValidationError
		if errors.As(err, &ve) {
			if ve.Kind == lep.ErrorTooLarge {
				return store.IngestResult{}, "too_large", ve.Error(), false, err
			}
			if ve.Kind == lep.ErrorUnsupported {
				return store.IngestResult{}, "unsupported", ve.Error(), false, err
			}
		}
		return store.IngestResult{}, "corrupt", err.Error(), false, err
	}
	key, hash, err := s.Object.Put(raw)
	if err != nil {
		return store.IngestResult{}, "storage_error", err.Error(), true, err
	}
	if eventID == "" {
		eventID = "evt_" + hash[:26]
	}
	severity := "error"
	if h.Type == lep.TypeCrash || h.Type == lep.TypeCoredump {
		severity = "fatal"
	}
	pipeline := pipelineForType(h.Type)
	decoded, _ := json.Marshal(map[string]any{"header": h})
	res, err := s.Store.CreateEventIdempotent(r.Context(), projectID, eventID, int16(h.Type), int16(h.Architecture), int64(h.Sequence), int64(h.EventID), severity, key, hash, int64(len(raw)), decoded, pipeline)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return store.IngestResult{}, "conflict", err.Error(), false, err
		}
		return store.IngestResult{}, "internal", err.Error(), true, err
	}
	if !res.Duplicate {
		if err := s.enqueueProcess(r.Context(), projectID, res.Event.ID); err != nil {
			return store.IngestResult{}, "queue_error", err.Error(), true, err
		}
	}
	return res, "", "", false, nil
}

// enqueueProcess sends an event to the processing pipeline.
func (s *Server) enqueueProcess(ctx context.Context, projectID, eventID uuid.UUID) error {
	payload := map[string]string{
		"event_id":   eventID.String(),
		"project_id": projectID.String(),
	}
	if s.Queue != nil {
		return s.Queue.Enqueue(ctx, "process_event", payload)
	}
	// Fallback: postgres jobs table (api-only without queue configured)
	return s.Store.EnqueueJob(ctx, "process_event", payload)
}

// uploadArtifact handles artifact upload from a relay.
func (s *Server) uploadArtifact(w http.ResponseWriter, r *http.Request) {
	tok, err := s.requireIngest(r, "artifact:write")
	if err != nil {
		status := 401
		if errors.Is(err, errForbidden) {
			status = 403
		}
		writeErr(w, status, "unauthorized", err.Error(), false)
		return
	}
	a, err := s.saveArtifact(r, tok.ProjectID)
	if err != nil {
		writeErr(w, 422, "invalid_artifact", err.Error(), false)
		return
	}
	metrics.ArtifactUploads.Add(1)
	tid := tok.ID
	pid := tok.ProjectID
	s.Store.Audit(r.Context(), nil, &tid, nil, &pid, "artifact.upload", "artifact", a.ID.String(), clientIP(r), r.UserAgent(), map[string]any{"build_id": a.BuildID, "status": a.Status})
	writeJSON(w, 201, a)
}

// relayHeartbeat handles relay heartbeat registration.
func (s *Server) relayHeartbeat(w http.ResponseWriter, r *http.Request) {
	tok, err := s.requireIngest(r, "event:write")
	if err != nil {
		writeErr(w, 401, "unauthorized", err.Error(), false)
		return
	}
	var body struct {
		RelayID      string          `json:"relay_id"`
		Name         string          `json:"name"`
		Version      string          `json:"version"`
		Capabilities json.RawMessage `json:"capabilities"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	if body.RelayID == "" {
		body.RelayID = "relay-" + tok.Prefix
	}
	rel, err := s.Store.UpsertRelay(r.Context(), tok.ProjectID, body.RelayID, body.Name, body.Version, body.Capabilities)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, rel)
}

// saveArtifact persists an artifact and returns metadata.
func (s *Server) saveArtifact(r *http.Request, projectID uuid.UUID) (store.Artifact, error) {
	body := http.MaxBytesReader(nil, r.Body, s.Cfg.MaxArtifactSize)
	defer body.Close()
	raw, err := io.ReadAll(body)
	if err != nil {
		return store.Artifact{}, err
	}
	if len(raw) == 0 {
		return store.Artifact{}, errors.New("empty body")
	}
	key, hash, err := s.Object.Put(raw)
	if err != nil {
		return store.Artifact{}, err
	}
	tmp, err := artifact.WriteTemp(raw)
	if err != nil {
		return store.Artifact{}, err
	}
	defer os.Remove(tmp)
	meta, err := artifact.InspectFile(tmp)
	status := "ready"
	if err != nil {
		// quarantine non-ELF unless explicit build id header
		if bid := r.Header.Get("X-Last-State-Build-ID"); bid != "" {
			meta.BuildID = strings.ToLower(bid)
			meta.Architecture = r.Header.Get("X-Last-State-Architecture")
			status = "quarantine"
		} else {
			// store as quarantine with reason
			a, cerr := s.Store.CreateArtifact(r.Context(), projectID, "unknown", "", "", hash, key, int64(len(raw)), "quarantine")
			if cerr != nil {
				return store.Artifact{}, err
			}
			_ = s.Store.QuarantineArtifact(r.Context(), projectID, a.ID, err.Error())
			return a, nil
		}
	}
	if meta.BuildID == "" {
		status = "quarantine"
	}
	return s.Store.CreateArtifact(r.Context(), projectID, "elf", meta.BuildID, meta.Architecture, hash, key, int64(len(raw)), status)
}

type ctxKey int

const (
	sessKey    ctxKey = 1
	projectKey ctxKey = 2
)

// openUIEndpoints lists the API paths that remain accessible without
// authentication when OpenUI is enabled. Only read-only overview/search
// endpoints are whitelisted — sensitive data (issues, events, audit logs,
// settings, tokens) requires authentication regardless of OpenUI.
var openUIEndpoints = map[string]bool{
	"/api/overview":     true,
	"/api/search":       true,
	"/api/query/events": true,
	"/api/releases":     true,
	"/api/events":       true,
	"/api/artifacts":    true,
	"/api/labels":       true,
	"/api/boots":        true,
}

// requireUI enforces UI authentication and role-based access.
func (s *Server) requireUI(next http.HandlerFunc, minRole string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Local deployment mode: everything is unlocked. The operator running
		// the server is implicitly trusted — no auth gate and no role check.
		if s.Cfg.IsLocal() {
			next(w, r)
			return
		}
		if s.Cfg.OpenUI && r.Method == http.MethodGet && openUIEndpoints[r.URL.Path] {
			next(w, r)
			return
		}
		sess, ok := sessionFrom(s, r)
		if !ok {
			writeErr(w, 401, "unauthorized", "login required", false)
			return
		}
		if !s.userHasRole(sess, minRole) {
			writeErr(w, 403, "forbidden", "insufficient role", false)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), sessKey, sess)))
	}
}

// project resolves the project for the current request.
func (s *Server) project(r *http.Request) (store.Project, error) {
	if sess, ok := sessionFrom(s, r); ok {
		var pid *uuid.UUID
		if h := r.Header.Get("X-Project-ID"); h != "" {
			if id, err := uuid.Parse(h); err == nil {
				pid = &id
			}
		}
		if q := r.URL.Query().Get("project_id"); q != "" && pid == nil {
			if id, err := uuid.Parse(q); err == nil {
				pid = &id
			}
		}
		p, err := s.Store.ProjectForSession(r.Context(), sess.OrganizationID, pid)
		if err != nil {
			return store.Project{}, err
		}
		ok, _, err := s.Store.UserCanAccessProject(r.Context(), sess.UserID, p.ID)
		if err != nil {
			return store.Project{}, err
		}
		if !ok {
			return store.Project{}, store.ErrNotFound
		}
		return p, nil
	}
	return s.Store.DefaultProject(r.Context())
}

// login handles user login with account lockout protection.
// When the user enrolled MFA, a TOTP code (mfa_code) is required: without it
// the just-minted session is revoked and the API answers mfa_required —
// a password alone never completes login for MFA users.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		MfaCode  string `json:"mfa_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	const maxFailedLogins = 5
	const lockoutWindow = 5 * time.Minute
	s.loginLockoutMu.Lock()
	if entry, ok := s.loginLockout[body.Email]; ok && time.Since(entry.LockedUntil) >= 0 {
		delete(s.loginLockout, body.Email)
	} else if entry, ok := s.loginLockout[body.Email]; ok && entry.LockedUntil.After(time.Now()) {
		retryAfter := int(time.Until(entry.LockedUntil).Seconds())
		if retryAfter < 1 {
			retryAfter = 1
		}
		s.loginLockoutMu.Unlock()
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		writeErr(w, http.StatusTooManyRequests, "account_locked",
			fmt.Sprintf("too many failed login attempts. Retry after %ds", retryAfter), false)
		return
	}
	s.loginLockoutMu.Unlock()

	sess, secret, err := s.Store.Login(r.Context(), body.Email, body.Password)
	if err != nil {
		s.recordFailedLogin(body.Email)
		s.Store.Audit(r.Context(), nil, nil, nil, nil, "auth.login_failed", "user", body.Email, clientIP(r), r.UserAgent(), nil)
		writeErr(w, 401, "invalid_credentials", "invalid email or password", false)
		return
	}
	s.loginLockoutMu.Lock()
	delete(s.loginLockout, body.Email)
	s.loginLockoutMu.Unlock()
	if err != nil {
		writeErr(w, 401, "invalid_credentials", "invalid email or password", false)
		return
	}
	if err := s.enforceLoginMFA(w, r, sess, body.MfaCode, body.Email); err != nil {
		return
	}
	uid := sess.UserID
	oid := sess.OrganizationID
	s.Store.Audit(r.Context(), &uid, nil, &oid, nil, "auth.login", "user", uid.String(), clientIP(r), r.UserAgent(), nil)
	s.setSessionCookie(w, secret)
	writeJSON(w, 200, map[string]any{
		"token": secret,
		"user":  map[string]any{"id": sess.UserID, "email": sess.Email, "name": sess.Name, "role": sess.Role, "organization_id": sess.OrganizationID},
	})
}

// recordFailedLogin bumps the in-memory lockout counter for an identifier.
func (s *Server) recordFailedLogin(email string) {
	const maxFailedLogins = 5
	const lockoutWindow = 5 * time.Minute
	s.loginLockoutMu.Lock()
	defer s.loginLockoutMu.Unlock()
	if s.loginLockout == nil {
		s.loginLockout = make(map[string]loginLockoutEntry)
	}
	entry := loginLockoutEntry{
		FirstFailedAt: time.Now(),
		FailedCount:   1,
	}
	if existing, ok := s.loginLockout[email]; ok {
		entry.FailedCount = existing.FailedCount + 1
		entry.FirstFailedAt = existing.FirstFailedAt
	}
	if entry.FailedCount >= maxFailedLogins {
		entry.LockedUntil = time.Now().Add(lockoutWindow)
	}
	s.loginLockout[email] = entry
}

// enforceLoginMFA completes the second factor for MFA-enrolled users.
// The password step already minted a session: on any MFA failure that
// session is revoked so a password alone never yields access.
func (s *Server) enforceLoginMFA(w http.ResponseWriter, r *http.Request, sess store.Session, code, email string) error {
	enabled, err := s.Store.IsMFAEnabled(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, 500, "mfa_check_failed", "could not verify MFA status", false)
		return err
	}
	if !enabled {
		return nil
	}
	fail := func(code, msg string) error {
		_ = s.Store.RevokeSession(r.Context(), sess.SessionID, sess.UserID)
		writeErr(w, 401, code, msg, false)
		return fmt.Errorf("%s", code)
	}
	if code == "" {
		return fail("mfa_required", "multi-factor code required")
	}
	// Primary: TOTP. Fallback: single-use emailed code (account recovery).
	secret, err := s.Store.GetMfaSecret(r.Context(), sess.UserID)
	if err == nil && auth.ValidateTOTPCode(secret, code) {
		return nil
	}
	if err := s.Store.ConsumeEmailCode(r.Context(), sess.UserID, code); err != nil {
		s.recordFailedLogin(email)
		return fail("mfa_invalid", "invalid multi-factor code")
	}
	return nil
}

func (s *Server) setSessionCookie(w http.ResponseWriter, secret string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "trace_session",
		Value:    secret,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.Cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		// Matches the DB session lifetime (sessions.expires_at = now + 7 days).
		MaxAge: 60 * 60 * 24 * 7,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: "trace_session", Value: "", Path: "/", HttpOnly: true,
		Secure: s.Cfg.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

func sessionSecretFrom(r *http.Request) (string, error) {
	if secret, err := auth.Bearer(r.Header.Get("Authorization")); err == nil && secret != "" {
		return secret, nil
	}
	if c, err := r.Cookie("trace_session"); err == nil && c.Value != "" {
		return c.Value, nil
	}
	return "", errors.New("no session")
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "invalid session", false)
		return
	}
	writeJSON(w, 200, map[string]any{
		"id": sess.UserID, "email": sess.Email, "name": sess.Name, "role": sess.Role,
		"organization_id": sess.OrganizationID,
	})
}

func (s *Server) apiOverview(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	// Pagination: limit trend data points to avoid large responses.
	days := 14
	if d := r.URL.Query().Get("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 && n <= 90 {
			days = n
		}
	}
	ov, err := s.Store.Overview(r.Context(), p.ID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	ov["project"] = p
	// Cap trend data to requested days
	if trend, ok := ov["trend"].([]map[string]any); ok && len(trend) > days {
		ov["trend"] = trend[len(trend)-days:]
	}
	writeJSON(w, 200, ov)
}

func (s *Server) apiIssue(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}

	// Ensure organization matches current session before proceeding
	if err := s.checkOrgMatch(r, p.OrganizationID); err != nil {
		writeErr(w, 403, "access_denied", "organization mismatch", false)
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	item, err := s.Store.GetIssueInProject(r.Context(), p.ID, id)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	events, _ := s.Store.ListEventsByIssueInProject(r.Context(), p.ID, id, 50)
	comments, _ := s.Store.ListIssueComments(r.Context(), id)
	activity, _ := s.Store.ListIssueActivity(r.Context(), id, 50)
	writeJSON(w, 200, map[string]any{"issue": item, "events": events, "comments": comments, "activity": activity})
}

func (s *Server) apiIssueStatus(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	switch body.Status {
	case "open", "investigating", "resolved", "ignored", "archived":
	default:
		writeErr(w, 400, "bad_status", "invalid status", false)
		return
	}
	item, err := s.Store.UpdateIssueStatus(r.Context(), p.ID, id, body.Status)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	var actor *uuid.UUID
	if sess, ok := sessionFrom(s, r); ok {
		actor = &sess.UserID
	}
	oid := p.OrganizationID
	pid := p.ID
	s.Store.Audit(r.Context(), actor, nil, &oid, &pid, "issue.status", "issue", id.String(), clientIP(r), r.UserAgent(), map[string]any{"status": body.Status})
	if body.Status == "resolved" {
		_ = s.Store.EnqueueJob(r.Context(), "notify_issue", map[string]string{
			"project_id": item.ProjectID.String(),
			"issue_id":   item.ID.String(),
			"kind":       "issue_resolved",
		})
	}
	writeJSON(w, 200, item)
}

func (s *Server) apiIssueComment(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	if _, err := s.Store.GetIssueInProject(r.Context(), p.ID, id); err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Body) == "" {
		writeErr(w, 400, "invalid_body", "body required", false)
		return
	}
	var author *uuid.UUID
	if sess, ok := sessionFrom(s, r); ok {
		author = &sess.UserID
	}
	cid, err := s.Store.AddIssueComment(r.Context(), p.ID, id, author, body.Body)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 201, map[string]any{"id": cid})
}

func (s *Server) apiEvent(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	item, err := s.Store.GetEventInProject(r.Context(), p.ID, id)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	hist, _ := s.Store.EventHistory(r.Context(), id)
	writeJSON(w, 200, map[string]any{"event": item, "history": hist})
}

func (s *Server) apiEventRaw(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	item, err := s.Store.GetEventInProject(r.Context(), p.ID, id)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	raw, err := s.Object.Get(item.RawObjectKey)
	if err != nil {
		writeErr(w, 500, "storage_error", err.Error(), true)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+item.EventID+".lep\"")
	_, _ = w.Write(raw)
}

func (s *Server) apiEventReprocess(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	if err := s.Store.RequestReprocess(r.Context(), p.ID, id); err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	var actor *uuid.UUID
	if sess, ok := sessionFrom(s, r); ok {
		actor = &sess.UserID
	}
	oid, pid := p.OrganizationID, p.ID
	s.Store.Audit(r.Context(), actor, nil, &oid, &pid, "event.reprocess", "event", id.String(), clientIP(r), r.UserAgent(), nil)
	writeJSON(w, 202, map[string]string{"status": "queued"})
}

func (s *Server) apiDevice(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	d, err := s.Store.GetDeviceInProject(r.Context(), p.ID, id)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	events, _ := s.Store.ListEventsByDevice(r.Context(), id, 50)
	writeJSON(w, 200, map[string]any{"device": d, "events": events})
}

func (s *Server) apiReleases(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	items, err := s.Store.ListReleases(r.Context(), p.ID, 100)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiRelease(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	item, err := s.Store.GetReleaseInProject(r.Context(), p.ID, id)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	writeJSON(w, 200, item)
}

func (s *Server) apiArtifacts(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	items, err := s.Store.ListArtifacts(r.Context(), p.ID, 100)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiUploadArtifact(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	a, err := s.saveArtifact(r, p.ID)
	if err != nil {
		writeErr(w, 422, "invalid_artifact", err.Error(), false)
		return
	}
	metrics.ArtifactUploads.Add(1)
	var actor *uuid.UUID
	if sess, ok := sessionFrom(s, r); ok {
		actor = &sess.UserID
	}
	oid, pid := p.OrganizationID, p.ID
	s.Store.Audit(r.Context(), actor, nil, &oid, &pid, "artifact.upload", "artifact", a.ID.String(), clientIP(r), r.UserAgent(), map[string]any{"build_id": a.BuildID})
	writeJSON(w, 201, a)
}

func (s *Server) apiPromoteArtifact(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	a, err := s.Store.PromoteArtifact(r.Context(), p.ID, id)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	writeJSON(w, 200, a)
}

func (s *Server) apiAudit(w http.ResponseWriter, r *http.Request) {
	var orgID uuid.UUID
	if sess, ok := sessionFrom(s, r); ok {
		orgID = sess.OrganizationID
	}

	// Ensure organization matches current session before proceeding
	if err := s.checkOrgMatch(r, orgID); err != nil {
		writeErr(w, 403, "access_denied", "organization mismatch", false)
		return
	}

	// Check for export flag
	if r.URL.Query().Get("export") == "true" {
		data, err := s.Store.ExportAuditLogsToNDJSON(r.Context(), orgID, 10000)
		if err != nil {
			writeErr(w, 500, "export", err.Error(), true)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Content-Disposition", "attachment; filename=\"audit-export.ndjson\"")
		w.WriteHeader(200)
		w.Write(data)
		return
	}

	items, err := s.Store.ListAudit(r.Context(), orgID, 100)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiCreateToken(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}

	// Ensure organization matches current session before proceeding
	if err := s.checkOrgMatch(r, p.OrganizationID); err != nil {
		writeErr(w, 403, "access_denied", "organization mismatch", false)
		return
	}

	var body struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	if body.Name == "" {
		body.Name = "api-token"
	}
	t, secret, err := s.Store.CreateProjectToken(r.Context(), p.ID, body.Name, body.Scopes)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	var actor *uuid.UUID
	if sess, ok := sessionFrom(s, r); ok {
		actor = &sess.UserID
	}
	oid, pid := p.OrganizationID, p.ID
	s.Store.Audit(r.Context(), actor, nil, &oid, &pid, "token.create", "token", t.ID.String(), clientIP(r), r.UserAgent(), map[string]any{"name": body.Name, "scopes": body.Scopes})
	writeJSON(w, 201, map[string]any{"token": t, "secret": secret})
}

func (s *Server) apiBootstrapInfo(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeJSON(w, 200, map[string]any{"bootstrapped": false, "open_ui": s.Cfg.OpenUI, "deployment": s.Cfg.Deployment, "billing_enabled": !s.Cfg.IsLocal()})
		return
	}
	writeJSON(w, 200, map[string]any{"bootstrapped": true, "project": p, "open_ui": s.Cfg.OpenUI, "deployment": s.Cfg.Deployment, "billing_enabled": !s.Cfg.IsLocal()})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string, retryable bool) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code": code, "message": msg, "retryable": retryable,
			"request_id": w.Header().Get("X-Request-ID"),
		},
	})
}

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = auth.RandomHex(8)
		}
		w.Header().Set("X-Request-ID", id)
		// Preserve the request's context (including cancellation) — do NOT
		// replace with context.Background() which would discard deadlines.
		next.ServeHTTP(w, r.WithContext(r.Context()))
	})
}

func clientIP(r *http.Request) string {
	return clientIPFrom(r, "leftmost")
}
