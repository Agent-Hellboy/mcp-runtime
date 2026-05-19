package runtimeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
		AllowedSideEffects: []sentinelaccess.ToolSideEffect{
			sentinelaccess.ToolSideEffect(" read "),
			sentinelaccess.ToolSideEffect("write"),
		},
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
	if len(req.AllowedSideEffects) != 2 || req.AllowedSideEffects[0] != "read" || req.AllowedSideEffects[1] != "write" {
		t.Fatalf("allowed side effects were not normalized: %#v", req.AllowedSideEffects)
	}
}

func TestValidateGrantRequestRejectsInvalidToolRule(t *testing.T) {
	req := &accessGrantRequest{
		Name:               "grant-a",
		ServerRef:          sentinelaccess.ServerReference{Name: "demo"},
		Subject:            sentinelaccess.SubjectRef{HumanID: "user-1"},
		AllowedSideEffects: []sentinelaccess.ToolSideEffect{"read"},
		ToolRules: []sentinelaccess.ToolRule{
			{Name: "aaa-ping", Decision: sentinelaccess.PolicyDecision("audit")},
		},
	}

	err := validateGrantRequest(req)
	if err == nil || !strings.Contains(err.Error(), "decision must be allow or deny") {
		t.Fatalf("validateGrantRequest error = %v, want invalid decision", err)
	}
}

func TestValidateGrantRequestRequiresAllowedSideEffect(t *testing.T) {
	req := &accessGrantRequest{
		Name:      "grant-a",
		ServerRef: sentinelaccess.ServerReference{Name: "demo"},
		Subject:   sentinelaccess.SubjectRef{HumanID: "user-1"},
	}

	err := validateGrantRequest(req)
	if err == nil || !strings.Contains(err.Error(), "at least one allowed side effect is required") {
		t.Fatalf("validateGrantRequest error = %v, want allowed side effect requirement", err)
	}
}

func TestValidateGrantRequestRejectsInvalidAllowedSideEffect(t *testing.T) {
	req := &accessGrantRequest{
		Name:               "grant-a",
		ServerRef:          sentinelaccess.ServerReference{Name: "demo"},
		Subject:            sentinelaccess.SubjectRef{HumanID: "user-1"},
		AllowedSideEffects: []sentinelaccess.ToolSideEffect{"read", "delete"},
	}

	err := validateGrantRequest(req)
	if err == nil || !strings.Contains(err.Error(), "allowedSideEffects[1] must be read, write, or destructive") {
		t.Fatalf("validateGrantRequest error = %v, want invalid side effect", err)
	}
}

func TestValidateSessionRequestRequiresSubject(t *testing.T) {
	req := &accessSessionRequest{
		Name:           "session-a",
		ServerRef:      sentinelaccess.ServerReference{Name: "demo"},
		ConsentedTrust: sentinelaccess.TrustLevel("low"),
	}

	err := validateSessionRequest(req)
	if err == nil || !strings.Contains(err.Error(), "one of subject.humanID, subject.agentID, or subject.teamID is required") {
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
		"allowedSideEffects": ["read"],
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
			Description:      "Demo server for basic arithmetic and text tools.",
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
	request := httptest.NewRequest(http.MethodGet, "/api/runtime/servers?namespace=mcp-servers", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{Role: roleAdmin, Subject: "admin-1"}))
	server.HandleRuntimeServers(recorder, request)
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
	if got.Description != "Demo server for basic arithmetic and text tools." {
		t.Fatalf("description = %q", got.Description)
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

func TestPublicMCPEndpointHonorsPlatformDomain(t *testing.T) {
	t.Setenv("MCP_MCP_INGRESS_HOST", "")
	t.Setenv("MCP_PLATFORM_DOMAIN", "example.com")

	mcpServer := mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-one", Namespace: "mcp-servers"},
	}
	endpoint := publicMCPEndpoint(mcpServer)
	if endpoint != "https://mcp.example.com/demo-one/mcp" {
		t.Fatalf("endpoint = %q, want platform domain MCP URL", endpoint)
	}
}

func TestRuntimeServerAccessJSONUsesForwardedLocalOrigin(t *testing.T) {
	t.Setenv("MCP_MCP_INGRESS_HOST", "")
	t.Setenv("MCP_PLATFORM_DOMAIN", "")

	mcpServer := mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-one", Namespace: "mcp-servers"},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/runtime/servers", nil)
	request.Header.Set("X-Forwarded-Host", "localhost:18080")
	request.Header.Set("X-Forwarded-Proto", "http")

	got := serverInfoFromMCPServer(mcpServer, serverDeploymentStatus{}, request)
	if got.Endpoint != "/demo-one/mcp" {
		t.Fatalf("endpoint = %q, want local path", got.Endpoint)
	}
	if url := accessJSONServerURL(t, got, "demo-one"); url != "http://localhost:18080/demo-one/mcp" {
		t.Fatalf("access_json url = %q, want local origin URL", url)
	}
}

func TestRuntimeServerAccessJSONMapsForwardedPlatformOrigin(t *testing.T) {
	t.Setenv("MCP_MCP_INGRESS_HOST", "")
	t.Setenv("MCP_PLATFORM_DOMAIN", "")

	mcpServer := mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-one", Namespace: "mcp-servers"},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/runtime/servers", nil)
	request.Header.Set("X-Forwarded-Host", "platform.mcpruntime.org")
	request.Header.Set("X-Forwarded-Proto", "https")

	got := serverInfoFromMCPServer(mcpServer, serverDeploymentStatus{}, request)
	if url := accessJSONServerURL(t, got, "demo-one"); url != "https://mcp.mcpruntime.org/demo-one/mcp" {
		t.Fatalf("access_json url = %q, want production MCP URL", url)
	}
}

func accessJSONServerURL(t *testing.T, info serverInfo, name string) string {
	t.Helper()
	rawServers, ok := info.AccessJSON["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("access_json.mcpServers = %#v", info.AccessJSON["mcpServers"])
	}
	rawServer, ok := rawServers[name].(map[string]any)
	if !ok {
		t.Fatalf("access_json.mcpServers.%s = %#v", name, rawServers[name])
	}
	url, _ := rawServer["url"].(string)
	return url
}

func TestRuntimeServersAdminDefaultsToSharedCatalogInTenantMode(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	shared := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "shared-server",
			Namespace: sharedCatalogNamespace,
		},
		Spec: mcpv1alpha1.MCPServerSpec{Image: "demo:latest"},
	}
	org := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "org-server",
			Namespace: defaultOrgCatalogNamespace,
		},
		Spec: mcpv1alpha1.MCPServerSpec{Image: "demo:latest"},
	}
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic:   dynamicfake.NewSimpleDynamicClient(scheme, shared, org),
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
	}

	request := httptest.NewRequest(http.MethodGet, "/api/runtime/servers", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{Role: roleAdmin, Subject: "admin-1"}))
	recorder := httptest.NewRecorder()
	server.HandleRuntimeServers(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Servers []serverInfo `json:"servers"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Servers) != 1 || payload.Servers[0].Namespace != sharedCatalogNamespace || payload.Servers[0].Name != "shared-server" {
		t.Fatalf("servers = %#v, want only %s/shared-server", payload.Servers, sharedCatalogNamespace)
	}
}

func TestRuntimeServersNonAdminDefaultsToAccessibleCatalog(t *testing.T) {
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
	team := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "team-server",
			Namespace: "mcp-team-acme",
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Image:  "demo:latest",
			TeamID: "team-acme-id",
		},
	}
	otherTeam := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-team-server",
			Namespace: "mcp-team-other",
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Image:  "demo:latest",
			TeamID: "team-other-id",
		},
	}
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic:   dynamicfake.NewSimpleDynamicClient(scheme, shared, private, team, otherTeam),
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
	}

	request := httptest.NewRequest(http.MethodGet, "/api/runtime/servers", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "user-1",
		AllowedNamespaces: []string{
			"user-1",
			sharedCatalogNamespace,
			"mcp-team-acme",
		},
		Teams: []principalTeam{{
			ID:        "team-acme-id",
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      teamRoleMember,
		}},
	}))
	recorder := httptest.NewRecorder()
	server.HandleRuntimeServers(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Servers []serverInfo `json:"servers"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	got := make([]string, 0, len(payload.Servers))
	for _, server := range payload.Servers {
		got = append(got, server.Namespace+"/"+server.Name)
	}
	want := []string{"mcp-servers/shared-server", "mcp-team-acme/team-server", "user-1/private-server"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("servers = %#v, want %v", got, want)
	}
}

func TestRuntimeServersAnonymousRequestRejected(t *testing.T) {
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic:   dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()),
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/runtime/servers", nil)
	recorder := httptest.NewRecorder()

	server.HandleRuntimeServers(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
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
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "user-1",
		AllowedNamespaces: []string{
			"user-1",
			sharedCatalogNamespace,
		},
	}))
	recorder := httptest.NewRecorder()
	server.HandleRuntimeServers(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestRuntimeServersObservabilityLinksOnlyForObservableServers(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	private := ownedTestMCPServer("private-demo", "user-1", "user-1")
	shared := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "shared-demo",
			Namespace: sharedCatalogNamespace,
		},
		Spec: mcpv1alpha1.MCPServerSpec{Image: "registry.example.com/shared-demo"},
	}
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic:   dynamicfake.NewSimpleDynamicClient(scheme, private, shared),
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/runtime/servers", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "user-1",
		AllowedNamespaces: []string{
			"user-1",
			sharedCatalogNamespace,
		},
	}))
	recorder := httptest.NewRecorder()

	server.HandleRuntimeServers(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Servers []serverInfo `json:"servers"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Servers) != 2 {
		t.Fatalf("servers = %#v, want private and shared", payload.Servers)
	}
	for _, got := range payload.Servers {
		switch got.Name {
		case "private-demo":
			if got.Observability == nil || len(got.Observability.Prometheus.Queries) == 0 {
				t.Fatalf("private server observability missing: %#v", got.Observability)
			}
		case "shared-demo":
			if got.Observability != nil {
				t.Fatalf("shared server should not expose user observability links: %#v", got.Observability)
			}
		}
	}
}

func TestRuntimeObservabilityLinksAllowOwnedNamespace(t *testing.T) {
	server := newRuntimeServerWithMCPServers(t, ownedTestMCPServer("demo", "user-1", "user-1"))
	request := httptest.NewRequest(http.MethodGet, "/api/runtime/observability/links?namespace=user-1&server=demo", nil)
	request.Header.Set("X-Forwarded-Host", "platform.example.test")
	request.Header.Set("X-Forwarded-Proto", "https")
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "user-1",
		AllowedNamespaces: []string{
			"user-1",
			sharedCatalogNamespace,
		},
	}))
	recorder := httptest.NewRecorder()

	server.HandleRuntimeObservabilityLinks(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload observabilityLinksResponse
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Namespace != "user-1" || payload.Server != "demo" {
		t.Fatalf("target = %s/%s, want user-1/demo", payload.Namespace, payload.Server)
	}
	if len(payload.Prometheus.Queries) == 0 {
		t.Fatalf("prometheus queries missing: %#v", payload.Prometheus)
	}
	if got := payload.Prometheus.Queries[0].URL; !strings.HasPrefix(got, "https://platform.example.test/api/runtime/observability/prometheus/query?") {
		t.Fatalf("prometheus URL = %q", got)
	}
	if !payload.Grafana.Available {
		t.Fatalf("grafana should be available through the default scoped dashboard: %#v", payload.Grafana)
	}
	if payload.Grafana.DirectAdminOnly {
		t.Fatalf("default scoped grafana dashboard should not be admin-only: %#v", payload.Grafana)
	}
	if got := payload.Grafana.URL; !strings.HasPrefix(got, "https://platform.example.test/api/runtime/observability/grafana/dashboard?") {
		t.Fatalf("grafana URL = %q", got)
	}
}

func TestRuntimeObservabilityLinksGenerateGrafanaURLAfterAuthorization(t *testing.T) {
	t.Setenv(envGrafanaServerDashboardURL, "/grafana/d/server/mcp-server?var-namespace={namespace}&var-server={server}")
	t.Setenv(envGrafanaScopedUserAccess, "true")
	server := newRuntimeServerWithMCPServers(t, &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "team-demo",
			Namespace: "mcp-team-acme",
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Image:  "registry.example.com/acme/team-demo",
			TeamID: "team-acme-id",
		},
	})
	request := httptest.NewRequest(http.MethodGet, "/api/runtime/observability/links?namespace=mcp-team-acme&server=team-demo", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "user-1",
		AllowedNamespaces: []string{
			"user-1",
			"mcp-team-acme",
		},
		Teams: []principalTeam{{
			ID:        "team-acme-id",
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      teamRoleMember,
		}},
	}))
	recorder := httptest.NewRecorder()

	server.HandleRuntimeObservabilityLinks(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload observabilityLinksResponse
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Grafana.Available {
		t.Fatalf("grafana should be available with scoped user access enabled: %#v", payload.Grafana)
	}
	wantURL := "/grafana/d/server/mcp-server?var-namespace=mcp-team-acme&var-server=team-demo"
	if payload.Grafana.URL != wantURL {
		t.Fatalf("grafana URL = %q, want %q", payload.Grafana.URL, wantURL)
	}
}

func TestRuntimeObservabilityRejectsCrossTenantServer(t *testing.T) {
	server := newRuntimeServerWithMCPServers(t, ownedTestMCPServer("demo", "tenant-b", "tenant-b-user"))
	request := httptest.NewRequest(http.MethodGet, "/api/runtime/observability/links?namespace=tenant-b&server=demo", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "tenant-a-user",
		Namespace: "tenant-a",
		AllowedNamespaces: []string{
			"tenant-a",
			sharedCatalogNamespace,
		},
	}))
	recorder := httptest.NewRecorder()

	server.HandleRuntimeObservabilityLinks(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestRuntimeObservabilityRejectsUnownedSharedCatalogServer(t *testing.T) {
	server := newRuntimeServerWithMCPServers(t, &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "shared-demo",
			Namespace: sharedCatalogNamespace,
		},
		Spec: mcpv1alpha1.MCPServerSpec{Image: "registry.example.com/shared-demo"},
	})
	request := httptest.NewRequest(http.MethodGet, "/api/runtime/observability/links?namespace=mcp-servers&server=shared-demo", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "user-1",
		AllowedNamespaces: []string{
			"user-1",
			sharedCatalogNamespace,
		},
	}))
	recorder := httptest.NewRecorder()

	server.HandleRuntimeObservabilityLinks(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestRuntimeObservabilityPrometheusProxyScopesQuery(t *testing.T) {
	var gotPath, gotQuery string
	prometheus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("query")
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer prometheus.Close()
	t.Setenv(envMCPPrometheusAPIURL, prometheus.URL+"/prometheus")

	server := newRuntimeServerWithMCPServers(t, ownedTestMCPServer("demo", "user-1", "user-1"))
	request := httptest.NewRequest(http.MethodGet, "/api/runtime/observability/prometheus/query?namespace=user-1&server=demo&query_id=request_rate", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "user-1",
		AllowedNamespaces: []string{
			"user-1",
			sharedCatalogNamespace,
		},
	}))
	recorder := httptest.NewRecorder()

	server.HandleRuntimeObservabilityPrometheusQuery(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if gotPath != "/prometheus/api/v1/query" {
		t.Fatalf("prometheus path = %q, want /prometheus/api/v1/query", gotPath)
	}
	wantQuery := `sum(rate(mcp_gateway_requests_total{namespace="user-1",server="demo"}[5m]))`
	if gotQuery != wantQuery {
		t.Fatalf("prometheus query = %q, want %q", gotQuery, wantQuery)
	}
	if strings.Contains(recorder.Body.String(), "tenant-b") {
		t.Fatalf("response leaked foreign tenant marker: %s", recorder.Body.String())
	}
}

func TestRuntimeObservabilityGrafanaDashboardScopesQueries(t *testing.T) {
	var gotQueries []string
	prometheus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQueries = append(gotQueries, r.URL.Query().Get("query"))
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer prometheus.Close()
	t.Setenv(envMCPPrometheusAPIURL, prometheus.URL+"/prometheus")

	server := newRuntimeServerWithMCPServers(t, ownedTestMCPServer("demo", "user-1", "user-1"))
	request := httptest.NewRequest(http.MethodGet, "/api/runtime/observability/grafana/dashboard?namespace=user-1&server=demo", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "user-1",
		AllowedNamespaces: []string{
			"user-1",
			sharedCatalogNamespace,
		},
	}))
	recorder := httptest.NewRecorder()

	server.HandleRuntimeObservabilityGrafanaDashboard(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("content-type"); !strings.Contains(got, "text/html") {
		t.Fatalf("content-type = %q, want text/html", got)
	}
	body := recorder.Body.String()
	for _, want := range []string{"Scoped Grafana", "user-1", "demo", "mcp_gateway_requests_total"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "tenant-b") {
		t.Fatalf("dashboard leaked foreign tenant marker: %s", body)
	}
	if len(gotQueries) == 0 {
		t.Fatal("expected scoped prometheus queries")
	}
	for _, query := range gotQueries {
		if !strings.Contains(query, `namespace="user-1",server="demo"`) {
			t.Fatalf("query = %q, want user/server scoped selector", query)
		}
	}
}

func newRuntimeServerWithMCPServers(t *testing.T, servers ...*mcpv1alpha1.MCPServer) *RuntimeServer {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	objects := make([]runtime.Object, 0, len(servers))
	for _, server := range servers {
		objects = append(objects, server)
	}
	return &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic:   dynamicfake.NewSimpleDynamicClient(scheme, objects...),
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
	}
}

func TestRuntimeServerApplyNonAdminRejectsSharedCatalogNamespace(t *testing.T) {
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic:   dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()),
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/servers", bytes.NewReader([]byte(`{
		"name": "demo",
		"namespace": "mcp-servers",
		"spec": {"image":"registry.example.com/core/demo"}
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "mcp-team-core",
		AllowedNamespaces: []string{
			"mcp-team-core",
			sharedCatalogNamespace,
		},
	}))
	recorder := httptest.NewRecorder()
	server.HandleRuntimeServers(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestRuntimeServerApplyPublicModeDefaultsPublicNamespace(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "public")
	t.Setenv("PLATFORM_TEAM_TRAEFIK_WATCH", "disabled")
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic:   dynamicfake.NewSimpleDynamicClient(scheme),
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/servers", bytes.NewReader([]byte(`{
		"name": "demo",
		"spec": {"image":"registry.example.com/demo"}
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "user-1",
		AllowedNamespaces: []string{
			"user-1",
		},
	}))
	recorder := httptest.NewRecorder()
	server.HandleRuntimeServers(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Server serverInfo `json:"server"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Server.Namespace != defaultPublicCatalogNamespace {
		t.Fatalf("namespace = %q, want %q", payload.Server.Namespace, defaultPublicCatalogNamespace)
	}
	if _, err := server.k8sClients.Clientset.CoreV1().Namespaces().Get(request.Context(), defaultPublicCatalogNamespace, metav1.GetOptions{}); err != nil {
		t.Fatalf("expected public catalog namespace to be created: %v", err)
	}
}

func TestRuntimeServerApplyDefaultsTeamIDFromPrincipalNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic:   dynamicfake.NewSimpleDynamicClient(scheme),
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/servers", bytes.NewReader([]byte(`{
		"name": "demo",
		"namespace": "mcp-team-acme",
		"spec": {"image":"registry.example.com/acme/demo"}
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "mcp-team-acme",
		AllowedNamespaces: []string{
			"mcp-team-acme",
		},
		Teams: []principalTeam{{
			ID:        "team-acme-id",
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      teamRoleOwner,
		}},
	}))
	recorder := httptest.NewRecorder()
	server.HandleRuntimeServers(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Server serverInfo `json:"server"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Server.TeamID != "team-acme-id" {
		t.Fatalf("teamID = %q, want team-acme-id", payload.Server.TeamID)
	}
}

func TestRuntimeServerApplyRejectsMismatchedTeamID(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic:   dynamicfake.NewSimpleDynamicClient(scheme),
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/servers", bytes.NewReader([]byte(`{
		"name": "demo",
		"namespace": "mcp-team-acme",
		"spec": {"teamID":"team-other","image":"registry.example.com/acme/demo"}
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "mcp-team-acme",
		AllowedNamespaces: []string{
			"mcp-team-acme",
		},
		Teams: []principalTeam{{
			ID:        "team-acme-id",
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      teamRoleOwner,
		}},
	}))
	recorder := httptest.NewRecorder()
	server.HandleRuntimeServers(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestRuntimeServerApplyAdminPreservesExistingOwnerLabels(t *testing.T) {
	t.Setenv(envPushCooldown, "0s")
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	existing := ownedTestMCPServer("demo", "user-1", "user-1")
	existing.Labels[createdByLabel] = "user-1"
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic:   dynamicfake.NewSimpleDynamicClient(scheme, existing),
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/servers", bytes.NewReader([]byte(`{
		"name": "demo",
		"namespace": "user-1",
		"spec": {"image":"registry.example.com/user-1/demo"}
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:    roleAdmin,
		Subject: "admin-1",
	}))
	recorder := httptest.NewRecorder()

	server.HandleRuntimeServers(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	current, err := server.controlPlane().GetServer(context.Background(), "user-1", "demo")
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if current.Labels[platformUserIDLabel] != "user-1" {
		t.Fatalf("owner label = %q, want user-1", current.Labels[platformUserIDLabel])
	}
	if current.Labels[createdByLabel] != "user-1" {
		t.Fatalf("created-by label = %q, want user-1", current.Labels[createdByLabel])
	}
}

func TestRuntimeServerApplyAllowsLegacyUnlabeledPublicCatalogServer(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "public")
	t.Setenv("PLATFORM_TEAM_TRAEFIK_WATCH", "disabled")
	t.Setenv(envActiveServerLimit, "0")
	t.Setenv(envPushCooldown, "0s")
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	existing := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: defaultPublicCatalogNamespace,
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Image: "registry.example.com/" + defaultPublicCatalogNamespace + "/demo",
		},
	}
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic:   dynamicfake.NewSimpleDynamicClient(scheme, existing),
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/servers", bytes.NewReader([]byte(`{
		"name": "demo",
		"namespace": "mcp-servers-public",
		"spec": {"image":"registry.example.com/mcp-servers-public/demo"}
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "user-1",
		AllowedNamespaces: []string{
			"user-1",
		},
	}))
	recorder := httptest.NewRecorder()

	server.HandleRuntimeServers(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	current, err := server.controlPlane().GetServer(context.Background(), defaultPublicCatalogNamespace, "demo")
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if current.Labels[platformUserIDLabel] != "user-1" {
		t.Fatalf("owner label = %q, want user-1", current.Labels[platformUserIDLabel])
	}
}

func TestRuntimeServerApplyRejectsActiveServerLimit(t *testing.T) {
	t.Setenv(envActiveServerLimit, "2")
	t.Setenv(envPushCooldown, "0s")
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic: dynamicfake.NewSimpleDynamicClient(scheme,
				ownedTestMCPServer("one", "user-1", "user-1"),
				ownedTestMCPServer("two", "user-1", "user-1"),
			),
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/servers", bytes.NewReader([]byte(`{
		"name": "three",
		"namespace": "user-1",
		"spec": {"image":"registry.example.com/user-1/three"}
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "user-1",
		AllowedNamespaces: []string{
			"user-1",
		},
	}))
	recorder := httptest.NewRecorder()

	server.HandleRuntimeServers(recorder, request)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "retire an existing server") {
		t.Fatalf("body = %q, want retire guidance", recorder.Body.String())
	}
}

func TestRuntimeServerApplyAllowsDisabledActiveServerLimit(t *testing.T) {
	t.Setenv(envActiveServerLimit, "0")
	t.Setenv(envPushCooldown, "0s")
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic: dynamicfake.NewSimpleDynamicClient(scheme,
				ownedTestMCPServer("one", "user-1", "user-1"),
				ownedTestMCPServer("two", "user-1", "user-1"),
			),
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/servers", bytes.NewReader([]byte(`{
		"name": "three",
		"namespace": "user-1",
		"spec": {"image":"registry.example.com/user-1/three"}
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "user-1",
		AllowedNamespaces: []string{
			"user-1",
		},
	}))
	recorder := httptest.NewRecorder()

	server.HandleRuntimeServers(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestRuntimeServerApplyRejectsPushInsideCooldown(t *testing.T) {
	t.Setenv(envActiveServerLimit, "0")
	t.Setenv(envPushCooldown, "1h")
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	existing := ownedTestMCPServer("demo", "user-1", "user-1")
	existing.SetAnnotations(map[string]string{
		platformLastPushAtAnnotation: time.Now().UTC().Format(time.RFC3339),
	})
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic:   dynamicfake.NewSimpleDynamicClient(scheme, existing),
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/servers", bytes.NewReader([]byte(`{
		"name": "demo",
		"namespace": "user-1",
		"spec": {"image":"registry.example.com/user-1/demo"}
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "user-1",
		AllowedNamespaces: []string{
			"user-1",
		},
	}))
	recorder := httptest.NewRecorder()

	server.HandleRuntimeServers(recorder, request)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "next allowed push at") {
		t.Fatalf("body = %q, want next allowed time", recorder.Body.String())
	}
	if recorder.Header().Get("retry-after") == "" {
		t.Fatal("retry-after header missing")
	}
}

func TestRuntimeServerRetireDeletesOwnedServer(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic:   dynamicfake.NewSimpleDynamicClient(scheme, ownedTestMCPServer("demo", "user-1", "user-1")),
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
	}
	request := httptest.NewRequest(http.MethodDelete, "/api/runtime/servers/user-1/demo", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "user-1",
		AllowedNamespaces: []string{
			"user-1",
		},
	}))
	recorder := httptest.NewRecorder()

	server.HandleRuntimeServerItem(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if _, err := server.controlPlane().GetServer(context.Background(), "user-1", "demo"); err == nil {
		t.Fatal("server still exists after retire")
	}
}

func ownedTestMCPServer(name, namespace, userID string) *mcpv1alpha1.MCPServer {
	return &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				platformUserIDLabel: userID,
			},
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Image: "registry.example.com/" + namespace + "/" + name,
		},
	}
}

func TestScopedNamespaceForPrincipal(t *testing.T) {
	server := &RuntimeServer{}
	userCtx := withPrincipal(context.Background(), principal{
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
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	accessMgr := sentinelaccess.NewManager(dynamicfake.NewSimpleDynamicClient(scheme, &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: "user-1",
		},
	}), nil)
	server := &RuntimeServer{accessMgr: accessMgr}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/grants", bytes.NewReader([]byte(`{
		"name": "grant-user",
		"serverRef": {"name": "demo"},
		"subject": {"humanID": "user-1"},
		"allowedSideEffects": ["read"],
		"maxTrust": "low"
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "user-1",
		AllowedNamespaces: []string{
			"user-1",
			sharedCatalogNamespace,
		},
	}))
	server.handleRuntimeGrantApply(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if _, err := accessMgr.GetGrant(ctx, "grant-user", "user-1"); err != nil {
		t.Fatalf("expected grant in user namespace: %v", err)
	}
}

func TestRuntimeGrantApplyDefaultsSubjectTeamID(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	accessMgr := sentinelaccess.NewManager(dynamicfake.NewSimpleDynamicClient(scheme, &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: "mcp-team-acme",
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			TeamID: "team-acme-id",
		},
	}), nil)
	server := &RuntimeServer{accessMgr: accessMgr}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/grants", bytes.NewReader([]byte(`{
		"name": "grant-team",
		"namespace": "mcp-team-acme",
		"serverRef": {"name": "demo"},
		"subject": {"humanID": "user-1"},
		"allowedSideEffects": ["read"],
		"maxTrust": "low"
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "mcp-team-acme",
		AllowedNamespaces: []string{
			"mcp-team-acme",
		},
		Teams: []principalTeam{{
			ID:        "team-acme-id",
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      teamRoleOwner,
		}},
	}))
	server.handleRuntimeGrantApply(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	grant, err := accessMgr.GetGrant(ctx, "grant-team", "mcp-team-acme")
	if err != nil {
		t.Fatalf("expected grant in team namespace: %v", err)
	}
	if grant.Spec.Subject.TeamID != "team-acme-id" {
		t.Fatalf("subject.teamID = %q, want team-acme-id", grant.Spec.Subject.TeamID)
	}
}

func TestRuntimeGrantApplyAllowsForeignSubjectTeamID(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	accessMgr := sentinelaccess.NewManager(dynamicfake.NewSimpleDynamicClient(scheme, &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: "mcp-team-acme",
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			TeamID: "team-acme-id",
		},
	}), nil)
	server := &RuntimeServer{accessMgr: accessMgr}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/grants", bytes.NewReader([]byte(`{
		"name": "grant-team",
		"namespace": "mcp-team-acme",
		"serverRef": {"name": "demo"},
		"subject": {"humanID": "user-1", "teamID": "team-other"},
		"allowedSideEffects": ["read"],
		"maxTrust": "low"
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "mcp-team-acme",
		AllowedNamespaces: []string{
			"mcp-team-acme",
		},
		Teams: []principalTeam{{
			ID:        "team-acme-id",
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      teamRoleOwner,
		}},
	}))
	server.handleRuntimeGrantApply(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	grant, err := accessMgr.GetGrant(ctx, "grant-team", "mcp-team-acme")
	if err != nil {
		t.Fatalf("expected grant in team namespace: %v", err)
	}
	if grant.Spec.Subject.TeamID != "team-other" {
		t.Fatalf("subject.teamID = %q, want team-other", grant.Spec.Subject.TeamID)
	}
}

func TestRuntimeGrantApplyRejectsCrossNamespaceServerRef(t *testing.T) {
	accessMgr := newTestAccessManager(t)
	server := &RuntimeServer{accessMgr: accessMgr}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/grants", bytes.NewReader([]byte(`{
		"name": "grant-cross",
		"namespace": "mcp-team-acme",
		"serverRef": {"name": "demo", "namespace": "mcp-team-globex"},
		"subject": {"humanID": "user-1"},
		"allowedSideEffects": ["read"],
		"maxTrust": "low"
	}`)))
	server.handleRuntimeGrantApply(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "must match access resource namespace") {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestRuntimeSessionApplyRejectsCrossNamespaceServerRef(t *testing.T) {
	accessMgr := newTestAccessManager(t)
	server := &RuntimeServer{accessMgr: accessMgr}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/sessions", bytes.NewReader([]byte(`{
		"name": "session-cross",
		"namespace": "mcp-team-acme",
		"serverRef": {"name": "demo", "namespace": "mcp-team-globex"},
		"subject": {"humanID": "user-1"},
		"consentedTrust": "low"
	}`)))
	server.handleRuntimeSessionApply(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestRuntimeGrantApplyNonAdminRejectsSharedCatalogNamespace(t *testing.T) {
	accessMgr := newTestAccessManager(t)
	server := &RuntimeServer{accessMgr: accessMgr}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/grants", bytes.NewReader([]byte(`{
		"name": "grant-shared",
		"namespace": "mcp-servers",
		"serverRef": {"name": "demo"},
		"subject": {"humanID": "user-1"},
		"allowedSideEffects": ["read"],
		"maxTrust": "low"
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "mcp-team-core",
		AllowedNamespaces: []string{
			"mcp-team-core",
			sharedCatalogNamespace,
		},
	}))
	server.handleRuntimeGrantApply(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
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
		"allowedSideEffects": ["read"],
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
		"allowedSideEffects": ["read"],
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
		"allowedSideEffects": ["read"],
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
