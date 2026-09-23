package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/analytics"
	"github.com/laststate/trace/internal/saml"
	"github.com/laststate/trace/internal/store"
)

func (s *Server) apiOncall(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 400, "bad_project", err.Error(), false)
		return
	}
	switch r.Method {
	case http.MethodGet:
		list, err := s.Store.ListOncallSchedules(r.Context(), p.ID)
		if err != nil {
			writeErr(w, 500, "list", err.Error(), true)
			return
		}
		who, _ := s.Store.WhoIsOnCall(r.Context(), p.ID, time.Now().UTC())
		writeJSON(w, 200, map[string]any{"schedules": list, "now_oncall": who})
	case http.MethodPost:
		var body struct {
			Name     string `json:"name"`
			Timezone string `json:"timezone"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
			writeErr(w, 400, "bad_json", "name required", false)
			return
		}
		o, err := s.Store.CreateOncallSchedule(r.Context(), p.ID, body.Name, body.Timezone)
		if err != nil {
			writeErr(w, 500, "create", err.Error(), true)
			return
		}
		writeJSON(w, 201, o)
	default:
		writeErr(w, 405, "method", "GET|POST", false)
	}
}

func (s *Server) apiOncallShift(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 400, "bad_project", err.Error(), false)
		return
	}
	_ = p
	var body struct {
		ScheduleID string `json:"schedule_id"`
		Email      string `json:"email"`
		Weekday    int    `json:"weekday"`
		StartMin   int    `json:"start_minute"`
		EndMin     int    `json:"end_minute"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "bad_json", err.Error(), false)
		return
	}
	sid, err := uuid.Parse(body.ScheduleID)
	if err != nil || body.Email == "" {
		writeErr(w, 400, "bad_request", "schedule_id and email required", false)
		return
	}
	if err := s.Store.AddOncallShift(r.Context(), sid, body.Email, body.Weekday, body.StartMin, body.EndMin); err != nil {
		writeErr(w, 500, "shift", err.Error(), true)
		return
	}
	writeJSON(w, 201, map[string]string{"status": "ok"})
}

func (s *Server) apiEscalation(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 400, "bad_project", err.Error(), false)
		return
	}
	switch r.Method {
	case http.MethodGet:
		list, err := s.Store.ListEscalationPolicies(r.Context(), p.ID)
		if err != nil {
			writeErr(w, 500, "list", err.Error(), true)
			return
		}
		writeJSON(w, 200, map[string]any{"items": list})
	case http.MethodPost:
		var body struct {
			Name   string `json:"name"`
			Levels any    `json:"levels"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
			writeErr(w, 400, "bad_json", "name required", false)
			return
		}
		pol, err := s.Store.CreateEscalationPolicy(r.Context(), p.ID, body.Name, body.Levels)
		if err != nil {
			writeErr(w, 500, "create", err.Error(), true)
			return
		}
		writeJSON(w, 201, pol)
	default:
		writeErr(w, 405, "method", "GET|POST", false)
	}
}

func (s *Server) apiSuspectCommits(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 400, "bad_project", err.Error(), false)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", err.Error(), false)
		return
	}
	items, err := s.Store.SuspectCommits(r.Context(), p.ID, id, 30)
	if err != nil {
		writeErr(w, 500, "suspect", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) apiIssueReplay(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 400, "bad_project", err.Error(), false)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", err.Error(), false)
		return
	}
	bc, err := s.Store.EventBreadcrumbs(r.Context(), p.ID, id)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error(), false)
		return
	}
	// Normalize into replay frames
	frames := make([]map[string]any, 0, len(bc))
	for i, b := range bc {
		frames = append(frames, map[string]any{
			"i":         i,
			"type":      b["type"],
			"category":  b["category"],
			"message":   b["message"],
			"timestamp": b["timestamp"],
			"data":      b["data"],
			"level":     b["level"],
		})
	}
	writeJSON(w, 200, map[string]any{
		"issue_id": id,
		"frames":   frames,
		"count":    len(frames),
		"kind":     "breadcrumb_replay",
	})
}

func (s *Server) apiReleaseCommits(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 400, "bad_project", err.Error(), false)
		return
	}
	rid, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", err.Error(), false)
		return
	}
	var body struct {
		Commits []struct {
			SHA     string `json:"sha"`
			Author  string `json:"author"`
			Message string `json:"message"`
		} `json:"commits"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "bad_json", err.Error(), false)
		return
	}
	n := 0
	for _, c := range body.Commits {
		if c.SHA == "" {
			continue
		}
		if err := s.Store.UpsertReleaseCommit(r.Context(), p.ID, rid, c.SHA, c.Author, c.Message, nil); err == nil {
			n++
		}
	}
	writeJSON(w, 200, map[string]any{"upserted": n})
}

func (s *Server) apiAnalyticsExport(w http.ResponseWriter, r *http.Request) {
	p, err := s.project(r)
	if err != nil {
		writeErr(w, 400, "bad_project", err.Error(), false)
		return
	}
	since := time.Now().Add(-24 * time.Hour)
	if h := r.URL.Query().Get("hours"); h != "" {
		var n int
		if _, err := parseInt(h, &n); err == nil && n > 0 {
			since = time.Now().Add(-time.Duration(n) * time.Hour)
		}
	}
	sink := analytics.FileSink{Root: s.Cfg.ObjectDir}
	if s.Object != nil {
		// prefer content-addressed store when available
		osink := analytics.ObjectSink{Objects: s.Object}
		key, n, err := analytics.ExportRecent(r.Context(), s.Store, osink, p.ID, since, 5000)
		if err == nil {
			writeJSON(w, 200, map[string]any{"object_key": key, "rows": n, "sink": "objects"})
			return
		}
	}
	key, n, err := analytics.ExportRecent(r.Context(), s.Store, sink, p.ID, since, 5000)
	if err != nil {
		writeErr(w, 500, "export", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{"object_key": key, "rows": n, "sink": "file"})
}

func parseInt(s string, n *int) (int, error) {
	var v int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errBadInt
		}
		v = v*10 + int(c-'0')
	}
	*n = v
	return v, nil
}

var errBadInt = errString("bad int")

type errString string

func (e errString) Error() string { return string(e) }

func (s *Server) apiSAMLMetadata(w http.ResponseWriter, r *http.Request) {
	entity := s.Cfg.PublicURL + "/saml/metadata"
	acs := s.Cfg.PublicURL + "/saml/acs"
	cfg := saml.Config{EntityID: entity, ACSURL: acs}
	// Prefer org config if any
	if org, err := s.orgFrom(r); err == nil {
		if sc, err := s.Store.GetSAMLConfig(r.Context(), org); err == nil && sc.EntityID != "" {
			cfg.EntityID = sc.EntityID
			if sc.ACSURL != "" {
				cfg.ACSURL = sc.ACSURL
			}
		}
	}
	w.Header().Set("Content-Type", "application/samlmetadata+xml")
	_, _ = w.Write([]byte(cfg.MetadataXML()))
}

func (s *Server) apiSAMLLogin(w http.ResponseWriter, r *http.Request) {
	org, err := s.orgFrom(r)
	if err != nil {
		// fallback public
		org = uuid.Nil
	}
	var sc store.SAMLConfig
	if org != uuid.Nil {
		sc, _ = s.Store.GetSAMLConfig(r.Context(), org)
	}
	cfg := saml.Config{
		EntityID:    firstNonEmpty(sc.EntityID, s.Cfg.PublicURL+"/saml/metadata"),
		ACSURL:      firstNonEmpty(sc.ACSURL, s.Cfg.PublicURL+"/saml/acs"),
		IDPSSOURL:   sc.SSOURL,
		IDPEntityID: sc.EntityID,
	}
	if !sc.Enabled || cfg.IDPSSOURL == "" {
		writeErr(w, 400, "saml_disabled", "SAML not configured for org", false)
		return
	}
	u, err := cfg.RedirectURL("trace")
	if err != nil {
		writeErr(w, 500, "saml", err.Error(), true)
		return
	}
	http.Redirect(w, r, u, http.StatusFound)
}

func (s *Server) apiSAMLACS(w http.ResponseWriter, r *http.Request) {
	// SAML ACS verifies XML signatures against the org's IdP certificate.
	// Without a certificate only the insecure local-test path remains, which
	// production config rejects.
	if err := r.ParseForm(); err != nil {
		writeErr(w, 400, "form", err.Error(), false)
		return
	}
	resp := r.FormValue("SAMLResponse")
	org, _ := s.orgFrom(r)
	entityID := s.Cfg.PublicURL + "/saml/metadata"
	acsURL := s.Cfg.PublicURL + "/saml/acs"
	cert := ""
	if org != uuid.Nil {
		if sc, err := s.Store.GetSAMLConfig(r.Context(), org); err == nil {
			cert = sc.CertificatePEM
			if sc.EntityID != "" {
				entityID = sc.EntityID
			}
			if sc.ACSURL != "" {
				acsURL = sc.ACSURL
			}
		}
	}
	if cert == "" && !s.Cfg.SAMLInsecure {
		writeErr(w, 501, "saml_not_ready", "SAML ACS requires an IdP certificate; set TRACE_SAML_INSECURE=true only for local testing (forbidden in production)", false)
		return
	}
	a, err := saml.ParseResponse(resp, saml.VerifyOpts{
		CertPEM:     cert,
		AllowNoCert: s.Cfg.SAMLInsecure,
		EntityID:    entityID,
		ACSURL:      acsURL,
	})
	if err != nil {
		writeErr(w, 400, "saml_assert", err.Error(), false)
		return
	}
	// Membership required — no auto-join first tenant
	_, sess, secret, err := s.Store.UpsertOIDCUserOpts(r.Context(), a.Email, a.NameID, s.Cfg.OIDCAutoJoin)
	if err != nil {
		writeErr(w, 403, "no_membership", err.Error(), false)
		return
	}
	uid := sess.UserID
	oid := sess.OrganizationID
	s.Store.Audit(r.Context(), &uid, nil, &oid, nil, "auth.saml_login", "user", uid.String(), clientIP(r), r.UserAgent(), map[string]any{"email": a.Email, "insecure": true})
	s.setSessionCookie(w, secret)
	writeJSON(w, 200, map[string]any{"token": secret, "email": a.Email, "via": "saml", "experimental": true})
}

func (s *Server) apiSAMLConfig(w http.ResponseWriter, r *http.Request) {
	org, err := s.orgFrom(r)
	if err != nil {
		writeErr(w, 400, "org", err.Error(), false)
		return
	}
	switch r.Method {
	case http.MethodGet:
		c, err := s.Store.GetSAMLConfig(r.Context(), org)
		if err != nil {
			writeJSON(w, 200, store.SAMLConfig{OrgID: org, ACSURL: s.Cfg.PublicURL + "/saml/acs"})
			return
		}
		c.CertificatePEM = "" // don't leak full cert in list by default
		writeJSON(w, 200, c)
	case http.MethodPut:
		var c store.SAMLConfig
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			writeErr(w, 400, "bad_json", err.Error(), false)
			return
		}
		c.OrgID = org
		if c.ACSURL == "" {
			c.ACSURL = s.Cfg.PublicURL + "/saml/acs"
		}
		out, err := s.Store.UpsertSAMLConfig(r.Context(), c)
		if err != nil {
			writeErr(w, 500, "save", err.Error(), true)
			return
		}
		writeJSON(w, 200, out)
	default:
		writeErr(w, 405, "method", "GET|PUT", false)
	}
}

func (s *Server) orgFrom(r *http.Request) (uuid.UUID, error) {
	if h := r.Header.Get("X-Org-ID"); h != "" {
		return uuid.Parse(h)
	}
	if sess, ok := sessionFrom(s, r); ok && sess.OrganizationID != uuid.Nil {
		return sess.OrganizationID, nil
	}
	return uuid.Nil, errString("no org")
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
