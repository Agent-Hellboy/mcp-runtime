package platformapi

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyAccessFromYAMLFile_MultiDocument(t *testing.T) {
	grantCalls := 0
	sessionCalls := 0
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("x-api-key") != "token-1" {
				t.Fatalf("x-api-key = %q, want token-1", r.Header.Get("x-api-key"))
			}
			if r.Header.Get("x-mcp-source") != "cli" {
				t.Fatalf("x-mcp-source = %q, want cli", r.Header.Get("x-mcp-source"))
			}
			switch r.URL.Path {
			case "/api/runtime/grants":
				grantCalls++
			case "/api/runtime/sessions":
				sessionCalls++
			default:
				t.Fatalf("unexpected path %q", r.URL.Path)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}),
	}

	d := t.TempDir()
	manifest := filepath.Join(d, "access.yaml")
	if err := os.WriteFile(manifest, []byte(`apiVersion: mcpruntime.org/v1alpha1
kind: MCPAccessGrant
metadata:
  name: grant-a
  namespace: mcp-servers
spec:
  serverRef:
    name: demo
  subject:
    humanID: user-1
  maxTrust: low
  toolRules:
    - name: add
      decision: allow
---
apiVersion: mcpruntime.org/v1alpha1
kind: MCPAgentSession
metadata:
  name: session-a
  namespace: mcp-servers
spec:
  serverRef:
    name: demo
  subject:
    humanID: user-1
  consentedTrust: low
`), 0o600); err != nil {
		t.Fatal(err)
	}

	client := &PlatformClient{
		baseURL:   "https://platform.example.com",
		token:     "token-1",
		http:      httpClient,
		apiPrefix: "/api",
	}
	if err := client.ApplyAccessFromYAMLFile(context.Background(), manifest); err != nil {
		t.Fatalf("ApplyAccessFromYAMLFile() error = %v", err)
	}
	if grantCalls != 1 || sessionCalls != 1 {
		t.Fatalf("calls = grant:%d session:%d, want 1/1", grantCalls, sessionCalls)
	}
}

func TestRecordImagePublish(t *testing.T) {
	var seenBody string
	client := &PlatformClient{
		baseURL:   "https://platform.example.com",
		token:     "token-1",
		apiPrefix: "/api",
		http: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/api/user/activity/image-publish" {
				t.Fatalf("path = %q, want image publish endpoint", r.URL.Path)
			}
			if r.Header.Get("x-mcp-source") != "cli" {
				t.Fatalf("x-mcp-source = %q, want cli", r.Header.Get("x-mcp-source"))
			}
			body, _ := io.ReadAll(r.Body)
			seenBody = string(body)
			return &http.Response{StatusCode: http.StatusAccepted, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
		})},
	}
	err := client.RecordImagePublish(context.Background(), ImagePublishRecord{
		ImageRef:    "registry.example.com/team/demo:v1",
		SourceImage: "demo:v1",
		Mode:        "direct",
	})
	if err != nil {
		t.Fatalf("RecordImagePublish() error = %v", err)
	}
	if !strings.Contains(seenBody, `"image_ref":"registry.example.com/team/demo:v1"`) {
		t.Fatalf("body = %s", seenBody)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}
