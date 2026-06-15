package main

import (
	"net/http"

	"mcp-platform-api/admin"
	"mcp-platform-api/identity"
	"mcp-platform-api/internal/platforminternal"
	"mcp-platform-api/registry"
	"mcp-runtime/pkg/apihttp"
	"mcp-runtime/pkg/platformauth"
)

func (s *apiServer) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ready", s.handleReady)

	platforminternal.Handler{
		Store: s.platform,
		Token: s.internalAuthToken,
	}.Register(mux)

	auth := s.authentic.Middleware
	adminOnly := func(h http.Handler) http.Handler {
		return auth(s.authentic.RequireRole(platformauth.RoleAdmin, h))
	}

	register := func(pattern string, handler http.Handler) {
		mux.Handle("/api/v1"+pattern, handler)
	}

	register("/registry/authz", http.HandlerFunc(s.handleRegistryAuthz))
	register("/auth/login", http.HandlerFunc(s.handleLogin))
	register("/auth/oidc", http.HandlerFunc(s.handleOIDCLogin))
	register("/auth/signup", http.HandlerFunc(s.handleSignup))
	register("/users", adminOnly(http.HandlerFunc(s.handleUsers)))
	register("/auth/me", auth(http.HandlerFunc(s.handleAuthMe)))
	register("/user/registry-credentials", auth(http.HandlerFunc(s.handleRegistryCredentials)))
	register("/user/registry-credentials/", auth(http.HandlerFunc(s.handleRegistryCredentialItem)))
	register("/user/activity/image-publish", auth(http.HandlerFunc(s.handleUserImagePublishActivity)))
	register("/admin/namespaces", adminOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin.HandleNamespaces(w, r, admin.Dependencies{Platform: s.platform, WriteJSON: writeJSON})
	})))
	register("/admin/audit", adminOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin.HandleAudit(w, r, admin.Dependencies{Platform: s.platform, WriteJSON: writeJSON})
	})))
	register("/admin/operations", adminOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin.HandleOperations(w, r, admin.Dependencies{Platform: s.platform, WriteJSON: writeJSON})
	})))
}

func (s *apiServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *apiServer) handleReady(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil {
		apihttp.WriteEnvelope(w, http.StatusServiceUnavailable, apihttp.CodeServiceUnavailable, "postgres not configured")
		return
	}
	if err := s.platform.Ping(r.Context()); err != nil {
		apihttp.WriteEnvelope(w, http.StatusServiceUnavailable, apihttp.CodeServiceUnavailable, "postgres unavailable")
		return
	}
	apihttp.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *apiServer) handleRegistryCredentials(w http.ResponseWriter, r *http.Request) {
	registry.HandleRegistryCredentials(w, r, registry.CredentialDependencies{
		Platform:             s.platform,
		PrincipalFromContext: principalFromContext,
		WriteJSON:            writeJSON,
		WriteBodyDecodeError: writeBodyDecodeError,
		RequestIP:            requestIP,
		AuditSource:          auditSource,
		AuditIdentityLabel:   auditIdentityLabel,
	})
}

func (s *apiServer) handleRegistryCredentialItem(w http.ResponseWriter, r *http.Request) {
	registry.HandleRegistryCredentialItem(w, r, registry.CredentialDependencies{
		Platform:             s.platform,
		PrincipalFromContext: principalFromContext,
		WriteJSON:            writeJSON,
		RequestIP:            requestIP,
		AuditSource:          auditSource,
		AuditIdentityLabel:   auditIdentityLabel,
	})
}

func (s *apiServer) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	identity.HandleAuthMe(w, r, identity.Dependencies{
		PrincipalFromContext: principalFromContext,
		WriteJSON:            writeJSON,
	})
}

func (s *apiServer) handleSignup(w http.ResponseWriter, r *http.Request) {
	identity.HandleSignup(w, r, identity.Dependencies{
		Platform:             s.platform,
		AuthenticateRequest:  s.authenticateRequest,
		WriteJSON:            writeJSON,
		WriteBodyDecodeError: writeBodyDecodeError,
		RequestIP:            requestIP,
		RequestSource:        requestSource,
	})
}

func (s *apiServer) handleUsers(w http.ResponseWriter, r *http.Request) {
	identity.HandleUsers(w, r, identity.Dependencies{
		Platform:             s.platform,
		AuthenticateRequest:  s.authenticateRequest,
		WriteJSON:            writeJSON,
		WriteBodyDecodeError: writeBodyDecodeError,
		RequestIP:            requestIP,
		RequestSource:        requestSource,
		AuditIdentityLabel:   auditIdentityLabel,
	})
}
