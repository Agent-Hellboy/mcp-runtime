package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"mcp-platform-api/internal/httperrors"
	"mcp-platform-api/internal/platformstore"
)

func HandlePasswordLogin(
	w http.ResponseWriter,
	r *http.Request,
	backend PasswordLoginBackend,
	tracker *LoginAttemptTracker,
	requestIP RequestIPFunc,
	requestSource RequestSourceFunc,
	writeJSON JSONWriterFunc,
	writeBodyDecodeError BodyDecodeErrorFunc,
	tokenTTL time.Duration,
	maxBodyBytes int64,
) {
	if backend == nil {
		httperrors.PlatformUnavailable(w)
		return
	}
	if r.Method != http.MethodPost {
		httperrors.MethodNotAllowed(w, "POST")
		return
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyDecodeError(w, err)
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	attemptKey := requestIP(r)
	if email != "" {
		attemptKey += "|" + email
	}
	if !tracker.Allow(attemptKey) {
		backend.WriteAudit(r.Context(), platformstore.AuditEvent{
			Action:   "login",
			Resource: email,
			Status:   "denied",
			Message:  "rate_limited",
			ActorIP:  requestIP(r),
			Source:   requestSource(r),
		})
		httperrors.TooManyRequests(w)
		return
	}
	u, ok, err := backend.AuthenticatePassword(r.Context(), email, req.Password)
	if err != nil {
		httperrors.LoginFailed(w)
		return
	}
	if !ok {
		failures := tracker.RecordFailure(attemptKey)
		backend.WriteAudit(r.Context(), platformstore.AuditEvent{
			Action:   "login",
			Resource: email,
			Status:   "denied",
			Message:  fmt.Sprintf("invalid credentials (failures=%d)", failures),
			ActorIP:  requestIP(r),
			Source:   requestSource(r),
		})
		httperrors.InvalidCredentials(w)
		return
	}
	tracker.RecordSuccess(attemptKey)
	token, err := backend.CreateAccessToken(u, tokenTTL)
	if err != nil {
		httperrors.Internal(w, "failed to issue token")
		return
	}
	backend.WriteAudit(r.Context(), platformstore.AuditEvent{UserID: u.ID, Action: "login", Resource: "user", Namespace: u.Namespace, Status: "success", ActorIP: requestIP(r), Source: requestSource(r) + ":password", AuthIdentity: "password:" + u.Email})
	writeJSON(w, http.StatusOK, map[string]any{"access_token": token, "token_type": "bearer", "expires_in": int(tokenTTL.Seconds()), "user": u})
}
