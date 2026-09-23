package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/store"
)

// SCIM 2.0 Users + Groups (RFC 7643/7644), minimal profile: Users list,
// create, read, update-name, deprovision-via-DELETE; Groups list/create/read/
// replace-members/delete (flat membership, no nesting per RFC 7644 §4.2 MAY;
// displayName immutable, renames via delete+create).

const scimUserSchema = "urn:ietf:params:scim:schemas:core:2.0:User"

// scimScopeOrg resolves the target org for SCIM calls: explicit
// ?organization_id wins, otherwise the session org. The caller must be a
// member; the route already requires an admin session role.
func (s *Server) scimScopeOrg(r *http.Request) (uuid.UUID, error) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		return uuid.Nil, scimErrNoSession
	}
	target := sess.OrganizationID
	if q := r.URL.Query().Get("organization_id"); q != "" {
		parsed, err := uuid.Parse(q)
		if err != nil {
			return uuid.Nil, err
		}
		target = parsed
	}
	member, err := s.Store.UserIsMemberOfOrg(r.Context(), sess.UserID, target)
	if err != nil || !member {
		return uuid.Nil, scimErrNoMembership
	}
	return target, nil
}

var (
	scimErrNoSession    = scimErrUnauthorized()
	scimErrNoMembership = scimErrForbidden()
)

func scimErrUnauthorized() error { return &scimError{"unauthorized"} }
func scimErrForbidden() error    { return &scimError{"forbidden"} }

type scimError struct{ msg string }

func (e *scimError) Error() string { return e.msg }

func scimUserResource(orgID, id uuid.UUID, email, name, role string) map[string]any {
	display := name
	if display == "" {
		display = email
	}
	return map[string]any{
		"schemas":  []string{scimUserSchema},
		"id":       id.String(),
		"userName": email,
		"name":     map[string]any{"formatted": display},
		"emails":   []any{map[string]any{"value": email, "primary": true}},
		"active":   true,
		"groups":   []any{},
		"meta": map[string]any{
			"resourceType": "User",
			"location":     "/scim/v2/Users/" + id.String() + "?organization_id=" + orgID.String(),
		},
		"urn:laststate:params:scim:schemas:extension:2.0:User": map[string]any{
			"role":           role,
			"organizationId": orgID.String(),
		},
	}
}

// apiSCIMUsers routes GET (list) and POST (create).
func (s *Server) apiSCIMUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.apiSCIMListUsers(w, r)
	case http.MethodPost:
		s.apiSCIMCreateUser(w, r)
	default:
		writeErr(w, 405, "method_not_allowed", "use GET or POST", false)
	}
}

func (s *Server) apiSCIMListUsers(w http.ResponseWriter, r *http.Request) {
	orgID, err := s.scimScopeOrg(r)
	if err != nil {
		writeErr(w, 403, "forbidden", "not a member of the target organization", false)
		return
	}
	members, err := s.Store.ListMembers(r.Context(), orgID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	filter := strings.TrimSpace(r.URL.Query().Get("filter"))
	var resources []any
	for _, m := range members {
		email, _ := m["email"].(string)
		if filter != "" && !matchUserNameFilter(filter, email) {
			continue
		}
		name, _ := m["name"].(string)
		role, _ := m["role"].(string)
		var id uuid.UUID
		switch v := m["id"].(type) {
		case uuid.UUID:
			id = v
		case string:
			id, _ = uuid.Parse(v)
		}
		resources = append(resources, scimUserResource(orgID, id, email, name, role))
	}
	if resources == nil {
		resources = []any{}
	}
	start, count := scimPage(r, len(resources))
	end := start + count
	if end > len(resources) {
		end = len(resources)
	}
	writeJSON(w, 200, map[string]any{
		"schemas":      []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"},
		"totalResults": len(resources),
		"startIndex":   start + 1,
		"itemsPerPage": end - start,
		"Resources":    resources[start:end],
	})
}

// matchUserNameFilter supports the one filter IdPs actually send:
// userName eq "addr@example.com" (case-insensitive).
func matchUserNameFilter(filter, email string) bool {
	lower := strings.ToLower(strings.TrimSpace(filter))
	if !strings.HasPrefix(lower, "username eq ") {
		return true // unknown filter shape: don't silently drop rows
	}
	want := strings.Trim(strings.TrimSpace(filter[len("userName eq "):]), `"'`)
	return strings.EqualFold(email, want)
}

func scimPage(r *http.Request, total int) (start, count int) {
	start, count = 0, total
	if v := r.URL.Query().Get("startIndex"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			start = n - 1 // SCIM is 1-based
		}
	}
	if v := r.URL.Query().Get("count"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			count = n
		}
	}
	if start > total {
		start = total
	}
	return start, count
}

func (s *Server) apiSCIMCreateUser(w http.ResponseWriter, r *http.Request) {
	orgID, err := s.scimScopeOrg(r)
	if err != nil {
		writeErr(w, 403, "forbidden", "not a member of the target organization", false)
		return
	}
	var body struct {
		UserName string `json:"userName"`
		Name     struct {
			Formatted string `json:"formatted"`
		} `json:"name"`
		Emails []struct {
			Value   string `json:"value"`
			Primary bool   `json:"primary"`
		} `json:"emails"`
		Active bool   `json:"active"`
		Role   string `json:"role"`
	}
	// Our role travels in a LastState extension; default viewer.
	var ext struct {
		Role string `json:"role"`
	}
	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeErr(w, 400, "bad_request", "invalid body", false)
		return
	}
	reb, _ := json.Marshal(raw)
	if err := json.Unmarshal(reb, &body); err != nil {
		writeErr(w, 400, "bad_request", "invalid body", false)
		return
	}
	if sub, ok := raw["urn:laststate:params:scim:schemas:extension:2.0:User"].(map[string]any); ok {
		if role, ok := sub["role"].(string); ok {
			ext.Role = role
		}
	}
	email := body.UserName
	if email == "" {
		for _, e := range body.Emails {
			if e.Primary || email == "" {
				email = e.Value
			}
		}
	}
	if email == "" {
		writeErr(w, 400, "bad_request", "userName or emails[].value is required", false)
		return
	}
	role := ext.Role
	if role == "" {
		role = body.Role
	}
	switch role {
	case "viewer", "developer", "maintainer", "admin", "owner", "":
		if role == "" {
			role = "viewer" // least privilege
		}
	default:
		writeErr(w, 400, "bad_request", "unknown role", false)
		return
	}
	name := body.Name.Formatted
	if name == "" {
		name = email
	}
	u, err := s.Store.CreatePasswordlessUser(r.Context(), email, name)
	if err != nil {
		writeErr(w, 409, "conflict", err.Error(), false)
		return
	}
	if err := s.Store.AddMember(r.Context(), orgID, u.ID, role); err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	sess, _ := sessionFrom(s, r)
	var actor *uuid.UUID
	if sess.UserID != uuid.Nil {
		actor = &sess.UserID
	}
	s.Store.Audit(r.Context(), actor, nil, &orgID, nil, "auth.scim_provision", "user", u.ID.String(), clientIP(r), r.UserAgent(), map[string]any{"role": role})
	w.Header().Set("Location", "/scim/v2/Users/"+u.ID.String())
	writeJSON(w, 201, scimUserResource(orgID, u.ID, email, name, role))
}

// apiSCIMUser routes GET, PUT/PATCH, DELETE on /scim/v2/Users/{id}.
func (s *Server) apiSCIMUser(w http.ResponseWriter, r *http.Request) {
	orgID, err := s.scimScopeOrg(r)
	if err != nil {
		writeErr(w, 403, "forbidden", "not a member of the target organization", false)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	member, err := s.Store.MemberByID(r.Context(), orgID, id)
	if err != nil || member == nil {
		writeErr(w, 404, "not_found", "user not found in organization", false)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, scimUserResource(orgID, id, member.Email, member.Name, member.Role))
	case http.MethodPut, http.MethodPatch:
		s.apiSCIMUpdateUser(w, r, orgID, member)
	case http.MethodDelete:
		if err := s.Store.RemoveMember(r.Context(), orgID, id); err != nil {
			writeErr(w, 500, "internal", err.Error(), true)
			return
		}
		sess, _ := sessionFrom(s, r)
		var actor *uuid.UUID
		if sess.UserID != uuid.Nil {
			actor = &sess.UserID
		}
		s.Store.Audit(r.Context(), actor, nil, &orgID, nil, "auth.scim_deprovision", "user", id.String(), clientIP(r), r.UserAgent(), nil)
		w.WriteHeader(http.StatusNoContent)
	default:
		writeErr(w, 405, "method_not_allowed", "use GET, PUT, PATCH or DELETE", false)
	}
}

func (s *Server) apiSCIMUpdateUser(w http.ResponseWriter, r *http.Request, orgID uuid.UUID, member *store.OrgMember) {
	var body struct {
		Name struct {
			Formatted string `json:"formatted"`
		} `json:"name"`
		Active *bool  `json:"active"`
		Role   string `json:"role"`
	}
	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeErr(w, 400, "bad_request", "invalid body", false)
		return
	}
	reb, _ := json.Marshal(raw)
	if err := json.Unmarshal(reb, &body); err != nil {
		writeErr(w, 400, "bad_request", "invalid body", false)
		return
	}
	if sub, ok := raw["urn:laststate:params:scim:schemas:extension:2.0:User"].(map[string]any); ok {
		if role, ok := sub["role"].(string); ok && body.Role == "" {
			body.Role = role
		}
	}
	if body.Active != nil && !*body.Active {
		// Deactivation = deprovision (no disabled flag exists).
		if err := s.Store.RemoveMember(r.Context(), orgID, member.ID); err != nil {
			writeErr(w, 500, "internal", err.Error(), true)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if body.Role != "" && body.Role != member.Role {
		switch body.Role {
		case "viewer", "developer", "maintainer", "admin", "owner":
		default:
			writeErr(w, 400, "bad_request", "unknown role", false)
			return
		}
		if err := s.Store.UpdateMemberRole(r.Context(), orgID, member.ID, body.Role); err != nil {
			writeErr(w, 500, "internal", err.Error(), true)
			return
		}
		member.Role = body.Role
	}
	if body.Name.Formatted != "" && body.Name.Formatted != member.Name {
		if err := s.Store.RenameUser(r.Context(), member.ID, body.Name.Formatted); err != nil {
			writeErr(w, 500, "internal", err.Error(), true)
			return
		}
		member.Name = body.Name.Formatted
	}
	writeJSON(w, 200, scimUserResource(orgID, member.ID, member.Email, member.Name, member.Role))
}

// ---- Groups (flat membership; no nesting) ----

const scimGroupSchema = "urn:ietf:params:scim:schemas:core:2.0:Group"

func scimGroupResource(orgID uuid.UUID, g store.SCIMGroup, emails map[uuid.UUID]string) map[string]any {
	members := make([]any, 0, len(g.Members))
	for _, uid := range g.Members {
		m := map[string]any{"value": uid.String()}
		if email, ok := emails[uid]; ok && email != "" {
			m["display"] = email
		}
		members = append(members, m)
	}
	return map[string]any{
		"schemas":     []string{scimGroupSchema},
		"id":          g.ID.String(),
		"displayName": g.DisplayName,
		"externalId":  g.ExternalID,
		"members":     members,
		"meta": map[string]any{
			"resourceType": "Group",
			"location":     "/scim/v2/Groups/" + g.ID.String() + "?organization_id=" + orgID.String(),
		},
	}
}

// memberEmails resolves display emails for a member id set.
func (s *Server) memberEmails(r *http.Request, orgID uuid.UUID, ids []uuid.UUID) map[uuid.UUID]string {
	out := map[uuid.UUID]string{}
	members, err := s.Store.ListMembers(r.Context(), orgID)
	if err != nil {
		return out
	}
	want := map[uuid.UUID]bool{}
	for _, id := range ids {
		want[id] = true
	}
	for _, m := range members {
		var id uuid.UUID
		switch v := m["id"].(type) {
		case uuid.UUID:
			id = v
		case string:
			id, _ = uuid.Parse(v)
		}
		if want[id] {
			if email, _ := m["email"].(string); email != "" {
				out[id] = email
			}
		}
	}
	return out
}

// apiSCIMGroups routes GET (list) and POST (create).
func (s *Server) apiSCIMGroups(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.apiSCIMListGroups(w, r)
	case http.MethodPost:
		s.apiSCIMCreateGroup(w, r)
	default:
		writeErr(w, 405, "method_not_allowed", "use GET or POST", false)
	}
}

func (s *Server) apiSCIMListGroups(w http.ResponseWriter, r *http.Request) {
	orgID, err := s.scimScopeOrg(r)
	if err != nil {
		writeErr(w, 403, "forbidden", "not a member of the target organization", false)
		return
	}
	groups, err := s.Store.ListSCIMGroups(r.Context(), orgID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	filter := strings.TrimSpace(r.URL.Query().Get("filter"))
	var resources []any
	for _, g := range groups {
		if filter != "" && !matchDisplayNameFilter(filter, g.DisplayName) {
			continue
		}
		resources = append(resources, scimGroupResource(orgID, g, s.memberEmails(r, orgID, g.Members)))
	}
	if resources == nil {
		resources = []any{}
	}
	start, count := scimPage(r, len(resources))
	end := start + count
	if end > len(resources) {
		end = len(resources)
	}
	writeJSON(w, 200, map[string]any{
		"schemas":      []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"},
		"totalResults": len(resources),
		"startIndex":   start + 1,
		"itemsPerPage": end - start,
		"Resources":    resources[start:end],
	})
}

// matchDisplayNameFilter supports: displayName eq "name" (case-insensitive).
func matchDisplayNameFilter(filter, display string) bool {
	lower := strings.ToLower(strings.TrimSpace(filter))
	if !strings.HasPrefix(lower, "displayname eq ") {
		return true
	}
	want := strings.Trim(strings.TrimSpace(filter[len("displayName eq "):]), `"'`)
	return strings.EqualFold(display, want)
}

func (s *Server) apiSCIMCreateGroup(w http.ResponseWriter, r *http.Request) {
	orgID, err := s.scimScopeOrg(r)
	if err != nil {
		writeErr(w, 403, "forbidden", "not a member of the target organization", false)
		return
	}
	var body struct {
		DisplayName string `json:"displayName"`
		ExternalID  string `json:"externalId"`
		Members     []struct {
			Value string `json:"value"`
		} `json:"members"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "bad_request", "invalid body", false)
		return
	}
	if strings.TrimSpace(body.DisplayName) == "" {
		writeErr(w, 400, "bad_request", "displayName is required", false)
		return
	}
	g, err := s.Store.CreateSCIMGroup(r.Context(), orgID, strings.TrimSpace(body.DisplayName), body.ExternalID)
	if err != nil {
		writeErr(w, 409, "conflict", err.Error(), false)
		return
	}
	if len(body.Members) > 0 {
		var ids []uuid.UUID
		for _, m := range body.Members {
			id, err := uuid.Parse(m.Value)
			if err != nil {
				writeErr(w, 400, "bad_request", "invalid member id", false)
				return
			}
			ids = append(ids, id)
		}
		if err := s.Store.SetSCIMGroupMembers(r.Context(), orgID, g.ID, ids); err != nil {
			writeErr(w, 400, "bad_request", err.Error(), false)
			return
		}
		g.Members = ids
	}
	sess, _ := sessionFrom(s, r)
	var actor *uuid.UUID
	if sess.UserID != uuid.Nil {
		actor = &sess.UserID
	}
	s.Store.Audit(r.Context(), actor, nil, &orgID, nil, "auth.scim_group_create", "group", g.ID.String(), clientIP(r), r.UserAgent(), map[string]any{"displayName": g.DisplayName})
	w.Header().Set("Location", "/scim/v2/Groups/"+g.ID.String())
	writeJSON(w, 201, scimGroupResource(orgID, g, s.memberEmails(r, orgID, g.Members)))
}

// apiSCIMGroup routes GET, PUT/PATCH, DELETE on /scim/v2/Groups/{id}.
func (s *Server) apiSCIMGroup(w http.ResponseWriter, r *http.Request) {
	orgID, err := s.scimScopeOrg(r)
	if err != nil {
		writeErr(w, 403, "forbidden", "not a member of the target organization", false)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_id", "invalid id", false)
		return
	}
	g, err := s.Store.GetSCIMGroup(r.Context(), orgID, id)
	if err != nil {
		writeErr(w, 404, "not_found", "group not found in organization", false)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, scimGroupResource(orgID, *g, s.memberEmails(r, orgID, g.Members)))
	case http.MethodPut, http.MethodPatch:
		var body struct {
			DisplayName string `json:"displayName"`
			Members     []struct {
				Value string `json:"value"`
			} `json:"members"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, 400, "bad_request", "invalid body", false)
			return
		}
		if body.Members != nil {
			var ids []uuid.UUID
			for _, m := range body.Members {
				uid, err := uuid.Parse(m.Value)
				if err != nil {
					writeErr(w, 400, "bad_request", "invalid member id", false)
					return
				}
				ids = append(ids, uid)
			}
			if ids == nil {
				ids = []uuid.UUID{}
			}
			if err := s.Store.SetSCIMGroupMembers(r.Context(), orgID, id, ids); err != nil {
				writeErr(w, 400, "bad_request", err.Error(), false)
				return
			}
			g.Members = ids
		}
		// displayName is immutable (unique key); renames go through delete+create.
		writeJSON(w, 200, scimGroupResource(orgID, *g, s.memberEmails(r, orgID, g.Members)))
	case http.MethodDelete:
		if err := s.Store.DeleteSCIMGroup(r.Context(), orgID, id); err != nil {
			writeErr(w, 500, "internal", err.Error(), true)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeErr(w, 405, "method_not_allowed", "use GET, PUT, PATCH or DELETE", false)
	}
}
