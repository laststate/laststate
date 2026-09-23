package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/analysis"
	"github.com/laststate/trace/internal/store"
)

func (s *Server) apiIssuesPage(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	opt := pageOpts(r)
	items, total, err := s.Store.ListIssuesPage(r.Context(), p.ID, opt)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "limit": opt.Limit, "offset": opt.Offset})
}

func (s *Server) apiEventsPage(w http.ResponseWriter, r *http.Request) {
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

	opt := pageOpts(r)
	items, total, err := s.Store.ListEventsPage(r.Context(), p.ID, opt)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "limit": opt.Limit, "offset": opt.Offset})
}

func (s *Server) apiDevicesPage(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	opt := pageOpts(r)
	items, total, err := s.Store.ListDevicesPage(r.Context(), p.ID, opt)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "limit": opt.Limit, "offset": opt.Offset})
}

func pageOpts(r *http.Request) store.ListOpts {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	return store.ListOpts{
		Limit: limit, Offset: offset,
		Status: r.URL.Query().Get("status"),
		Query:  r.URL.Query().Get("q"),
		Sev:    r.URL.Query().Get("severity"),
	}
}

func (s *Server) apiIssueAssign(w http.ResponseWriter, r *http.Request) {
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
		UserID *uuid.UUID `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	item, err := s.Store.AssignIssue(r.Context(), p.ID, id, body.UserID)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	actor := actorPtr(s, r)
	oid, pid := p.OrganizationID, p.ID
	s.Store.Audit(r.Context(), actor, nil, &oid, &pid, "issue.assign", "issue", id.String(), clientIP(r), r.UserAgent(), body)
	writeJSON(w, 200, item)
}

func (s *Server) apiIssueMerge(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	source, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	var body struct {
		TargetID uuid.UUID `json:"target_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	actor := actorPtr(s, r)
	if err := s.Store.MergeIssues(r.Context(), p.ID, source, body.TargetID, actor); err != nil {
		writeErr(w, 400, "merge_failed", err.Error(), false)
		return
	}
	oid, pid := p.OrganizationID, p.ID
	s.Store.Audit(r.Context(), actor, nil, &oid, &pid, "issue.merge", "issue", source.String(), clientIP(r), r.UserAgent(), body)
	writeJSON(w, 200, map[string]string{"status": "merged"})
}

func (s *Server) apiIssueSplit(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	source, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	var body struct {
		EventIDs []uuid.UUID `json:"event_ids"`
		Title    string      `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	actor := actorPtr(s, r)
	ni, err := s.Store.SplitIssue(r.Context(), p.ID, source, body.EventIDs, body.Title, actor)
	if err != nil {
		writeErr(w, 400, "split_failed", err.Error(), false)
		return
	}
	writeJSON(w, 201, ni)
}

func (s *Server) apiIssueLabels(w http.ResponseWriter, r *http.Request) {
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
		Labels []string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	if err := s.Store.SetIssueLabels(r.Context(), p.ID, id, body.Labels); err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	writeJSON(w, 200, map[string]any{"labels": body.Labels})
}

func (s *Server) apiDevicePatch(w http.ResponseWriter, r *http.Request) {
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
		Aliases []string `json:"aliases"`
		Tags    []string `json:"tags"`
		Probe   string   `json:"probe_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	d, err := s.Store.UpdateDeviceMeta(r.Context(), p.ID, id, body.Aliases, body.Tags, body.Probe)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	writeJSON(w, 200, d)
}

func (s *Server) apiDeviceHistory(w http.ResponseWriter, r *http.Request) {
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
	if _, err := s.Store.GetDeviceInProject(r.Context(), p.ID, id); err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	hist, err := s.Store.ListFirmwareHistory(r.Context(), p.ID, id)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": hist})
}

func (s *Server) apiReleaseStats(w http.ResponseWriter, r *http.Request) {
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
	if _, err := s.Store.GetReleaseInProject(r.Context(), p.ID, id); err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	st, err := s.Store.CrashFreeRate(r.Context(), p.ID, id)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, st)
}

func (s *Server) apiReleasePatch(w http.ResponseWriter, r *http.Request) {
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
		RolloutPct int    `json:"rollout_pct"`
		GitCommit  string `json:"git_commit"`
		Toolchain  string `json:"toolchain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	rel, err := s.Store.UpdateReleaseRollout(r.Context(), p.ID, id, body.RolloutPct, body.GitCommit, body.Toolchain)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	writeJSON(w, 200, rel)
}

func (s *Server) apiHardwareCompare(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	items, err := s.Store.CompareHardwareFailures(r.Context(), p.ID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiChannels(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	items, err := s.Store.ListChannels(r.Context(), p.ID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiCreateChannel(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	var body struct {
		Kind   string         `json:"kind"`
		Name   string         `json:"name"`
		Config map[string]any `json:"config"`
		Secret string         `json:"secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	switch body.Kind {
	case "webhook", "slack", "discord", "email", "github", "gitlab":
	default:
		writeErr(w, 400, "bad_kind", "unsupported channel kind", false)
		return
	}
	id, err := s.Store.CreateChannel(r.Context(), p.ID, body.Kind, body.Name, body.Config, body.Secret)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	actor := actorPtr(s, r)
	oid, pid := p.OrganizationID, p.ID
	s.Store.Audit(r.Context(), actor, nil, &oid, &pid, "channel.create", "channel", id.String(), clientIP(r), r.UserAgent(), map[string]any{"kind": body.Kind})
	writeJSON(w, 201, map[string]any{"id": id})
}

func (s *Server) apiReprocessBatch(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	ids, err := s.Store.EventsNeedingReprocess(r.Context(), p.ID, analysis.AnalyzerVersion, 100)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	for _, id := range ids {
		_ = s.Store.RequestReprocess(r.Context(), p.ID, id)
	}
	writeJSON(w, 202, map[string]any{"queued": len(ids), "analyzer_version": analysis.AnalyzerVersion})
}

func (s *Server) apiUpdateOrg(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	o, err := s.Store.UpdateOrganization(r.Context(), sess.OrganizationID, body.Name)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	writeJSON(w, 200, o)
}

func (s *Server) apiUpdateMember(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	var body struct {
		UserID uuid.UUID `json:"user_id"`
		Role   string    `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	if err := s.Store.UpdateMemberRole(r.Context(), sess.OrganizationID, body.UserID, body.Role); err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "updated"})
}

func (s *Server) apiRemoveMember(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	uid, err := uuid.Parse(r.URL.Query().Get("user_id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "user_id required", false)
		return
	}
	if err := s.Store.RemoveMember(r.Context(), sess.OrganizationID, uid); err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "removed"})
}

func (s *Server) apiQueryEvents(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 400, "bad_project", err.Error(), false)
		return
	}
	q := store.ParseEventQuery(r.URL.Query().Get("q"))
	items, err := s.Store.QueryEvents(r.Context(), p.ID, q)
	if err != nil {
		writeErr(w, 500, "query", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "query": q, "total": len(items)})
}

func (s *Server) apiSearchFull(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	q := r.URL.Query().Get("q")
	res, err := s.Store.FTSSearch(r.Context(), p.ID, q, 20)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, res)
}

func actorPtr(s *Server, r *http.Request) *uuid.UUID {
	if sess, ok := sessionFrom(s, r); ok {
		u := sess.UserID
		return &u
	}
	return nil
}

func (s *Server) apiListTokens(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	items, err := s.Store.ListTokens(r.Context(), p.ID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiRevokeToken(w http.ResponseWriter, r *http.Request) {
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
	if err := s.Store.RevokeToken(r.Context(), p.ID, id); err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	actor := actorPtr(s, r)
	oid, pid := p.OrganizationID, p.ID
	s.Store.Audit(r.Context(), actor, nil, &oid, &pid, "token.revoke", "token", id.String(), clientIP(r), r.UserAgent(), nil)
	writeJSON(w, 200, map[string]string{"status": "revoked"})
}

func (s *Server) apiRegister(w http.ResponseWriter, r *http.Request) {
	if !s.Cfg.AllowPublicRegister {
		writeErr(w, 403, "register_disabled", "public registration is disabled; use invites or TRACE_ALLOW_PUBLIC_REGISTER=true for local dev only", false)
		return
	}
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	org, err := s.Store.DefaultProject(r.Context())
	if err != nil {
		writeErr(w, 503, "not_ready", "bootstrap first", true)
		return
	}
	// Public register is viewer-only (never developer/admin).
	u, err := s.Store.CreateUser(r.Context(), org.OrganizationID, body.Email, body.Password, body.Name, "viewer")
	if err != nil {
		writeErr(w, 400, "register_failed", err.Error(), false)
		return
	}
	// Issue a verification email: without it the "check your email" promise
	// in the UI would be false (tokens are upserted, raw value only in mail).
	if token, err := s.Store.IssueVerificationToken(r.Context(), u.ID); err != nil {
		s.Log.Warn("failed to issue verification token", "user", u.ID.String(), "error", err)
	} else if err := s.Mailer.SendVerificationEmail(r.Context(), body.Email, token); err != nil {
		s.Log.Warn("failed to send verification email", "email", body.Email, "error", err)
	}
	s.Store.Audit(r.Context(), &u.ID, nil, &org.OrganizationID, nil, "auth.register", "user", u.ID.String(), clientIP(r), r.UserAgent(), nil)
	sess, secret, err := s.Store.MintSession(r.Context(), u, org.OrganizationID, "viewer")
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	s.setSessionCookie(w, secret)
	writeJSON(w, 201, map[string]any{
		"token": secret,
		"user":  map[string]any{"id": sess.UserID, "email": sess.Email, "name": sess.Name, "role": sess.Role},
	})
}

func (s *Server) apiLogout(w http.ResponseWriter, r *http.Request) {
	s.clearSessionCookie(w)
	sess, ok := sessionFrom(s, r)
	if !ok {
		// still clear cookie even without session context
		writeJSON(w, 200, map[string]string{"status": "ok"})
		return
	}
	_ = s.Store.RevokeSession(r.Context(), sess.SessionID, sess.UserID)
	s.Store.Audit(r.Context(), &sess.UserID, nil, &sess.OrganizationID, nil, "auth.logout", "user", sess.UserID.String(), clientIP(r), r.UserAgent(), nil)
	writeJSON(w, 200, map[string]string{"status": "logged_out"})
}

func (s *Server) apiDeadJobs(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	pid := p.ID
	items, err := s.Store.ListDeadJobs(r.Context(), &pid, 100)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiRequeueDead(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	actor := actorPtr(s, r)
	if err := s.Store.RequeueDeadJob(r.Context(), id, actor); err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "requeued"})
}

func (s *Server) apiDiscardDead(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	if err := s.Store.DiscardDeadJob(r.Context(), id, actorPtr(s, r)); err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "discarded"})
}

func (s *Server) apiAlertSkips(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	items, err := s.Store.ListAlertSkips(r.Context(), p.ID, 100)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiGetSettings(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	ps, err := s.Store.GetOrCreateSettings(r.Context(), p.ID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, ps)
}

func (s *Server) apiPutSettings(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	var body struct {
		RetentionEventsDays  int `json:"retention_events_days"`
		RetentionHealthDays  int `json:"retention_health_days"`
		RetentionLogsDays    int `json:"retention_logs_days"`
		RetentionMetricsDays int `json:"retention_metrics_days"`
		AnalyzerVersionMin   int `json:"analyzer_version_min"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	ps, err := s.Store.UpdateSettings(r.Context(), p.ID, body.RetentionEventsDays, body.RetentionHealthDays, body.RetentionLogsDays, body.RetentionMetricsDays, body.AnalyzerVersionMin)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, ps)
}

func (s *Server) apiListLabels(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	items, err := s.Store.ListLabels(r.Context(), p.ID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiCreateLabel(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	var body struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeErr(w, 400, "bad_request", "name required", false)
		return
	}
	id, err := s.Store.EnsureLabel(r.Context(), p.ID, body.Name, body.Color)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "name": body.Name})
}

func (s *Server) apiCompareReleases(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	a, err1 := uuid.Parse(r.URL.Query().Get("a"))
	b, err2 := uuid.Parse(r.URL.Query().Get("b"))
	if err1 != nil || err2 != nil {
		writeErr(w, 400, "bad_id", "a and b release uuids required", false)
		return
	}
	res, err := s.Store.CompareReleases(r.Context(), p.ID, a, b)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) apiBootSessions(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 404, "no_project", err.Error(), false)
		return
	}
	var dev *uuid.UUID
	if d := r.URL.Query().Get("device_id"); d != "" {
		if id, err := uuid.Parse(d); err == nil {
			dev = &id
		}
	}
	items, err := s.Store.ListBootSessions(r.Context(), p.ID, dev, 100)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiAcceptInvite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InviteToken string `json:"invite_token"`
		Password    string `json:"password"`
		Name        string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error(), false)
		return
	}
	_, sess, secret, err := s.Store.AcceptInvite(r.Context(), body.InviteToken, body.Password, body.Name)
	if err != nil {
		writeErr(w, 400, "invite_failed", err.Error(), false)
		return
	}
	s.Store.Audit(r.Context(), &sess.UserID, nil, &sess.OrganizationID, nil, "auth.invite_accept", "user", sess.UserID.String(), clientIP(r), r.UserAgent(), nil)
	writeJSON(w, 200, map[string]any{
		"token": secret,
		"user":  map[string]any{"id": sess.UserID, "email": sess.Email, "role": sess.Role},
	})
}
