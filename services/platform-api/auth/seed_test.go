package auth

import (
	"testing"
)

func TestJWTSecretFromEnv(t *testing.T) {
	t.Setenv("JWT_SECRET", "canonical-secret-value-32bytes-min")

	got, err := JWTSecretFromEnv()
	if err != nil {
		t.Fatalf("JWTSecretFromEnv: %v", err)
	}
	if string(got) != "canonical-secret-value-32bytes-min" {
		t.Fatalf("got %q, want JWT_SECRET value", string(got))
	}
}

func TestJWTSecretFromEnvRequiresValue(t *testing.T) {
	t.Setenv("JWT_SECRET", "")

	if _, err := JWTSecretFromEnv(); err == nil {
		t.Fatal("expected error when JWT_SECRET is unset")
	}
}
