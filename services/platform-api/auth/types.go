package auth

import (
	"context"
	"net/http"
	"time"

	"mcp-platform-api/internal/platformstore"
)

const PlatformAccessTokenTTL = 15 * time.Minute

const (
	PlatformSignupRequestMaxBytes        int64 = 4 * 1024
	PlatformPasswordLoginRequestMaxBytes int64 = 4 * 1024
	PlatformOIDCLoginRequestMaxBytes     int64 = 8 * 1024
)

type PasswordUserEnsurer interface {
	EnsurePasswordUser(ctx context.Context, email, password string, role string) (platformstore.User, error)
}

type PasswordLoginBackend interface {
	AuthenticatePassword(ctx context.Context, email, password string) (platformstore.User, bool, error)
	CreateAccessToken(user platformstore.User, ttl time.Duration) (string, error)
	WriteAudit(ctx context.Context, ev platformstore.AuditEvent)
}

type RequestIPFunc func(*http.Request) string
type RequestSourceFunc func(*http.Request) string
type JSONWriterFunc func(http.ResponseWriter, int, any)
type BodyDecodeErrorFunc func(http.ResponseWriter, error)

type OIDCRequestAuthenticator interface {
	AuthenticateRequest(*http.Request) (platformstore.Principal, bool, error)
}
