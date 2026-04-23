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

func TestNewNilConfigUsesDefaults(t *testing.T) {
	handler, err := New(context.Background(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), nil, "pii")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	middleware, ok := handler.(*Middleware)
	if !ok {
		t.Fatalf("handler type = %T, want *Middleware", handler)
	}
	if middleware.mask != "[redacted]" {
		t.Fatalf("mask = %q, want [redacted]", middleware.mask)
	}
	if middleware.maxBody != 1<<20 {
		t.Fatalf("maxBody = %d, want %d", middleware.maxBody, 1<<20)
	}
}

func TestOversizedRequestReturns413(t *testing.T) {
	called := false
	handler, err := New(context.Background(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}), &Config{MaxBodyBytes: 8}, "pii")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "http://traefik.local/api", strings.NewReader("0123456789"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	if called {
		t.Fatal("upstream handler should not be called for oversized body")
	}
}

func TestStreamingResponsesBypassBodyRedaction(t *testing.T) {
	handler, err := New(context.Background(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: alice@example.com\n\n"))
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("data: tok-abcdef123\n\n"))
	}), CreateConfig(), "pii")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://traefik.local/api", nil))

	body := rec.Body.String()
	if !strings.Contains(body, "alice@example.com") || !strings.Contains(body, "tok-abcdef123") {
		t.Fatalf("streaming body should pass through unredacted, got %q", body)
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
