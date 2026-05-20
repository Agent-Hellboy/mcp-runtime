package platformapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
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
				body, _ := io.ReadAll(r.Body)
				var payload grantAPIBody
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("decode grant body: %v", err)
				}
				if payload.Subject.TeamID != "team-acme" {
					t.Fatalf("grant subject teamID = %q, want team-acme", payload.Subject.TeamID)
				}
			case "/api/runtime/sessions":
				sessionCalls++
				body, _ := io.ReadAll(r.Body)
				var payload sessionAPIBody
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Fatalf("decode session body: %v", err)
				}
				if payload.Subject.TeamID != "team-acme" {
					t.Fatalf("session subject teamID = %q, want team-acme", payload.Subject.TeamID)
				}
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
    teamID: team-acme
  maxTrust: low
  allowedSideEffects:
    - read
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
    teamID: team-acme
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

func TestValidateCredentials(t *testing.T) {
	client := &PlatformClient{
		baseURL:   "https://platform.example.com",
		token:     "token-1",
		apiPrefix: "/api",
		http: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/api/auth/me" {
				t.Fatalf("path = %q, want auth/me endpoint", r.URL.Path)
			}
			if r.Header.Get("x-api-key") != "token-1" {
				t.Fatalf("x-api-key = %q, want token-1", r.Header.Get("x-api-key"))
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"principal":{"role":"user"}}`))}, nil
		})},
	}
	if err := client.ValidateCredentials(context.Background()); err != nil {
		t.Fatalf("ValidateCredentials() error = %v", err)
	}
}

func TestPlatformClientTeamAndServerRoutes(t *testing.T) {
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("x-api-key") != "token-1" {
				t.Fatalf("x-api-key = %q, want token-1", r.Header.Get("x-api-key"))
			}
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/runtime/teams":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"teams":[{"slug":"core","name":"Core","namespace":"mcp-team-core"}]}`)),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/api/runtime/teams/core":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"team":{"slug":"core","name":"Core","namespace":"mcp-team-core"}}`)),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/api/runtime/teams/core/members":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"members":[{"team_slug":"core","team_namespace":"mcp-team-core","user_id":"user-1","email":"member@example.com","role":"member"}]}`)),
				}, nil
			case r.Method == http.MethodPost && r.URL.Path == "/api/runtime/teams":
				body, _ := io.ReadAll(r.Body)
				var payload map[string]string
				_ = json.Unmarshal(body, &payload)
				if payload["slug"] != "core" || payload["name"] != "Core Team" {
					t.Fatalf("create team payload = %#v", payload)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"team":{"slug":"core","name":"Core Team","namespace":"mcp-team-core"}}`)),
				}, nil
			case r.Method == http.MethodPost && r.URL.Path == "/api/runtime/teams/core/users":
				body, _ := io.ReadAll(r.Body)
				var payload map[string]string
				_ = json.Unmarshal(body, &payload)
				if payload["email"] != "member@example.com" || payload["password"] != "password123" || payload["role"] != "member" {
					t.Fatalf("create team user payload = %#v", payload)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"user":{"id":"user-1","email":"member@example.com","role":"user"},"membership":{"team_slug":"core","team_namespace":"mcp-team-core","user_id":"user-1","role":"member"}}`)),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/api/runtime/servers":
				if got := r.URL.Query().Get("namespace"); got != "" {
					t.Fatalf("list runtime servers namespace query = %q, want empty", got)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"servers":[]}`)),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/api/auth/me":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"authenticated":true,"principal":{"role":"user","namespace":"mcp-team-core","teams":[{"slug":"core","namespace":"mcp-team-core"}]}}`)),
				}, nil
			case r.Method == http.MethodPost && r.URL.Path == "/api/runtime/servers":
				body, _ := io.ReadAll(r.Body)
				var payload map[string]any
				_ = json.Unmarshal(body, &payload)
				if payload["name"] != "demo" {
					t.Fatalf("server name payload = %#v", payload)
				}
				if payload["scope"] != "tenant" {
					t.Fatalf("server scope payload = %#v", payload)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"server":{"name":"demo","namespace":"mcp-team-core"}}`)),
				}, nil
			case r.Method == http.MethodDelete && r.URL.Path == "/api/runtime/servers/mcp-team-core/demo":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"success":true}`)),
				}, nil
			default:
				t.Fatalf("unexpected route %s %s", r.Method, r.URL.Path)
				return nil, nil
			}
		}),
	}
	client := &PlatformClient{
		baseURL:   "https://platform.example.com",
		token:     "token-1",
		http:      httpClient,
		apiPrefix: "/api",
	}
	if _, err := client.ListTeams(context.Background()); err != nil {
		t.Fatalf("ListTeams() error = %v", err)
	}
	if _, err := client.GetTeam(context.Background(), "core"); err != nil {
		t.Fatalf("GetTeam() error = %v", err)
	}
	if _, err := client.CreateTeam(context.Background(), "core", "Core Team"); err != nil {
		t.Fatalf("CreateTeam() error = %v", err)
	}
	members, err := client.ListTeamMembers(context.Background(), "core")
	if err != nil {
		t.Fatalf("ListTeamMembers() error = %v", err)
	}
	if len(members) != 1 || members[0].Email != "member@example.com" {
		t.Fatalf("members = %#v", members)
	}
	created, err := client.CreateTeamUser(context.Background(), "core", "member@example.com", "password123", "member")
	if err != nil {
		t.Fatalf("CreateTeamUser() error = %v", err)
	}
	if created.Email != "member@example.com" || created.TeamSlug != "core" {
		t.Fatalf("created membership = %#v", created)
	}
	if _, err := client.ListRuntimeServers(context.Background(), ""); err != nil {
		t.Fatalf("ListRuntimeServers() error = %v", err)
	}
	principal, err := client.CurrentPrincipal(context.Background())
	if err != nil {
		t.Fatalf("CurrentPrincipal() error = %v", err)
	}
	if principal.Namespace != "mcp-team-core" || len(principal.Teams) != 1 || principal.Teams[0].Slug != "core" {
		t.Fatalf("principal = %#v", principal)
	}
	if _, err := client.ApplyRuntimeServerWithScope(context.Background(), "demo", "mcp-team-core", "tenant", mcpv1alpha1.MCPServerSpec{Image: "registry.example/core/demo", ImageTag: "latest"}); err != nil {
		t.Fatalf("ApplyRuntimeServer() error = %v", err)
	}
	if err := client.DeleteRuntimeServer(context.Background(), "mcp-team-core", "demo"); err != nil {
		t.Fatalf("DeleteRuntimeServer() error = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}
