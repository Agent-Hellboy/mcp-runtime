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
	var seenAuthorization string
	var seenAPIKey string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seenBody = string(body)
		seenAuthorization = r.Header.Get("Authorization")
		seenAPIKey = r.Header.Get("X-Api-Key")

		w.Header().Set("X-Request-Id", "123e4567-e89b-12d3-a456-426614174000")
		w.Header().Set("X-Custom-Token", "secret-123456789")
		w.Header().Set("Authorization", "Bearer keep-me")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"email":"bob@example.com","phone":"+1 202 555 0188","token":"secret-123","uuid":"123e4567-e89b-12d3-a456-426614174000"}`))
	})

	handler, err := New(context.Background(), next, cfg, "pii")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "http://traefik.local/api", strings.NewReader(`{"email":"alice@example.com","phone":"+1-202-555-0188","ssn":"111-22-3333","note":"id 123e4567-e89b-12d3-a456-426614174000"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer internal-token")
	req.Header.Set("X-Api-Key", "sk-internal-123") // bypassed
	req.Header.Set("X-Custom-Token", "tok-secret-123456789")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertGolden(t, "testdata/request_body.golden", seenBody)
	assertGolden(t, "testdata/response_body.golden", rec.Body.String())

	if got := seenAuthorization; got != "Bearer internal-token" {
		t.Fatalf("request Authorization header should be bypassed, got %q", got)
	}
	if got := seenAPIKey; got != "sk-internal-123" {
		t.Fatalf("request X-Api-Key header should be bypassed, got %q", got)
	}

	// Response headers: sensitive values must always be redacted.
	if got := rec.Header().Get("X-Custom-Token"); got != cfg.MaskReplacement {
		t.Fatalf("X-Custom-Token header not redacted, got %q", got)
	}
	if got := rec.Header().Get("Authorization"); got != cfg.MaskReplacement {
		t.Fatalf("Authorization response header should be redacted, got %q", got)
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
	req.Header.Set("Content-Type", "application/json")
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
		w.Header().Set("Authorization", "Bearer keep-me")
		w.Header().Set("X-Api-Key", "stream-key")
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
	if got := rec.Header().Get("Authorization"); got != "[redacted]" {
		t.Fatalf("streaming Authorization header should be redacted, got %q", got)
	}
	if got := rec.Header().Get("X-Api-Key"); got != "[redacted]" {
		t.Fatalf("streaming X-Api-Key header should be redacted, got %q", got)
	}
}

func TestBinaryAndCompressedRequestsBypassBodyRedaction(t *testing.T) {
	tests := []struct {
		name            string
		contentType     string
		contentEncoding string
		body            string
	}{
		{
			name:        "binary payload",
			contentType: "application/octet-stream",
			body:        "\x00alice@example.com\x01",
		},
		{
			name:            "compressed json",
			contentType:     "application/json",
			contentEncoding: "gzip",
			body:            `{"email":"alice@example.com"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var seenBody string
			handler, err := New(context.Background(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				seenBody = string(body)
				w.WriteHeader(http.StatusNoContent)
			}), CreateConfig(), "pii")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			req := httptest.NewRequest(http.MethodPost, "http://traefik.local/upload", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			if tc.contentEncoding != "" {
				req.Header.Set("Content-Encoding", tc.contentEncoding)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
			}
			if seenBody != tc.body {
				t.Fatalf("body should pass through unchanged, got %q want %q", seenBody, tc.body)
			}
		})
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
