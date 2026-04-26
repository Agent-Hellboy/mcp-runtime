package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestVerifyPlatformAPIToken(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/runtime/servers" {
			t.Errorf("path: %q", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		if r.Header.Get("x-api-key") != "k" {
			t.Errorf("x-api-key header")
			w.WriteHeader(500)
			return
		}
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := verifyPlatformAPIToken(ctx, srv.URL, "k"); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyPlatformAPIToken_Unauthorized(t *testing.T) {
	t.Parallel()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := verifyPlatformAPIToken(ctx, s.URL, "k"); err == nil {
		t.Fatal("expected error")
	}
}

func TestAuthLoginSavesAndVerifies(t *testing.T) {
	d := t.TempDir()
	t.Setenv("MCP_RUNTIME_CONFIG_DIR", d)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/runtime/servers" {
			t.Errorf("path: %q", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "good" {
			t.Errorf("x-api-key")
		}
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	cmd := NewAuthCmd(zap.NewNop())
	var out, errb bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetArgs([]string{"login", "--api-url", srv.URL, "--token", "good"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v stderr=%s", err, errb.String())
	}
	b, rerr := os.ReadFile(filepath.Join(d, "credentials.json"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !bytes.Contains(b, []byte("good")) {
		t.Fatalf("credentials: %s", b)
	}
}
