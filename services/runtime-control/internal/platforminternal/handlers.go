package platforminternal

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"mcp-runtime-control/internal/platformclient"
	"mcp-runtime-control/internal/runtimeapi"
	"mcp-runtime/pkg/apihttp"
)

// Handler serves runtime-control internal endpoints for peer services.
type Handler struct {
	Runtime  *runtimeapi.RuntimeServer
	Platform *platformclient.Client
	Token    string
}

// Register mounts internal routes on mux.
func (h Handler) Register(mux *http.ServeMux) {
	mux.Handle("/internal/auth/resolve", h.authorize(http.HandlerFunc(h.resolveAuth)))
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
		w.Header().Set("allow", http.MethodPost)
		apihttp.WriteEnvelope(w, http.StatusMethodNotAllowed, apihttp.CodeMethodNotAllowed, "method not allowed")
		return
	}
	if h.Runtime == nil {
		apihttp.WriteEnvelope(w, http.StatusServiceUnavailable, apihttp.CodeServiceUnavailable, "runtime not configured")
		return
	}
	var request struct {
		APIKey string `json:"api_key"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		apihttp.WriteEnvelope(w, http.StatusBadRequest, apihttp.CodeInvalidRequestBody, "invalid request body")
		return
	}
	principal, ok, err := h.Runtime.AuthenticateUserAPIKey(r.Context(), request.APIKey)
	if err != nil {
		apihttp.WriteEnvelope(w, http.StatusInternalServerError, apihttp.CodeAuthFailed, "failed to resolve API key")
		return
	}
	if !ok {
		apihttp.WriteJSON(w, http.StatusOK, map[string]any{"ok": false})
		return
	}
	if h.Platform != nil && h.Platform.Configured() && strings.TrimSpace(principal.Subject) != "" {
		enriched, enrichErr := h.Platform.PrincipalForUserID(r.Context(), principal.Subject)
		if enrichErr == nil {
			enriched.AuthType = principal.AuthType
			enriched.APIKeyID = principal.APIKeyID
			principal = enriched
		}
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "principal": principal})
}
