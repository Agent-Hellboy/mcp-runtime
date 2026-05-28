package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"mcp-sentinel-api/auth"
)

var oidcLoginHook func(context.Context, *apiServer, string) (platformUser, error)
var errOIDCUnauthorized = errors.New("oidc unauthorized")
var platformLoginAttempts = auth.NewLoginAttemptTracker(time.Now)

const platformAccessTokenTTL = auth.PlatformAccessTokenTTL
const platformSignupRequestMaxBytes = auth.PlatformSignupRequestMaxBytes
const platformPasswordLoginRequestMaxBytes = auth.PlatformPasswordLoginRequestMaxBytes
const platformOIDCLoginRequestMaxBytes = auth.PlatformOIDCLoginRequestMaxBytes
const defaultDevUserEmail = auth.DefaultDevUserEmail
const defaultDevUserPassword = auth.DefaultDevUserPassword
const defaultDevAdminEmail = auth.DefaultDevAdminEmail
const defaultDevAdminPassword = auth.DefaultDevAdminPassword

func platformDSNFromEnv() string { return auth.PlatformDSNFromEnv() }

func platformJWTSecretFromEnv() ([]byte, error) { return auth.PlatformJWTSecretFromEnv() }

func runPlatformAdminBootstrap(ctx context.Context) error {
	return auth.RunPlatformAdminBootstrap(ctx, func(ctx context.Context, dsn string, jwtSecret []byte) (auth.PasswordUserEnsurer, error) {
		return newPlatformStore(ctx, dsn, jwtSecret)
	})
}

func seedPlatformAdminFromEnv(ctx context.Context, store auth.PasswordUserEnsurer) error {
	return auth.SeedPlatformAdminFromEnv(ctx, store)
}

func seedPlatformDevUsersFromEnv(ctx context.Context, store auth.PasswordUserEnsurer) error {
	return auth.SeedPlatformDevUsersFromEnv(ctx, store, boolEnv, envOr)
}

func (s *apiServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	var backend auth.PasswordLoginBackend
	if s.platform != nil {
		backend = s.platform
	}
	auth.HandlePasswordLogin(
		w,
		r,
		backend,
		platformLoginAttempts,
		requestIP,
		requestSource,
		writeJSON,
		writeBodyDecodeError,
		auth.PlatformAccessTokenTTL,
		auth.PlatformPasswordLoginRequestMaxBytes,
	)
}

func (s *apiServer) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	var backend auth.PasswordLoginBackend
	if s.platform != nil {
		backend = s.platform
	}
	auth.HandleOIDCLogin(
		w,
		r,
		backend,
		s.authenticateRequest,
		func(ctx context.Context, idToken string) (platformUser, error) {
			if oidcLoginHook != nil {
				return oidcLoginHook(ctx, s, idToken)
			}
			u, err := auth.ResolveOIDCLoginUser(ctx, idToken, s.authenticateRequest, errOIDCUnauthorized)
			return platformUser(u), err
		},
		errOIDCUnauthorized,
		requestIP,
		requestSource,
		writeJSON,
		writeBodyDecodeError,
		auth.PlatformAccessTokenTTL,
		auth.PlatformOIDCLoginRequestMaxBytes,
		s.oidcIssuer,
		s.oidcAudience,
	)
}

func oidcAuditResource(idToken string) string { return auth.OIDCAuditResource(idToken) }
