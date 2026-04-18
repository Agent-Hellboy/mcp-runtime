package pii_redactor

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestRedactionPipeline(t *testing.T) {
	cfg := CreateConfig()

	var seenBody string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seenBody = string(body)

		w.Header().Set("X-Request-Id", "123e4567-e89b-12d3-a456-426614174000")
		w.Header().Set("X-Custom-Token", "secret-123456789")
		w.Header().Set("Authorization", "Bearer keep-me") // bypassed
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"email":"bob@example.com","phone":"+1 202 555 0188","token":"secret-123","uuid":"123e4567-e89b-12d3-a456-426614174000"}`))
	})

	handler, err := New(context.Background(), next, cfg, "pii")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "http://traefik.local/api", strings.NewReader(`{"email":"alice@example.com","phone":"+1-202-555-0188","ssn":"111-22-3333","note":"id 123e4567-e89b-12d3-a456-426614174000"}`))
	req.Header.Set("Authorization", "Bearer internal-token")
	req.Header.Set("X-Api-Key", "sk-internal-123") // bypassed
	req.Header.Set("X-Custom-Token", "tok-secret-123456789")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertGolden(t, "testdata/request_body.golden", seenBody)
	assertGolden(t, "testdata/response_body.golden", rec.Body.String())

	// Response headers: custom token must be redacted, authorization preserved via bypass.
	if got := rec.Header().Get("X-Custom-Token"); got != cfg.MaskReplacement {
		t.Fatalf("X-Custom-Token header not redacted, got %q", got)
	}
	if got := rec.Header().Get("Authorization"); got != "Bearer keep-me" {
		t.Fatalf("Authorization header should be bypassed, got %q", got)
	}
}

func assertGolden(t *testing.T, path, actual string) {
	t.Helper()
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	if !bytes.Equal(bytes.TrimSpace(want), bytes.TrimSpace([]byte(actual))) {
		t.Fatalf("mismatch for %s\nwant: %s\n got: %s", path, string(want), actual)
	}
}
