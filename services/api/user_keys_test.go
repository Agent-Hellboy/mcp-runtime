package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeUserAPIKeyStore struct {
	key       userAPIKeySummary
	cleartext string
	principal principal
	ok        bool
	err       error
}

func (f *fakeUserAPIKeyStore) AuthenticateUserAPIKey(context.Context, string) (principal, bool, error) {
	return f.principal, f.ok, f.err
}

func (f *fakeUserAPIKeyStore) ListUserAPIKeys(context.Context, string) ([]userAPIKeySummary, error) {
	return nil, nil
}

func (f *fakeUserAPIKeyStore) CreateUserAPIKey(context.Context, string, string) (userAPIKeySummary, string, error) {
	return f.key, f.cleartext, nil
}

func (f *fakeUserAPIKeyStore) RevokeUserAPIKey(context.Context, string, string) (userAPIKeySummary, error) {
	return userAPIKeySummary{}, nil
}

func TestHandleUserAPIKeysReturnsOneTimeAlias(t *testing.T) {
	store := &fakeUserAPIKeyStore{
		key: userAPIKeySummary{
			ID:        "key-1",
			Name:      "laptop",
			Prefix:    "mcpu_123",
			CreatedAt: time.Now().UTC(),
		},
		cleartext: "mcpu_123456789",
	}
	server := &apiServer{userKeys: store}
	request := httptest.NewRequest(http.MethodPost, "/api/user/api-keys", strings.NewReader(`{"name":"laptop"}`))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:    roleUser,
		Subject: "user-1",
	}))
	recorder := httptest.NewRecorder()

	server.handleUserAPIKeys(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["api_key"] != store.cleartext {
		t.Fatalf("api_key = %#v, want cleartext", payload["api_key"])
	}
	if payload["one_time_key"] != store.cleartext {
		t.Fatalf("one_time_key = %#v, want cleartext", payload["one_time_key"])
	}
}
