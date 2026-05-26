package runtimeapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// HandleRuntimeTeams lists visible teams and lets admins create team identity records.
func (s *RuntimeServer) HandleRuntimeTeams(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "platform identity database not configured")
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleRuntimeTeamList(w, r, p)
	case http.MethodPost:
		if p.Role != roleAdmin {
			writeAPIError(w, http.StatusForbidden, "forbidden")
			return
		}
		s.handleRuntimeTeamCreate(w, r, p)
	default:
		w.Header().Set("allow", "GET, POST")
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed")
	}
}

// HandleRuntimeTeamItemPath routes team detail, membership, and team-user operations after role checks.
func (s *RuntimeServer) HandleRuntimeTeamItemPath(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "platform identity database not configured")
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/runtime/teams/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid path")
		return
	}
	teamSlug := NormalizeTeamSlug(parts[0])

	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		s.handleRuntimeTeamGet(w, r, p, teamSlug)
		return
	case len(parts) == 2 && parts[1] == "members" && r.Method == http.MethodGet:
		s.handleRuntimeTeamMemberList(w, r, p, teamSlug)
		return
	case len(parts) == 2 && parts[1] == "members" && r.Method == http.MethodPost:
		s.handleRuntimeTeamMemberUpsertLegacy(w, r, p, teamSlug)
		return
	case len(parts) == 3 && parts[1] == "members" && r.Method == http.MethodPut:
		s.handleRuntimeTeamMemberUpsert(w, r, p, teamSlug, strings.TrimSpace(parts[2]))
		return
	case len(parts) == 3 && parts[1] == "members" && r.Method == http.MethodDelete:
		s.handleRuntimeTeamMemberDelete(w, r, p, teamSlug, strings.TrimSpace(parts[2]))
		return
	case len(parts) == 2 && parts[1] == "users" && r.Method == http.MethodPost:
		s.handleRuntimeTeamUserCreate(w, r, p, teamSlug)
		return
	default:
		w.Header().Set("allow", "GET, POST, PUT, DELETE")
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed")
	}
}

// HandleRuntimeNamespaces lists namespaces visible to the caller, including catalog namespace entries for the current platform mode.
func (s *RuntimeServer) HandleRuntimeNamespaces(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "platform identity database not configured")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("allow", http.MethodGet)
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if p.Role == roleAdmin {
		namespaces, err := s.platform.ListNamespaces(ctx)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "failed to list namespaces")
			return
		}
		namespaces = appendCatalogNamespaceEntries(namespaces)
		writeJSON(w, http.StatusOK, map[string]any{"namespaces": namespaces})
		return
	}

	entries := make([]map[string]any, 0, len(p.AllowedNamespaces))
	for _, namespace := range catalogNamespacesForPrincipal(p) {
		namespace = strings.TrimSpace(namespace)
		if namespace == "" {
			continue
		}
		if isModeCatalogNamespace(namespace) {
			entries = append(entries, catalogNamespaceEntry(namespace))
			continue
		}
		entry := map[string]any{
			"namespace": namespace,
			"is_shared": namespace == sharedCatalogNamespace,
		}
		for _, team := range p.Teams {
			if strings.TrimSpace(team.Namespace) == namespace {
				entry["team_id"] = team.ID
				entry["team_slug"] = team.Slug
				entry["team_name"] = team.Name
				entry["team_role"] = team.Role
				entry["scope"] = namespaceScopeTeam
			}
		}
		if _, ok := entry["scope"]; !ok {
			entry["scope"] = namespaceScopeUser
		}
		entries = append(entries, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"namespaces": entries})
}

// HandleRuntimeNamespaceItem returns one visible namespace record or synthetic catalog namespace entry.
func (s *RuntimeServer) HandleRuntimeNamespaceItem(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "platform identity database not configured")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("allow", http.MethodGet)
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	namespace := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/runtime/namespaces/"))
	namespace = strings.Trim(namespace, "/")
	if namespace == "" {
		writeAPIError(w, http.StatusBadRequest, "namespace required")
		return
	}

	if p.Role != roleAdmin && !principalCanReadNamespace(p, namespace) {
		writeAPIError(w, http.StatusForbidden, "forbidden")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	item, ok, err := s.platform.GetNamespace(ctx, namespace)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to fetch namespace")
		return
	}
	if ok {
		writeJSON(w, http.StatusOK, map[string]any{"namespace": item})
		return
	}
	if namespace == sharedCatalogNamespace || isModeCatalogNamespace(namespace) {
		entry := catalogNamespaceEntry(namespace)
		if namespace == sharedCatalogNamespace && !sharedCatalogWritableForUsers() {
			entry["scope"] = "shared"
			entry["is_public"] = false
		}
		writeJSON(w, http.StatusOK, map[string]any{"namespace": entry})
		return
	}
	writeAPIError(w, http.StatusNotFound, "namespace not found")
}

func catalogNamespaceEntry(namespace string) map[string]any {
	scope := "shared"
	isPublic := false
	scopeName := "Shared catalog"
	if namespace != sharedCatalogNamespace {
		switch PlatformMode() {
		case platformModePublic:
			scope = "public"
			isPublic = true
			scopeName = "Public preview"
		case platformModeOrg:
			scope = "org"
			scopeName = "Organization"
		}
	}
	return map[string]any{
		"namespace":  namespace,
		"is_shared":  namespace == sharedCatalogNamespace,
		"is_public":  isPublic,
		"scope":      scope,
		"scope_name": scopeName,
	}
}

func appendCatalogNamespaceEntries(namespaces []map[string]any) []map[string]any {
	seen := map[string]struct{}{}
	for _, entry := range namespaces {
		if namespace := strings.TrimSpace(fmt.Sprint(entry["namespace"])); namespace != "" {
			seen[namespace] = struct{}{}
		}
	}
	catalogNamespaces := append([]string{sharedCatalogNamespace}, modeCatalogNamespaces()...)
	for _, namespace := range dedupeNonEmptyStrings(catalogNamespaces) {
		if _, ok := seen[namespace]; ok {
			continue
		}
		namespaces = append(namespaces, catalogNamespaceEntry(namespace))
	}
	return namespaces
}

func (s *RuntimeServer) handleRuntimeTeamList(w http.ResponseWriter, r *http.Request, p principal) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if p.Role == roleAdmin {
		teams, err := s.platform.ListTeams(ctx)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "failed to list teams")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"teams": teams})
		return
	}
	teams, err := s.platform.ListUserTeams(ctx, p.UserID())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to list teams")
		return
	}
	out := make([]teamRecord, 0, len(teams))
	for _, membership := range teams {
		out = append(out, teamRecord{
			ID:        membership.TeamID,
			Slug:      membership.TeamSlug,
			Name:      membership.TeamName,
			Namespace: membership.TeamNamespace,
			CreatedAt: membership.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"teams": out})
}

func (s *RuntimeServer) handleRuntimeTeamGet(w http.ResponseWriter, r *http.Request, p principal, teamSlug string) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	team, ok, err := s.platform.GetTeamBySlug(ctx, teamSlug)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to fetch team")
		return
	}
	if !ok {
		writeAPIError(w, http.StatusNotFound, "team not found")
		return
	}
	if p.Role != roleAdmin && p.TeamRole(teamSlug) == "" {
		writeAPIError(w, http.StatusForbidden, "forbidden")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"team": team})
}

func (s *RuntimeServer) handleRuntimeTeamCreate(w http.ResponseWriter, r *http.Request, p principal) {
	var req struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, teamApplyMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyDecodeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	team, err := s.platform.CreateTeam(ctx, req.Slug, req.Name, p.UserID())
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") || strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeAPIError(w, http.StatusConflict, "team already exists")
			return
		}
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.ensureTeamNamespace(ctx, team); err != nil {
		if cleanupErr := s.platform.DeleteTeamBySlug(ctx, team.Slug); cleanupErr != nil {
			log.Printf("failed to clean up team %s after namespace provisioning failure: %v", team.Slug, cleanupErr)
			writeAPIError(w, http.StatusInternalServerError, "failed to provision team namespace")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to provision team namespace")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"team": team})
}

func (s *RuntimeServer) handleRuntimeTeamMemberList(w http.ResponseWriter, r *http.Request, p principal, teamSlug string) {
	if p.Role != roleAdmin && p.TeamRole(teamSlug) == "" {
		writeAPIError(w, http.StatusForbidden, "forbidden")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	memberships, err := s.platform.ListTeamMemberships(ctx, teamSlug)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to list team members")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": memberships})
}

type teamMemberUpsertRequest struct {
	UserID string `json:"userID"`
	Role   string `json:"role"`
}

func (s *RuntimeServer) handleRuntimeTeamMemberUpsert(w http.ResponseWriter, r *http.Request, p principal, teamSlug, userID string) {
	if p.Role != roleAdmin && p.TeamRole(teamSlug) != teamRoleOwner {
		writeAPIError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req teamMemberUpsertRequest
	r.Body = http.MaxBytesReader(w, r.Body, teamApplyMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyDecodeError(w, err)
		return
	}
	if strings.TrimSpace(req.UserID) != "" && strings.TrimSpace(userID) != "" && strings.TrimSpace(req.UserID) != strings.TrimSpace(userID) {
		writeAPIError(w, http.StatusBadRequest, "userID must match the path")
		return
	}
	if strings.TrimSpace(userID) == "" {
		userID = strings.TrimSpace(req.UserID)
	}
	s.handleRuntimeTeamMemberUpsertDecoded(w, r, teamSlug, userID, req.Role)
}

func (s *RuntimeServer) handleRuntimeTeamMemberUpsertDecoded(w http.ResponseWriter, r *http.Request, teamSlug, userID, role string) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	membership, err := s.platform.UpsertTeamMembership(ctx, teamSlug, userID, role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusNotFound, "team or user not found")
			return
		}
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"membership": membership})
}

func (s *RuntimeServer) handleRuntimeTeamMemberUpsertLegacy(w http.ResponseWriter, r *http.Request, p principal, teamSlug string) {
	var req teamMemberUpsertRequest
	r.Body = http.MaxBytesReader(w, r.Body, teamApplyMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyDecodeError(w, err)
		return
	}
	if p.Role != roleAdmin && p.TeamRole(teamSlug) != teamRoleOwner {
		writeAPIError(w, http.StatusForbidden, "forbidden")
		return
	}
	s.handleRuntimeTeamMemberUpsertDecoded(w, r, teamSlug, req.UserID, req.Role)
}

func (s *RuntimeServer) handleRuntimeTeamUserCreate(w http.ResponseWriter, r *http.Request, p principal, teamSlug string) {
	if p.Role != roleAdmin {
		writeAPIError(w, http.StatusForbidden, "admin role required")
		return
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, teamApplyMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyDecodeError(w, err)
		return
	}
	membershipRole := strings.ToLower(strings.TrimSpace(req.Role))
	if membershipRole == "" {
		membershipRole = teamRoleMember
	}
	if membershipRole != teamRoleMember && membershipRole != teamRoleOwner {
		writeAPIError(w, http.StatusBadRequest, "role must be member or owner")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if _, ok, err := s.platform.GetTeamBySlug(ctx, teamSlug); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to fetch team")
		return
	} else if !ok {
		writeAPIError(w, http.StatusNotFound, "team not found")
		return
	}
	u, err := s.platform.EnsureTeamPasswordUser(ctx, req.Email, req.Password)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	membership, err := s.platform.UpsertTeamMembership(ctx, teamSlug, u.ID, membershipRole)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusNotFound, "team or user not found")
			return
		}
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	membership.Email = u.Email
	s.writeAudit(r.Context(), auditEvent{
		UserID:       p.UserID(),
		Action:       "team_user_create",
		Resource:     u.Email,
		Namespace:    membership.TeamNamespace,
		Status:       "success",
		Message:      "team=" + membership.TeamSlug + " role=" + membership.Role,
		ActorIP:      requestIP(r),
		Source:       auditSource(r, p),
		AuthIdentity: auditIdentityLabel(p),
	})
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "membership": membership})
}

func (s *RuntimeServer) handleRuntimeTeamMemberDelete(w http.ResponseWriter, r *http.Request, p principal, teamSlug, userID string) {
	if p.Role != roleAdmin && p.TeamRole(teamSlug) != teamRoleOwner {
		writeAPIError(w, http.StatusForbidden, "forbidden")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := s.platform.DeleteTeamMembership(ctx, teamSlug, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusNotFound, "membership not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to delete membership")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"team":    teamSlug,
		"userID":  userID,
	})
}
