package runtimeapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// HandleRuntimeTeamItemPath routes team detail and membership operations after role checks.
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
	default:
		w.Header().Set("allow", "GET, POST, PUT, DELETE")
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed")
	}
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
