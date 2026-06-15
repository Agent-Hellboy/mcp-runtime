package platforminternal

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mcp-platform-api/internal/platformstore"
	"mcp-runtime/pkg/apihttp"
	"mcp-runtime/pkg/platformauth"
)

type PlatformStore interface {
	AuthenticateUserAPIKey(ctx context.Context, rawKey string) (platformauth.Principal, bool, error)
	ResolveUserIDs(ctx context.Context, ids []string) (map[string]string, error)
	ResolveTeamIDs(ctx context.Context, ids []string) (map[string]string, error)
	WriteAudit(ctx context.Context, event platformstore.AuditEvent)
	CreateTeam(ctx context.Context, slug, name, createdByUserID string) (platformstore.Team, error)
	ListTeams(ctx context.Context) ([]platformstore.Team, error)
	GetTeamBySlug(ctx context.Context, slug string) (platformstore.Team, bool, error)
	DeleteTeamBySlug(ctx context.Context, slug string) error
	ListNamespaces(ctx context.Context) ([]map[string]any, error)
	GetNamespace(ctx context.Context, namespace string) (map[string]any, bool, error)
	ListTeamMemberships(ctx context.Context, teamSlug string) ([]platformstore.TeamMembership, error)
	UpsertTeamMembership(ctx context.Context, teamSlug, userID, role string) (platformstore.TeamMembership, error)
	DeleteTeamMembership(ctx context.Context, teamSlug, userID string) error
	CreatePasswordUser(ctx context.Context, email, password, role string) (platformstore.User, error)
	OperationsSnapshot(ctx context.Context, filter platformstore.OperationsFilter) (platformstore.OperationsSnapshot, error)
}

type Handler struct {
	Store PlatformStore
	Token string
}

func (h Handler) Register(mux *http.ServeMux) {
	mux.Handle("/internal/auth/resolve", h.authorize(http.HandlerFunc(h.resolveAuth)))
	mux.Handle("/internal/identity/resolve-ids", h.authorize(http.HandlerFunc(h.resolveIDs)))
	mux.Handle("/internal/audit", h.authorize(http.HandlerFunc(h.audit)))
	mux.Handle("/internal/identity/teams", h.authorize(http.HandlerFunc(h.teams)))
	mux.Handle("/internal/identity/teams/", h.authorize(http.HandlerFunc(h.teamPath)))
	mux.Handle("/internal/identity/namespaces", h.authorize(http.HandlerFunc(h.namespaces)))
	mux.Handle("/internal/identity/namespaces/", h.authorize(http.HandlerFunc(h.namespaceItem)))
	mux.Handle("/internal/identity/users", h.authorize(http.HandlerFunc(h.createUser)))
	mux.Handle("/internal/operations/snapshot", h.authorize(http.HandlerFunc(h.operationsSnapshot)))
}

func (h Handler) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := strings.TrimSpace(h.Token)
		provided := strings.TrimPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer ")
		if expected == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			apihttp.WriteEnvelope(w, http.StatusUnauthorized, apihttp.CodeUnauthorized, "internal authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h Handler) resolveAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var request struct {
		APIKey string `json:"api_key"`
	}
	if err := decodeJSON(r, &request); err != nil {
		apihttp.WriteEnvelope(w, http.StatusBadRequest, apihttp.CodeInvalidRequestBody, "invalid request body")
		return
	}
	principal, ok, err := h.Store.AuthenticateUserAPIKey(r.Context(), request.APIKey)
	if err != nil {
		apihttp.WriteEnvelope(w, http.StatusInternalServerError, apihttp.CodeAuthFailed, "failed to resolve API key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "principal": principal})
}

func (h Handler) resolveIDs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var request struct {
		UserIDs []string `json:"user_ids"`
		TeamIDs []string `json:"team_ids"`
	}
	if err := decodeJSON(r, &request); err != nil {
		apihttp.WriteEnvelope(w, http.StatusBadRequest, apihttp.CodeInvalidRequestBody, "invalid request body")
		return
	}
	users, err := h.Store.ResolveUserIDs(r.Context(), request.UserIDs)
	if err != nil {
		apihttp.WriteEnvelope(w, http.StatusInternalServerError, apihttp.CodeQueryFailed, "failed to resolve user ids")
		return
	}
	teams, err := h.Store.ResolveTeamIDs(r.Context(), request.TeamIDs)
	if err != nil {
		apihttp.WriteEnvelope(w, http.StatusInternalServerError, apihttp.CodeQueryFailed, "failed to resolve team ids")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users, "teams": teams})
}

func (h Handler) audit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var event platformstore.AuditEvent
	if err := decodeJSON(r, &event); err != nil {
		apihttp.WriteEnvelope(w, http.StatusBadRequest, apihttp.CodeInvalidRequestBody, "invalid request body")
		return
	}
	h.Store.WriteAudit(r.Context(), event)
	w.WriteHeader(http.StatusAccepted)
}

func (h Handler) teams(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		teams, err := h.Store.ListTeams(r.Context())
		if err != nil {
			apihttp.WriteEnvelope(w, http.StatusInternalServerError, apihttp.CodeQueryFailed, "failed to list teams")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"teams": teams})
	case http.MethodPost:
		var request struct {
			Slug            string `json:"slug"`
			Name            string `json:"name"`
			CreatedByUserID string `json:"created_by_user_id"`
		}
		if err := decodeJSON(r, &request); err != nil {
			apihttp.WriteEnvelope(w, http.StatusBadRequest, apihttp.CodeInvalidRequestBody, "invalid request body")
			return
		}
		team, err := h.Store.CreateTeam(r.Context(), request.Slug, request.Name, request.CreatedByUserID)
		if err != nil {
			apihttp.WriteEnvelope(w, http.StatusBadRequest, apihttp.CodeInvalidRequestBody, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, team)
	default:
		methodNotAllowed(w)
	}
}

func (h Handler) teamPath(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/internal/identity/teams/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		apihttp.WriteEnvelope(w, http.StatusNotFound, apihttp.CodeNotFound, "team not found")
		return
	}
	slug := parts[0]
	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		h.teamItemGet(w, r, slug)
	case len(parts) == 1 && r.Method == http.MethodDelete:
		h.teamItemDelete(w, r, slug)
	case len(parts) == 2 && parts[1] == "members" && r.Method == http.MethodGet:
		h.teamMembersList(w, r, slug)
	case len(parts) == 2 && parts[1] == "members" && r.Method == http.MethodPost:
		h.teamMembersUpsertBody(w, r, slug)
	case len(parts) == 3 && parts[1] == "members" && r.Method == http.MethodPut:
		h.teamMemberUpsert(w, r, slug, parts[2])
	case len(parts) == 3 && parts[1] == "members" && r.Method == http.MethodDelete:
		h.teamMemberDelete(w, r, slug, parts[2])
	case len(parts) == 2 && parts[1] == "users" && r.Method == http.MethodPost:
		h.teamUserCreate(w, r, slug)
	default:
		methodNotAllowed(w)
	}
}

func (h Handler) teamItemGet(w http.ResponseWriter, r *http.Request, slug string) {
	team, ok, err := h.Store.GetTeamBySlug(r.Context(), slug)
	if err != nil {
		apihttp.WriteEnvelope(w, http.StatusInternalServerError, apihttp.CodeQueryFailed, "failed to get team")
		return
	}
	if !ok {
		apihttp.WriteEnvelope(w, http.StatusNotFound, apihttp.CodeNotFound, "team not found")
		return
	}
	writeJSON(w, http.StatusOK, team)
}

func (h Handler) teamItemDelete(w http.ResponseWriter, r *http.Request, slug string) {
	if err := h.Store.DeleteTeamBySlug(r.Context(), slug); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			apihttp.WriteEnvelope(w, http.StatusNotFound, apihttp.CodeNotFound, "team not found")
			return
		}
		apihttp.WriteEnvelope(w, http.StatusInternalServerError, apihttp.CodeQueryFailed, "failed to delete team")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h Handler) teamMembersList(w http.ResponseWriter, r *http.Request, slug string) {
	members, err := h.Store.ListTeamMemberships(r.Context(), slug)
	if err != nil {
		apihttp.WriteEnvelope(w, http.StatusInternalServerError, apihttp.CodeQueryFailed, "failed to list team members")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": members})
}

func (h Handler) teamMembersUpsertBody(w http.ResponseWriter, r *http.Request, slug string) {
	var request struct {
		UserID string `json:"userID"`
		Role   string `json:"role"`
	}
	if err := decodeJSON(r, &request); err != nil {
		apihttp.WriteEnvelope(w, http.StatusBadRequest, apihttp.CodeInvalidRequestBody, "invalid request body")
		return
	}
	role := strings.TrimSpace(request.Role)
	if role == "" {
		role = teamRoleMember
	}
	membership, err := h.Store.UpsertTeamMembership(r.Context(), slug, strings.TrimSpace(request.UserID), role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			apihttp.WriteEnvelope(w, http.StatusNotFound, apihttp.CodeNotFound, "team or user not found")
			return
		}
		apihttp.WriteEnvelope(w, http.StatusBadRequest, apihttp.CodeInvalidRequestBody, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"membership": membership})
}

func (h Handler) teamMemberUpsert(w http.ResponseWriter, r *http.Request, slug, userID string) {
	var request struct {
		Role   string `json:"role"`
		UserID string `json:"userID"`
	}
	if r.Method == http.MethodPut {
		if err := decodeJSON(r, &request); err != nil {
			apihttp.WriteEnvelope(w, http.StatusBadRequest, apihttp.CodeInvalidRequestBody, "invalid request body")
			return
		}
	}
	role := strings.TrimSpace(request.Role)
	if role == "" {
		role = teamRoleMember
	}
	membership, err := h.Store.UpsertTeamMembership(r.Context(), slug, strings.TrimSpace(userID), role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			apihttp.WriteEnvelope(w, http.StatusNotFound, apihttp.CodeNotFound, "team or user not found")
			return
		}
		apihttp.WriteEnvelope(w, http.StatusBadRequest, apihttp.CodeInvalidRequestBody, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"membership": membership})
}

func (h Handler) teamMemberDelete(w http.ResponseWriter, r *http.Request, slug, userID string) {
	if err := h.Store.DeleteTeamMembership(r.Context(), slug, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			apihttp.WriteEnvelope(w, http.StatusNotFound, apihttp.CodeNotFound, "membership not found")
			return
		}
		apihttp.WriteEnvelope(w, http.StatusInternalServerError, apihttp.CodeQueryFailed, "failed to delete membership")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h Handler) teamUserCreate(w http.ResponseWriter, r *http.Request, teamSlug string) {
	var request struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := decodeJSON(r, &request); err != nil {
		apihttp.WriteEnvelope(w, http.StatusBadRequest, apihttp.CodeInvalidRequestBody, "invalid request body")
		return
	}
	role := strings.TrimSpace(request.Role)
	if role == "" {
		role = teamRoleMember
	}
	user, err := h.Store.CreatePasswordUser(r.Context(), strings.TrimSpace(request.Email), strings.TrimSpace(request.Password), roleUser)
	if err != nil {
		apihttp.WriteEnvelope(w, http.StatusBadRequest, apihttp.CodeInvalidRequestBody, err.Error())
		return
	}
	membership, err := h.Store.UpsertTeamMembership(r.Context(), teamSlug, user.ID, role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			apihttp.WriteEnvelope(w, http.StatusNotFound, apihttp.CodeNotFound, "team not found")
			return
		}
		apihttp.WriteEnvelope(w, http.StatusBadRequest, apihttp.CodeInvalidRequestBody, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": user, "membership": membership})
}

func (h Handler) createUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var request struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := decodeJSON(r, &request); err != nil {
		apihttp.WriteEnvelope(w, http.StatusBadRequest, apihttp.CodeInvalidRequestBody, "invalid request body")
		return
	}
	role := strings.TrimSpace(request.Role)
	if role == "" {
		role = roleUser
	}
	user, err := h.Store.CreatePasswordUser(r.Context(), strings.TrimSpace(request.Email), strings.TrimSpace(request.Password), role)
	if err != nil {
		apihttp.WriteEnvelope(w, http.StatusBadRequest, apihttp.CodeInvalidRequestBody, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": user})
}

func (h Handler) operationsSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	filter, err := operationsFilterFromRequest(r)
	if err != nil {
		apihttp.WriteEnvelope(w, http.StatusBadRequest, apihttp.CodeInvalidQueryParam, err.Error())
		return
	}
	snapshot, err := h.Store.OperationsSnapshot(r.Context(), filter)
	if err != nil {
		apihttp.WriteEnvelope(w, http.StatusInternalServerError, apihttp.CodeQueryFailed, "failed to load operations snapshot")
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

const (
	teamRoleMember = platformstore.TeamRoleMember
	roleUser       = platformstore.RoleUser
)

func operationsFilterFromRequest(r *http.Request) (platformstore.OperationsFilter, error) {
	user := strings.TrimSpace(r.URL.Query().Get("user"))
	filter := platformstore.OperationsFilter{
		User:       user,
		UserSearch: strings.ToLower(user),
		Limit:      queryInt(r, "limit", 50),
	}
	if filter.Limit < 1 {
		filter.Limit = 1
	}
	if filter.Limit > 200 {
		filter.Limit = 200
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			if parsed, err = time.Parse("2006-01-02", raw); err != nil {
				return platformstore.OperationsFilter{}, err
			}
		}
		filter.Since = parsed
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("until")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			if parsed, err = time.Parse("2006-01-02", raw); err != nil {
				return platformstore.OperationsFilter{}, err
			}
			parsed = parsed.Add(24*time.Hour - time.Nanosecond)
		}
		filter.Until = parsed
	}
	if !filter.Since.IsZero() && !filter.Until.IsZero() && filter.Since.After(filter.Until) {
		return platformstore.OperationsFilter{}, errors.New("since must be before until")
	}
	return filter, nil
}

func queryInt(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

func (h Handler) namespaces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	namespaces, err := h.Store.ListNamespaces(r.Context())
	if err != nil {
		apihttp.WriteEnvelope(w, http.StatusInternalServerError, apihttp.CodeQueryFailed, "failed to list namespaces")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"namespaces": namespaces})
}

func (h Handler) namespaceItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	namespace := strings.TrimPrefix(r.URL.Path, "/internal/identity/namespaces/")
	item, ok, err := h.Store.GetNamespace(r.Context(), namespace)
	if err != nil {
		apihttp.WriteEnvelope(w, http.StatusInternalServerError, apihttp.CodeQueryFailed, "failed to get namespace")
		return
	}
	if !ok {
		apihttp.WriteEnvelope(w, http.StatusNotFound, apihttp.CodeNotFound, "namespace not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func decodeJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func methodNotAllowed(w http.ResponseWriter) {
	apihttp.WriteEnvelope(w, http.StatusMethodNotAllowed, apihttp.CodeMethodNotAllowed, "method not allowed")
}
