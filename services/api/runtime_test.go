package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
	sentinelaccess "mcp-runtime/pkg/access"
	"mcp-runtime/pkg/k8sclient"
)

func TestValidateGrantRequestDefaultsAndNormalizes(t *testing.T) {
	req := &accessGrantRequest{
		Name: " grant-a ",
		ServerRef: sentinelaccess.ServerReference{
			Name: " demo ",
		},
		Subject: sentinelaccess.SubjectRef{
			HumanID: " user-1 ",
		},
		MaxTrust: sentinelaccess.TrustLevel(" high "),
		ToolRules: []sentinelaccess.ToolRule{
			{Name: " aaa-ping ", Decision: sentinelaccess.PolicyDecision(" allow ")},
		},
	}

	if err := validateGrantRequest(req); err != nil {
		t.Fatalf("validateGrantRequest returned error: %v", err)
	}
	if req.Name != "grant-a" {
		t.Fatalf("Name = %q, want grant-a", req.Name)
	}
	if req.Namespace != sentinelaccess.DefaultMCPResourceNamespace {
		t.Fatalf("Namespace = %q, want mcp-servers", req.Namespace)
	}
	if req.PolicyVersion != "v1" {
		t.Fatalf("PolicyVersion = %q, want v1", req.PolicyVersion)
	}
	if req.ToolRules[0].Name != "aaa-ping" || req.ToolRules[0].Decision != "allow" {
		t.Fatalf("tool rule was not normalized: %#v", req.ToolRules[0])
	}
}

func TestValidateGrantRequestRejectsInvalidToolRule(t *testing.T) {
	req := &accessGrantRequest{
		Name:      "grant-a",
		ServerRef: sentinelaccess.ServerReference{Name: "demo"},
		Subject:   sentinelaccess.SubjectRef{HumanID: "user-1"},
		ToolRules: []sentinelaccess.ToolRule{
			{Name: "aaa-ping", Decision: sentinelaccess.PolicyDecision("audit")},
		},
	}

	err := validateGrantRequest(req)
	if err == nil || !strings.Contains(err.Error(), "decision must be allow or deny") {
		t.Fatalf("validateGrantRequest error = %v, want invalid decision", err)
	}
}

func TestValidateSessionRequestRequiresSubject(t *testing.T) {
	req := &accessSessionRequest{
		Name:           "session-a",
		ServerRef:      sentinelaccess.ServerReference{Name: "demo"},
		ConsentedTrust: sentinelaccess.TrustLevel("low"),
	}

	err := validateSessionRequest(req)
	if err == nil || !strings.Contains(err.Error(), "either subject.humanID or subject.agentID is required") {
		t.Fatalf("validateSessionRequest error = %v, want subject requirement", err)
	}
}

func newTestAccessManager(t *testing.T) *sentinelaccess.Manager {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	srv := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: sentinelaccess.DefaultMCPResourceNamespace,
		},
	}
	return sentinelaccess.NewManager(dynamicfake.NewSimpleDynamicClient(scheme, srv), nil)
}

func TestRuntimeGrantApplyRejectsUnknownServer(t *testing.T) {
	accessMgr := newTestAccessManager(t)
	server := &RuntimeServer{accessMgr: accessMgr}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/grants", bytes.NewReader([]byte(`{
		"name": "grant-orphan",
		"namespace": "mcp-servers",
		"serverRef": {"name": "definitely-missing", "namespace": "mcp-servers"},
		"subject": {"humanID": "user-1"},
		"maxTrust": "low"
	}`)))
	server.handleRuntimeGrantApply(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "unknown serverRef") {
		t.Fatalf("body = %q, want unknown serverRef", recorder.Body.String())
	}
}

func TestRuntimeSessionApplyRejectsUnknownServer(t *testing.T) {
	accessMgr := newTestAccessManager(t)
	server := &RuntimeServer{accessMgr: accessMgr}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/sessions", bytes.NewReader([]byte(`{
		"name": "sess-orphan",
		"namespace": "mcp-servers",
		"serverRef": {"name": "definitely-missing", "namespace": "mcp-servers"},
		"subject": {"humanID": "user-1"},
		"consentedTrust": "low"
	}`)))
	server.handleRuntimeSessionApply(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestRuntimeServersIncludesMCPServerInventory(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	srv := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "demo-one",
			Namespace:         "mcp-servers",
			CreationTimestamp: metav1.Now(),
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Image:            "demo:latest",
			PublicPathPrefix: "demo-one",
			Tools: []mcpv1alpha1.ToolConfig{
				{Name: "add", Description: "Add two numbers", RequiredTrust: mcpv1alpha1.TrustLevelLow},
			},
			Prompts: []mcpv1alpha1.InventoryItem{
				{Name: "summarize"},
			},
			MCPResources: []mcpv1alpha1.InventoryItem{
				{Name: "repo://README.md"},
			},
			Tasks: []mcpv1alpha1.InventoryItem{
				{Name: "triage-incident"},
			},
		},
	}
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic:   dynamicfake.NewSimpleDynamicClient(scheme, srv),
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/runtime/servers", nil)
	request = request.WithContext(context.WithValue(request.Context(), principalContextKey{}, principal{Role: roleAdmin, Subject: "admin-1"}))
	server.handleRuntimeServers(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Servers []serverInfo `json:"servers"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Servers) != 1 {
		t.Fatalf("servers = %d, want 1", len(payload.Servers))
	}
	got := payload.Servers[0]
	if got.Name != "demo-one" || len(got.Tools) != 1 || got.Tools[0].Name != "add" {
		t.Fatalf("server inventory = %#v", got)
	}
	if len(got.Prompts) != 1 || got.Prompts[0].Name != "summarize" {
		t.Fatalf("prompts = %#v", got.Prompts)
	}
	if len(got.Resources) != 1 || got.Resources[0].Name != "repo://README.md" {
		t.Fatalf("resources = %#v", got.Resources)
	}
	if len(got.Tasks) != 1 || got.Tasks[0].Name != "triage-incident" {
		t.Fatalf("tasks = %#v", got.Tasks)
	}
	if got.Endpoint != "/demo-one/mcp" {
		t.Fatalf("endpoint = %q, want /demo-one/mcp", got.Endpoint)
	}
	if got.AccessJSON == nil {
		t.Fatalf("access_json missing: %#v", got)
	}
	rawServers, ok := got.AccessJSON["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("access_json.mcpServers = %#v", got.AccessJSON["mcpServers"])
	}
	rawServer, ok := rawServers["demo-one"].(map[string]any)
	if !ok {
		t.Fatalf("access_json.mcpServers.demo-one = %#v", rawServers["demo-one"])
	}
	if rawServer["type"] != "http" || rawServer["url"] != "/demo-one/mcp" {
		t.Fatalf("access_json server payload = %#v", rawServer)
	}
	if _, ok := rawServer["headers"]; ok {
		t.Fatalf("access_json should not include headers: %#v", rawServer)
	}
}

func TestRuntimeServersNonAdminDefaultsToSharedNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	shared := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "shared-server",
			Namespace: "mcp-servers",
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Image: "demo:latest",
		},
	}
	private := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "private-server",
			Namespace: "user-1",
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Image: "demo:latest",
		},
	}
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic:   dynamicfake.NewSimpleDynamicClient(scheme, shared, private),
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
	}

	request := httptest.NewRequest(http.MethodGet, "/api/runtime/servers", nil)
	request = request.WithContext(context.WithValue(request.Context(), principalContextKey{}, principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "user-1",
	}))
	recorder := httptest.NewRecorder()
	server.handleRuntimeServers(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Servers []serverInfo `json:"servers"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Servers) != 1 || payload.Servers[0].Name != "shared-server" {
		t.Fatalf("servers = %#v, want only shared-server", payload.Servers)
	}
}

func TestRuntimeServersNonAdminRejectsOtherNamespace(t *testing.T) {
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic:   dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()),
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/runtime/servers?namespace=another-ns", nil)
	request = request.WithContext(context.WithValue(request.Context(), principalContextKey{}, principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "user-1",
	}))
	recorder := httptest.NewRecorder()
	server.handleRuntimeServers(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestScopedNamespaceForPrincipal(t *testing.T) {
	server := &RuntimeServer{}
	userCtx := context.WithValue(context.Background(), principalContextKey{}, principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "user-1",
	})

	got, err := server.scopedNamespaceForPrincipal(userCtx, "")
	if err != nil || got != "user-1" {
		t.Fatalf("scoped namespace default = %q err=%v, want user-1 nil", got, err)
	}
	got, err = server.scopedNamespaceForPrincipal(userCtx, "user-1")
	if err != nil || got != "user-1" {
		t.Fatalf("scoped namespace explicit = %q err=%v, want user-1 nil", got, err)
	}
	if _, err := server.scopedNamespaceForPrincipal(userCtx, "mcp-servers"); err == nil {
		t.Fatal("expected forbidden namespace error")
	}
}

func TestRuntimeGrantApplyNonAdminDefaultsToPrincipalNamespace(t *testing.T) {
	ctx := context.Background()
	accessMgr := newTestAccessManager(t)
	server := &RuntimeServer{accessMgr: accessMgr}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/grants", bytes.NewReader([]byte(`{
		"name": "grant-user",
		"serverRef": {"name": "demo"},
		"subject": {"humanID": "user-1"},
		"maxTrust": "low"
	}`)))
	request = request.WithContext(context.WithValue(request.Context(), principalContextKey{}, principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "user-1",
	}))
	server.handleRuntimeGrantApply(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if _, err := accessMgr.GetGrant(ctx, "grant-user", "user-1"); err != nil {
		t.Fatalf("expected grant in user namespace: %v", err)
	}
}

func TestRuntimeGrantDeleteMapsNotFound(t *testing.T) {
	server := &RuntimeServer{accessMgr: newTestAccessManager(t)}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/runtime/grants/mcp-servers/missing", nil)
	server.handleGrantDelete(recorder, request, "mcp-servers", "missing")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestRuntimeSessionDeleteMapsNotFound(t *testing.T) {
	server := &RuntimeServer{accessMgr: newTestAccessManager(t)}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/runtime/sessions/mcp-servers/missing", nil)
	server.handleSessionDelete(recorder, request, "mcp-servers", "missing")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestRuntimeGrantApplyPreservesOmittedDisabled(t *testing.T) {
	ctx := context.Background()
	accessMgr := newTestAccessManager(t)
	server := &RuntimeServer{accessMgr: accessMgr}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/grants", bytes.NewReader([]byte(`{
		"name": "grant-new",
		"namespace": "mcp-servers",
		"serverRef": {"name": "demo"},
		"subject": {"humanID": "user-1"},
		"maxTrust": "low"
	}`)))
	server.handleRuntimeGrantApply(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("new grant status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	if _, err := accessMgr.ApplyGrant(ctx, &sentinelaccess.MCPAccessGrant{
		ObjectMeta: metav1.ObjectMeta{Name: "grant-a", Namespace: "mcp-servers"},
		Spec: sentinelaccess.MCPAccessGrantSpec{
			ServerRef: sentinelaccess.ServerReference{Name: "demo"},
			Subject:   sentinelaccess.SubjectRef{HumanID: "user-1"},
			MaxTrust:  sentinelaccess.TrustLevel("low"),
			Disabled:  true,
		},
	}); err != nil {
		t.Fatalf("seed grant: %v", err)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/runtime/grants", bytes.NewReader([]byte(`{
		"name": "grant-a",
		"namespace": "mcp-servers",
		"serverRef": {"name": "demo"},
		"subject": {"humanID": "user-1"},
		"maxTrust": "high"
	}`)))
	server.handleRuntimeGrantApply(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	grant, err := accessMgr.GetGrant(ctx, "grant-a", "mcp-servers")
	if err != nil {
		t.Fatalf("get grant: %v", err)
	}
	if !grant.Spec.Disabled {
		t.Fatalf("omitted disabled reset grant state: %#v", grant.Spec)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/runtime/grants", bytes.NewReader([]byte(`{
		"name": "grant-a",
		"namespace": "mcp-servers",
		"serverRef": {"name": "demo"},
		"subject": {"humanID": "user-1"},
		"maxTrust": "high",
		"disabled": false
	}`)))
	server.handleRuntimeGrantApply(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	grant, err = accessMgr.GetGrant(ctx, "grant-a", "mcp-servers")
	if err != nil {
		t.Fatalf("get grant after explicit false: %v", err)
	}
	if grant.Spec.Disabled {
		t.Fatalf("explicit disabled=false did not update grant state: %#v", grant.Spec)
	}
}

func TestRuntimeSessionApplyPreservesOmittedRevoked(t *testing.T) {
	ctx := context.Background()
	accessMgr := newTestAccessManager(t)
	server := &RuntimeServer{accessMgr: accessMgr}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/sessions", bytes.NewReader([]byte(`{
		"name": "session-new",
		"namespace": "mcp-servers",
		"serverRef": {"name": "demo"},
		"subject": {"humanID": "user-1"},
		"consentedTrust": "low"
	}`)))
	server.handleRuntimeSessionApply(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("new session status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	if _, err := accessMgr.ApplySession(ctx, &sentinelaccess.MCPAgentSession{
		ObjectMeta: metav1.ObjectMeta{Name: "session-a", Namespace: "mcp-servers"},
		Spec: sentinelaccess.MCPAgentSessionSpec{
			ServerRef:      sentinelaccess.ServerReference{Name: "demo"},
			Subject:        sentinelaccess.SubjectRef{HumanID: "user-1"},
			ConsentedTrust: sentinelaccess.TrustLevel("low"),
			Revoked:        true,
		},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/runtime/sessions", bytes.NewReader([]byte(`{
		"name": "session-a",
		"namespace": "mcp-servers",
		"serverRef": {"name": "demo"},
		"subject": {"humanID": "user-1"},
		"consentedTrust": "medium"
	}`)))
	server.handleRuntimeSessionApply(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	session, err := accessMgr.GetSession(ctx, "session-a", "mcp-servers")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if !session.Spec.Revoked {
		t.Fatalf("omitted revoked reset session state: %#v", session.Spec)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/runtime/sessions", bytes.NewReader([]byte(`{
		"name": "session-a",
		"namespace": "mcp-servers",
		"serverRef": {"name": "demo"},
		"subject": {"humanID": "user-1"},
		"consentedTrust": "medium",
		"revoked": false
	}`)))
	server.handleRuntimeSessionApply(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	session, err = accessMgr.GetSession(ctx, "session-a", "mcp-servers")
	if err != nil {
		t.Fatalf("get session after explicit false: %v", err)
	}
	if session.Spec.Revoked {
		t.Fatalf("explicit revoked=false did not update session state: %#v", session.Spec)
	}
}

func TestAccessApplyRequestPointersDecodeOmittedState(t *testing.T) {
	var grant accessGrantRequest
	if err := json.Unmarshal([]byte(`{"disabled":false}`), &grant); err != nil {
		t.Fatalf("unmarshal grant: %v", err)
	}
	if grant.Disabled == nil || *grant.Disabled {
		t.Fatalf("disabled pointer = %#v, want explicit false", grant.Disabled)
	}

	var session accessSessionRequest
	if err := json.Unmarshal([]byte(`{}`), &session); err != nil {
		t.Fatalf("unmarshal session: %v", err)
	}
	if session.Revoked != nil {
		t.Fatalf("revoked pointer = %#v, want nil for omitted field", session.Revoked)
	}
}

func TestValidateGrantRequestRejectsInvalidName(t *testing.T) {
	cases := map[string]*accessGrantRequest{
		"underscore in name": {
			Name:      "grant_a",
			ServerRef: sentinelaccess.ServerReference{Name: "demo"},
			Subject:   sentinelaccess.SubjectRef{HumanID: "user-1"},
		},
		"uppercase serverRef.name": {
			Name:      "grant-a",
			ServerRef: sentinelaccess.ServerReference{Name: "Demo"},
			Subject:   sentinelaccess.SubjectRef{HumanID: "user-1"},
		},
		"invalid serverRef.namespace": {
			Name:      "grant-a",
			ServerRef: sentinelaccess.ServerReference{Name: "demo", Namespace: "Bad_NS"},
			Subject:   sentinelaccess.SubjectRef{HumanID: "user-1"},
		},
	}
	for label, req := range cases {
		t.Run(label, func(t *testing.T) {
			if err := validateGrantRequest(req); err == nil {
				t.Fatalf("expected validation error for %q", label)
			}
		})
	}
}

func TestValidateSessionRequestRejectsInvalidName(t *testing.T) {
	req := &accessSessionRequest{
		Name:           "Session-A",
		ServerRef:      sentinelaccess.ServerReference{Name: "demo"},
		Subject:        sentinelaccess.SubjectRef{HumanID: "user-1"},
		ConsentedTrust: sentinelaccess.TrustLevel("low"),
	}
	if err := validateSessionRequest(req); err == nil {
		t.Fatal("expected validation error for uppercase session name")
	}
}

func TestRuntimeGrantApplyRejectsOversizedBody(t *testing.T) {
	server := &RuntimeServer{accessMgr: sentinelaccess.NewManager(dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()), nil)}
	body := oversizedJSON(accessApplyMaxBytes + 1)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/grants", bytes.NewReader(body))
	server.handleRuntimeGrantApply(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "exceeds") {
		t.Fatalf("body should mention size limit, got %q", recorder.Body.String())
	}
}

func TestRuntimeSessionApplyRejectsOversizedBody(t *testing.T) {
	server := &RuntimeServer{accessMgr: sentinelaccess.NewManager(dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()), nil)}
	body := oversizedJSON(accessApplyMaxBytes + 1)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/sessions", bytes.NewReader(body))
	server.handleRuntimeSessionApply(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", recorder.Code, recorder.Body.String())
	}
}

// oversizedJSON returns a syntactically-valid JSON object whose serialized size
// exceeds approxBytes, so http.MaxBytesReader trips before json decoding fails
// on a structural error.
func oversizedJSON(approxBytes int) []byte {
	pad := strings.Repeat("x", approxBytes)
	return []byte(`{"name":"grant-a","note":"` + pad + `"}`)
}
