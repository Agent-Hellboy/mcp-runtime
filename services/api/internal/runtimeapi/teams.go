package runtimeapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (s *RuntimeServer) HandleRuntimeTeams(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "platform identity database not configured"})
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleRuntimeTeamList(w, r, p)
	case http.MethodPost:
		if p.Role != roleAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		s.handleRuntimeTeamCreate(w, r, p)
	default:
		w.Header().Set("allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *RuntimeServer) HandleRuntimeTeamItemPath(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "platform identity database not configured"})
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/runtime/teams/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
	teamSlug := NormalizeTeamSlug(parts[0])

	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		s.handleRuntimeTeamGet(w, r, p, teamSlug)
		return
	case len(parts) == 2 && parts[1] == "members" && r.Method == http.MethodPost:
		s.handleRuntimeTeamMemberUpsert(w, r, p, teamSlug)
		return
	case len(parts) == 3 && parts[1] == "members" && r.Method == http.MethodDelete:
		s.handleRuntimeTeamMemberDelete(w, r, p, teamSlug, strings.TrimSpace(parts[2]))
		return
	default:
		w.Header().Set("allow", "GET, POST, DELETE")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *RuntimeServer) HandleRuntimeNamespaces(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "platform identity database not configured"})
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if p.Role == roleAdmin {
		namespaces, err := s.platform.ListNamespaces(ctx)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list namespaces"})
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

func (s *RuntimeServer) HandleRuntimeNamespaceItem(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "platform identity database not configured"})
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	namespace := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/runtime/namespaces/"))
	namespace = strings.Trim(namespace, "/")
	if namespace == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "namespace required"})
		return
	}

	if p.Role != roleAdmin && !principalCanReadNamespace(p, namespace) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	item, ok, err := s.platform.GetNamespace(ctx, namespace)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to fetch namespace"})
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
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "namespace not found"})
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
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list teams"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"teams": teams})
		return
	}
	teams, err := s.platform.ListUserTeams(ctx, p.UserID())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list teams"})
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to fetch team"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "team not found"})
		return
	}
	if p.Role != roleAdmin && p.TeamRole(teamSlug) == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
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
			writeJSON(w, http.StatusConflict, map[string]string{"error": "team already exists"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.ensureTeamNamespace(ctx, team); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to provision team namespace"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"team": team})
}

func (s *RuntimeServer) handleRuntimeTeamMemberUpsert(w http.ResponseWriter, r *http.Request, p principal, teamSlug string) {
	if p.Role != roleAdmin && p.TeamRole(teamSlug) != teamRoleOwner {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	var req struct {
		UserID string `json:"userID"`
		Role   string `json:"role"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, teamApplyMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyDecodeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	membership, err := s.platform.UpsertTeamMembership(ctx, teamSlug, req.UserID, req.Role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "team or user not found"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"membership": membership})
}

func (s *RuntimeServer) handleRuntimeTeamMemberDelete(w http.ResponseWriter, r *http.Request, p principal, teamSlug, userID string) {
	if p.Role != roleAdmin && p.TeamRole(teamSlug) != teamRoleOwner {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := s.platform.DeleteTeamMembership(ctx, teamSlug, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "membership not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete membership"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"team":    teamSlug,
		"userID":  userID,
	})
}
