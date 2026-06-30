package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakePasswordUserEnsurer struct {
	calls []ensurePasswordUserCall
	err   error
}

type ensurePasswordUserCall struct {
	email    string
	password string
	role     string
}

func (f *fakePasswordUserEnsurer) EnsurePasswordUser(_ context.Context, email, password string, role string) (platformUser, error) {
	f.calls = append(f.calls, ensurePasswordUserCall{email: email, password: password, role: role})
	if f.err != nil {
		return platformUser{}, f.err
	}
	return platformUser{Email: email, Role: role, Namespace: "user-test"}, nil
}

func TestSeedPlatformDevUsersFromEnvDisabledByDefault(t *testing.T) {
	store := &fakePasswordUserEnsurer{}

	if err := seedPlatformDevUsersFromEnv(context.Background(), store); err != nil {
		t.Fatalf("seedPlatformDevUsersFromEnv() error = %v", err)
	}
	if len(store.calls) != 0 {
		t.Fatalf("expected no dev users when disabled, got %#v", store.calls)
	}
}

func TestSeedPlatformDevUsersFromEnvSeedsDefaultAccounts(t *testing.T) {
	t.Setenv("PLATFORM_DEV_LOGIN_ENABLED", "true")
	store := &fakePasswordUserEnsurer{}

	if err := seedPlatformDevUsersFromEnv(context.Background(), store); err != nil {
		t.Fatalf("seedPlatformDevUsersFromEnv() error = %v", err)
	}
	want := []ensurePasswordUserCall{
		{email: defaultDevUserEmail, password: defaultDevUserPassword, role: roleUser},
		{email: defaultDevAdminEmail, password: defaultDevAdminPassword, role: roleAdmin},
	}
	if len(store.calls) != len(want) {
		t.Fatalf("seed calls = %#v, want %#v", store.calls, want)
	}
	for i := range want {
		if store.calls[i] != want[i] {
			t.Fatalf("seed call %d = %#v, want %#v", i, store.calls[i], want[i])
		}
	}
}

func TestSeedPlatformDevUsersFromEnvPropagatesEnsureErrors(t *testing.T) {
	t.Setenv("PLATFORM_DEV_LOGIN_ENABLED", "true")
	store := &fakePasswordUserEnsurer{err: errors.New("boom")}

	err := seedPlatformDevUsersFromEnv(context.Background(), store)
	if err == nil {
		t.Fatal("expected error")
	}
	if len(store.calls) != 1 {
		t.Fatalf("expected seeding to stop at first failure, got calls %#v", store.calls)
	}
}

func TestOpenPlatformStoreWithRetryRetriesUntilSuccess(t *testing.T) {
	originalInterval := platformStoreConnectRetryInterval
	originalAttemptTimeout := platformStoreConnectAttemptTimeout
	platformStoreConnectRetryInterval = time.Millisecond
	platformStoreConnectAttemptTimeout = time.Second
	t.Cleanup(func() {
		platformStoreConnectRetryInterval = originalInterval
		platformStoreConnectAttemptTimeout = originalAttemptTimeout
	})

	calls := 0
	store, err := openPlatformStoreWithRetry(context.Background(), "postgres://example", []byte("secret"), func(context.Context, string, []byte) (*platformStore, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("postgres not ready")
		}
		return newTestPlatformStore([]byte("secret")), nil
	})
	if err != nil {
		t.Fatalf("openPlatformStoreWithRetry() error = %v", err)
	}
	if store == nil {
		t.Fatal("expected platform store")
	}
	if calls != 2 {
		t.Fatalf("open attempts = %d, want 2", calls)
	}
}
