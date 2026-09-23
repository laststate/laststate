package api

import (
	"encoding/json"
	"net/http"
)

// apiOnboardingStatus returns the onboarding status for the current user.
func (s *Server) apiOnboardingStatus(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	status, err := s.Store.GetOnboardingStatus(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, status)
}

// apiOnboardingStep handles advancing an onboarding step.
func (s *Server) apiOnboardingStep(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Step string `json:"step"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "bad_request", "invalid body", false)
		return
	}
	if body.Step == "" {
		writeErr(w, 400, "bad_request", "step is required", false)
		return
	}
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	// Validate step.
	validSteps := map[string]bool{
		"signup":         true,
		"org_create":     true,
		"invite_members": true,
		"first_event":    true,
		"mfa_setup":      true,
	}
	if !validSteps[body.Step] {
		writeErr(w, 400, "bad_request", "invalid step", false)
		return
	}
	// Mark step as completed.
	if err := s.Store.MarkOnboardingStep(r.Context(), sess.UserID, body.Step); err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	status, err := s.Store.GetOnboardingStatus(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, status)
}

// apiOnboardingProgress returns the onboarding progress for the current user.
func (s *Server) apiOnboardingProgress(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	progress, err := s.Store.GetOnboardingProgress(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, progress)
}

// wire onboarding routes in server.go:
// mux.HandleFunc("GET /api/onboarding/status", s.requireAuth(http.HandlerFunc(s.apiOnboardingStatus)))
// mux.HandleFunc("POST /api/onboarding/step", s.requireAuth(http.HandlerFunc(s.apiOnboardingStep)))
// mux.HandleFunc("GET /api/onboarding/progress", s.requireAuth(http.HandlerFunc(s.apiOnboardingProgress)))
