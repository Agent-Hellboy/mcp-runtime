package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"mcp-platform-api/internal/httperrors"
	"mcp-platform-api/internal/platformstore"
)

func HandleOIDCLogin(
	w http.ResponseWriter,
	r *http.Request,
	backend PasswordLoginBackend,
	authenticateRequest func(*http.Request) (platformstore.Principal, bool, error),
	hook func(context.Context, string) (platformstore.User, error),
	unauthorizedErr error,
	requestIP RequestIPFunc,
	requestSource RequestSourceFunc,
	writeJSON JSONWriterFunc,
	writeBodyDecodeError BodyDecodeErrorFunc,
	tokenTTL time.Duration,
	maxBodyBytes int64,
	oidcIssuer string,
	oidcAudience string,
) {
	if backend == nil {
		httperrors.PlatformUnavailable(w)
		return
	}
	if r.Method != http.MethodPost {
		httperrors.MethodNotAllowed(w, "POST")
		return
	}
	if strings.TrimSpace(oidcIssuer) == "" || strings.TrimSpace(oidcAudience) == "" {
		httperrors.OIDCNotConfigured(w)
		return
	}
	var req struct {
		IDToken string `json:"id_token"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBodyDecodeError(w, err)
		return
	}
	idToken := strings.TrimSpace(req.IDToken)
	if idToken == "" {
		httperrors.BadRequest(w, "missing id token")
		return
	}

	var (
		u   platformstore.User
		err error
	)
	if hook != nil {
		u, err = hook(r.Context(), idToken)
	} else {
		u, err = ResolveOIDCLoginUser(r.Context(), idToken, authenticateRequest, unauthorizedErr)
	}
	if err != nil {
		statusCode := http.StatusInternalServerError
		auditStatus := "error"
		auditResource := strings.ToLower(strings.TrimSpace(u.Email))
		if auditResource == "" {
			auditResource = OIDCAuditResource(idToken)
		}
		if errors.Is(err, unauthorizedErr) {
			statusCode = http.StatusUnauthorized
			auditStatus = "denied"
		}
		backend.WriteAudit(r.Context(), platformstore.AuditEvent{
			Action:   "oidc_login",
			Resource: auditResource,
			Status:   auditStatus,
			Message:  err.Error(),
			ActorIP:  requestIP(r),
			Source:   requestSource(r) + ":oidc",
		})
		if statusCode == http.StatusUnauthorized {
			httperrors.Unauthorized(w)
			return
		}
		httperrors.LoginFailed(w)
		return
	}

	token, err := backend.CreateAccessToken(u, tokenTTL)
	if err != nil {
		httperrors.Internal(w, "failed to issue token")
		return
	}
	backend.WriteAudit(r.Context(), platformstore.AuditEvent{UserID: u.ID, Action: "oidc_login", Resource: "user", Namespace: u.Namespace, Status: "success", ActorIP: requestIP(r), Source: requestSource(r) + ":oidc", AuthIdentity: "oidc:" + u.Email})
	writeJSON(w, http.StatusOK, map[string]any{"access_token": token, "token_type": "bearer", "expires_in": int(tokenTTL.Seconds()), "user": u})
}

func ResolveOIDCLoginUser(
	ctx context.Context,
	idToken string,
	authenticateRequest func(*http.Request) (platformstore.Principal, bool, error),
	unauthorizedErr error,
) (platformstore.User, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://oidc.internal/verify", nil)
	if err != nil {
		return platformstore.User{}, err
	}
	req.Header.Set("authorization", "Bearer "+idToken)
	p, ok, err := authenticateRequest(req)
	if err != nil {
		return platformstore.User{}, err
	}
	if !ok || p.AuthType != "oidc_jwt" {
		return platformstore.User{}, fmt.Errorf("%w: token authentication failed", unauthorizedErr)
	}
	if p.Subject == "" || p.Email == "" {
		return platformstore.User{}, fmt.Errorf("%w: token missing identity", unauthorizedErr)
	}
	return platformstore.User{ID: p.Subject, Email: p.Email, Role: p.Role, Namespace: p.Namespace}, nil
}

func OIDCAuditResource(idToken string) string {
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(strings.TrimSpace(idToken), claims); err != nil {
		return "unknown"
	}
	email, _ := claims["email"].(string)
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return "unknown"
	}
	return email
}
