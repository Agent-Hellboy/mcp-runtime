package main

import (
	"context"
	"net/http"

	"mcp-runtime/pkg/platformauth"
)

func (s *apiServer) requireRole(role string, next http.Handler) http.Handler {
	return s.authentic.RequireRole(role, next)
}

func (s *apiServer) auth(next http.Handler) http.Handler {
	return s.authentic.Middleware(next)
}

type fakeUserAPIKeyStore struct {
	ok        bool
	err       error
	principal principal
}

func (f *fakeUserAPIKeyStore) AuthenticateUserAPIKey(context.Context, string) (principal, bool, error) {
	return f.principal, f.ok, f.err
}

func (f *fakeUserAPIKeyStore) ListUserAPIKeys(context.Context, string) ([]userAPIKeySummary, error) {
	return nil, nil
}

func (f *fakeUserAPIKeyStore) CreateUserAPIKey(context.Context, string, string) (userAPIKeySummary, string, error) {
	return userAPIKeySummary{}, "", nil
}

func (f *fakeUserAPIKeyStore) RevokeUserAPIKey(context.Context, string, string) (userAPIKeySummary, error) {
	return userAPIKeySummary{}, nil
}

type fakeUserKeyResolver struct {
	fake *fakeUserAPIKeyStore
}

func (f fakeUserKeyResolver) ResolveAPIKey(ctx context.Context, rawKey string) (platformauth.Principal, bool, error) {
	p, ok, err := f.fake.AuthenticateUserAPIKey(ctx, rawKey)
	return platformauth.Principal(p), ok, err
}

func testAuthenticator(s *apiServer) {
	resolver := platformauth.UserKeyResolver(nil)
	if s.userKeys != nil {
		switch keys := s.userKeys.(type) {
		case *platformStore:
			if keys != nil {
				resolver = storeAPIKeyResolver{store: keys}
			}
		case *fakeUserAPIKeyStore:
			resolver = fakeUserKeyResolver{fake: keys}
		}
	}
	s.authentic = platformauth.Authenticator{
		Audience:        platformauth.AudiencePlatform,
		ServiceAPIKeys:  s.apiKeys,
		AdminAPIKeys:    s.adminAPIKeys,
		UserKeyResolver: resolver,
	}
}
