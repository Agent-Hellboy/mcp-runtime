package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
	"mcp-runtime/internal/cli/core"
	"mcp-runtime/pkg/authfile"
)

func TestVerifyPlatformAPIToken(t *testing.T) {
	prevHook := httpDoHook
	httpDoHook = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/auth/me" {
			t.Errorf("path: %q", r.URL.Path)
			return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(bytes.NewReader(nil))}, nil
		}
		if r.Header.Get("x-api-key") != "k" {
			t.Errorf("x-api-key header")
			return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(bytes.NewReader(nil))}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte("[]")))}, nil
	}
	defer func() { httpDoHook = prevHook }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := verifyPlatformAPIToken(ctx, "https://platform.example.com", "k"); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyPlatformAPITokenUnauthorized(t *testing.T) {
	prevHook := httpDoHook
	httpDoHook = func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(bytes.NewReader(nil))}, nil
	}
	defer func() { httpDoHook = prevHook }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := verifyPlatformAPIToken(ctx, "https://platform.example.com", "k"); err == nil {
		t.Fatal("expected error")
	}
}

func TestAuthLoginSavesAndVerifies(t *testing.T) {
	d := t.TempDir()
	t.Setenv("MCP_RUNTIME_CONFIG_DIR", d)

	prevHTTPHook := httpDoHook
	httpDoHook = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/auth/me" {
			t.Errorf("path: %q", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "good" {
			t.Errorf("x-api-key")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte("[]")))}, nil
	}
	defer func() { httpDoHook = prevHTTPHook }()

	cmd := New(core.NewRuntime(zap.NewNop()))
	var out, errb bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetArgs([]string{"login", "--api-url", "https://platform.example.com", "--token", "good"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v stderr=%s", err, errb.String())
	}
	b, rerr := os.ReadFile(filepath.Join(d, "credentials.json"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	var creds authfile.Credentials
	if err := json.Unmarshal(b, &creds); err != nil {
		t.Fatalf("unmarshal credentials: %v", err)
	}
	if creds.Token != "good" {
		t.Fatalf("token = %q, want good", creds.Token)
	}
	if creds.APIBaseURL != "https://platform.example.com" {
		t.Fatalf("api_url = %q, want https://platform.example.com", creds.APIBaseURL)
	}
}

func TestAuthLoginNormalizesTrailingAPIPath(t *testing.T) {
	d := t.TempDir()
	t.Setenv("MCP_RUNTIME_CONFIG_DIR", d)
	previousHook := apiTestHook
	apiTestHook = func(_ context.Context, apiBaseURL, token string) error {
		if apiBaseURL != "https://platform.example.com" {
			t.Fatalf("apiBaseURL = %q, want https://platform.example.com", apiBaseURL)
		}
		if token != "good" {
			t.Fatalf("token = %q, want good", token)
		}
		return nil
	}
	defer func() { apiTestHook = previousHook }()

	cmd := New(core.NewRuntime(zap.NewNop()))
	cmd.SetArgs([]string{"login", "--api-url", "https://platform.example.com/api/", "--token", "good"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(d, "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	var creds authfile.Credentials
	if err := json.Unmarshal(b, &creds); err != nil {
		t.Fatalf("unmarshal credentials: %v", err)
	}
	if creds.APIBaseURL != "https://platform.example.com" {
		t.Fatalf("api_url = %q, want https://platform.example.com", creds.APIBaseURL)
	}
	if creds.Token != "good" {
		t.Fatalf("token = %q, want good", creds.Token)
	}
}
