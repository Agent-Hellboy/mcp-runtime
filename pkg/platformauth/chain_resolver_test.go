package platformauth

import (
	"context"
	"errors"
	"testing"
)

type stubUserKeyResolver struct {
	principal Principal
	ok        bool
	err       error
}

func (s stubUserKeyResolver) ResolveAPIKey(context.Context, string) (Principal, bool, error) {
	return s.principal, s.ok, s.err
}

func TestChainUserKeyResolversUsesFallback(t *testing.T) {
	resolver := ChainUserKeyResolvers(
		stubUserKeyResolver{},
		stubUserKeyResolver{principal: Principal{Subject: "user-1", Role: RoleUser, AuthType: "user_api_key"}, ok: true},
	)
	principal, ok, err := resolver.ResolveAPIKey(context.Background(), "key")
	if err != nil {
		t.Fatalf("ResolveAPIKey() error = %v", err)
	}
	if !ok || principal.Subject != "user-1" {
		t.Fatalf("ResolveAPIKey() = (%#v, %v), want user principal", principal, ok)
	}
}

func TestChainUserKeyResolversReturnsLastError(t *testing.T) {
	want := errors.New("resolver failed")
	resolver := ChainUserKeyResolvers(stubUserKeyResolver{err: want})
	_, ok, err := resolver.ResolveAPIKey(context.Background(), "key")
	if ok {
		t.Fatal("expected unresolved key")
	}
	if !errors.Is(err, want) {
		t.Fatalf("ResolveAPIKey() error = %v, want %v", err, want)
	}
}
