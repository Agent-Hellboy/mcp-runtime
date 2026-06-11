package runtimeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
	sentinelaccess "mcp-runtime/pkg/access"
	"mcp-runtime/pkg/controlplane"
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
	srv := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: sentinelaccess.DefaultMCPResourceNamespace,
		},
	}
	return newTestAccessManagerWithObjects(t, srv)
}

func newTestAccessManagerWithObjects(t *testing.T, objects ...runtime.Object) *sentinelaccess.Manager {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	return sentinelaccess.NewManager(dynamicfake.NewSimpleDynamicClient(scheme, objects...), nil)
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
	t.Setenv("MCP_PLATFORM_DOMAIN", "")
	t.Setenv("MCP_MCP_INGRESS_HOST", "")
	t.Setenv("MCP_REGISTRY_ENDPOINT", "10.43.69.247:5000")
	t.Setenv("MCP_REGISTRY_INGRESS_HOST", "registry.mcpruntime.org")
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
			Image:            "10.43.69.247:5000/public/demo:latest",
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
	if got.Image != "registry.mcpruntime.org/public/demo:latest" {
		t.Fatalf("image = %q", got.Image)
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

func TestRuntimeToolsCatalogScopesFiltersAndComputesRisk(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	srv := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "payments",
			Namespace:         "mcp-team-acme",
			CreationTimestamp: metav1.Now(),
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			TeamID:           "acme",
			Image:            "payments:latest",
			PublicPathPrefix: "payments",
			Tools: []mcpv1alpha1.ToolConfig{
				{
					Name:          "refund_invoice",
					Description:   "Refund an invoice.",
					RequiredTrust: mcpv1alpha1.TrustLevelHigh,
					SideEffect:    mcpv1alpha1.ToolSideEffectDestructive,
					Labels:        map[string]string{"domain": "billing"},
				},
			},
		},
	}
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic:   dynamicfake.NewSimpleDynamicClient(scheme, srv),
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
		liveInventoryProbe: staticLiveInventoryProber{inventory: &liveInventory{
			Tools: []liveInventoryTool{
				{Name: "refund_invoice", Description: "Refund an invoice."},
				{Name: "lookup_invoice", Description: "Look up invoice status."},
			},
		}},
	}
	waitForCachedInventory(t, server.liveInventory(), controlplane.ServerInfoFromMCPServer(*srv, controlplane.ServerDeploymentStatus{}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/runtime/tools?namespace=mcp-team-acme&risk=high", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{Role: roleAdmin, Subject: "admin-1"}))
	server.HandleRuntimeTools(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Tools []runtimeToolRow `json:"tools"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Tools) != 1 {
		t.Fatalf("tools = %#v, want one high-risk declared tool", payload.Tools)
	}
	got := payload.Tools[0]
	if got.ToolName != "refund_invoice" || got.RiskLevel != "high" || got.DriftStatus != "declared" || !got.Declared || !got.Live {
		t.Fatalf("tool row = %#v", got)
	}
	if got.ConnectConfig == nil {
		t.Fatalf("connect_config missing: %#v", got)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/runtime/tools?namespace=mcp-team-acme&drift=ungoverned", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{Role: roleAdmin, Subject: "admin-1"}))
	server.HandleRuntimeTools(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	payload.Tools = nil
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Tools) != 1 || payload.Tools[0].ToolName != "lookup_invoice" || payload.Tools[0].DriftStatus != "ungoverned" {
		t.Fatalf("ungoverned tools = %#v", payload.Tools)
	}
	if payload.Tools[0].RequiredTrust != "" || payload.Tools[0].RiskLevel != "" {
		t.Fatalf("ungoverned tool risk metadata = %#v, want empty trust and risk", payload.Tools[0])
	}
}

func TestHandleRuntimeToolsReturnsForbiddenForUnreadableNamespace(t *testing.T) {
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

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/runtime/tools?namespace=other-team", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{Role: roleUser, Subject: "user-1"}))
	server.HandleRuntimeTools(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "forbidden namespace") {
		t.Fatalf("body = %s, want forbidden namespace", recorder.Body.String())
	}
}

type staticLiveInventoryProber struct {
	inventory *liveInventory
}

func (p staticLiveInventoryProber) probe(context.Context, controlplane.ServerInfo) (*liveInventory, error) {
	return p.inventory, nil
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

func TestRuntimeServersAdminDefaultsToAllNamespaces(t *testing.T) {
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
	if len(payload.Servers) != 2 {
		t.Fatalf("servers = %#v, want both namespaces", payload.Servers)
	}
	if payload.Servers[0].Namespace != sharedCatalogNamespace || payload.Servers[0].Name != "shared-server" {
		t.Fatalf("first server = %#v, want shared-server sorted by namespace", payload.Servers[0])
	}
	if payload.Servers[1].Namespace != defaultOrgCatalogNamespace || payload.Servers[1].Name != "org-server" {
		t.Fatalf("second server = %#v, want org-server", payload.Servers[1])
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
		"spec": {"image":"registry.example.com/public/demo"}
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

func TestRuntimeServerApplyPublicScopeResolvesCatalogNamespace(t *testing.T) {
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
		"scope": "public",
		"spec": {"image":"registry.example.com/public/demo"}
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
	if current.Labels[platformScopeLabel] != "public" {
		t.Fatalf("scope label = %q, want public", current.Labels[platformScopeLabel])
	}
}

func TestRuntimeServerApplyDefaultsGatewayAndAnalyticsSecret(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "public")
	t.Setenv("PLATFORM_TEAM_TRAEFIK_WATCH", "disabled")
	t.Setenv("MCP_SENTINEL_INGEST_URL", "http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events")
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic: dynamicfake.NewSimpleDynamicClient(scheme),
			Clientset: kubernetesfake.NewSimpleClientset(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: defaultAnalyticsCredentialSourceSecretName, Namespace: "mcp-sentinel"},
				Data: map[string][]byte{
					defaultAnalyticsCredentialSourceKey: []byte("ingest-key-1,ingest-key-2"),
				},
			}),
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/servers", bytes.NewReader([]byte(`{
		"name": "demo",
		"scope": "public",
		"spec": {"image":"registry.example.com/public/demo"}
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "mcp-team-acme",
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
	if current.Spec.Gateway == nil || !current.Spec.Gateway.Enabled {
		t.Fatalf("gateway = %#v, want enabled", current.Spec.Gateway)
	}
	if current.Spec.Analytics == nil || current.Spec.Analytics.APIKeySecretRef == nil {
		t.Fatalf("analytics = %#v, want api key secret ref", current.Spec.Analytics)
	}
	if current.Spec.Analytics.APIKeySecretRef.Name != "demo-analytics-creds" || current.Spec.Analytics.APIKeySecretRef.Key != "api-key" {
		t.Fatalf("analytics secret ref = %#v", current.Spec.Analytics.APIKeySecretRef)
	}
	secret, err := server.k8sClients.Clientset.CoreV1().Secrets(defaultPublicCatalogNamespace).Get(context.Background(), "demo-analytics-creds", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("analytics secret missing: %v", err)
	}
	if got := string(secret.Data["api-key"]); got != "ingest-key-1" {
		t.Fatalf("analytics secret api-key = %q, want ingest-key-1", got)
	}
}

func TestRuntimeServerApplyDefaultsAnalyticsSecretNameFitsDNSLabelLimit(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "public")
	t.Setenv("PLATFORM_TEAM_TRAEFIK_WATCH", "disabled")
	t.Setenv("MCP_SENTINEL_INGEST_URL", "http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events")
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic: dynamicfake.NewSimpleDynamicClient(scheme),
			Clientset: kubernetesfake.NewSimpleClientset(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: defaultAnalyticsCredentialSourceSecretName, Namespace: "mcp-sentinel"},
				Data: map[string][]byte{
					defaultAnalyticsCredentialSourceKey: []byte("ingest-key-1"),
				},
			}),
		},
	}
	longName := strings.Repeat("demo", 16)
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/servers", bytes.NewReader([]byte(`{
		"name": "`+longName+`",
		"scope": "public",
		"spec": {"image":"registry.example.com/public/demo"}
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "mcp-team-acme",
	}))
	recorder := httptest.NewRecorder()

	server.HandleRuntimeServers(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	current, err := server.controlPlane().GetServer(context.Background(), defaultPublicCatalogNamespace, longName)
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if current.Spec.Analytics == nil || current.Spec.Analytics.APIKeySecretRef == nil {
		t.Fatalf("analytics = %#v, want api key secret ref", current.Spec.Analytics)
	}
	got := current.Spec.Analytics.APIKeySecretRef.Name
	if len(got) > 63 {
		t.Fatalf("analytics secret name len = %d, want <= 63 (%q)", len(got), got)
	}
}

func TestRuntimeServerApplyAllowsMissingDefaultAnalyticsSecret(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "public")
	t.Setenv("PLATFORM_TEAM_TRAEFIK_WATCH", "disabled")
	t.Setenv("MCP_SENTINEL_INGEST_URL", "http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events")
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
		"scope": "public",
		"spec": {"image":"registry.example.com/public/demo"}
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "mcp-team-acme",
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
	if current.Spec.Gateway == nil || !current.Spec.Gateway.Enabled {
		t.Fatalf("gateway = %#v, want enabled", current.Spec.Gateway)
	}
	if current.Spec.Analytics != nil && current.Spec.Analytics.APIKeySecretRef != nil {
		t.Fatalf("analytics api key ref = %#v, want nil when source secret is missing", current.Spec.Analytics.APIKeySecretRef)
	}
}

func TestApplyPublishedServerDefaultsSkipsAnalyticsSecretWhenRBACRestricted(t *testing.T) {
	t.Setenv("MCP_SENTINEL_INGEST_URL", "http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events")
	client := kubernetesfake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: defaultAnalyticsCredentialSourceSecretName, Namespace: "mcp-sentinel"},
		Data: map[string][]byte{
			defaultAnalyticsCredentialSourceKey: []byte("ingest-key-1"),
		},
	})
	client.Fake.PrependReactor("create", "secrets", func(action ktesting.Action) (bool, runtime.Object, error) {
		create := action.(ktesting.CreateAction)
		if create.GetNamespace() == defaultPublicCatalogNamespace {
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "demo-analytics-creds", errors.New("blocked"))
		}
		return false, nil, nil
	})
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{Clientset: client},
	}
	spec := &mcpv1alpha1.MCPServerSpec{Gateway: &mcpv1alpha1.GatewayConfig{Enabled: true}}

	if err := server.applyPublishedServerDefaults(context.Background(), defaultPublicCatalogNamespace, "demo", spec); err != nil {
		t.Fatalf("applyPublishedServerDefaults() error = %v", err)
	}
	if spec.Analytics != nil && spec.Analytics.APIKeySecretRef != nil {
		t.Fatalf("analytics api key ref = %#v, want nil when namespace secret writes are forbidden", spec.Analytics.APIKeySecretRef)
	}
}

func TestRuntimeServerApplyPreservesExplicitGatewaySettings(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "public")
	t.Setenv("PLATFORM_TEAM_TRAEFIK_WATCH", "disabled")
	t.Setenv("MCP_SENTINEL_INGEST_URL", "http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events")
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic: dynamicfake.NewSimpleDynamicClient(scheme),
			Clientset: kubernetesfake.NewSimpleClientset(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: defaultAnalyticsCredentialSourceSecretName, Namespace: "mcp-sentinel"},
				Data: map[string][]byte{
					defaultAnalyticsCredentialSourceKey: []byte("ingest-key-1"),
				},
			}),
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/servers", bytes.NewReader([]byte(`{
		"name": "demo",
		"scope": "public",
		"spec": {
			"image":"registry.example.com/public/demo",
			"gateway":{"enabled":false}
		}
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "mcp-team-acme",
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
	if current.Spec.Gateway == nil || current.Spec.Gateway.Enabled {
		t.Fatalf("gateway = %#v, want explicitly disabled", current.Spec.Gateway)
	}
	if current.Spec.Analytics != nil && current.Spec.Analytics.APIKeySecretRef != nil {
		t.Fatalf("analytics api key ref = %#v, want nil when gateway disabled", current.Spec.Analytics.APIKeySecretRef)
	}
}

func TestRuntimeServerApplyPreservesExplicitAnalyticsDisable(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "public")
	t.Setenv("PLATFORM_TEAM_TRAEFIK_WATCH", "disabled")
	t.Setenv("MCP_SENTINEL_INGEST_URL", "http://mcp-sentinel-ingest.mcp-sentinel.svc.cluster.local:8081/events")
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic: dynamicfake.NewSimpleDynamicClient(scheme),
			Clientset: kubernetesfake.NewSimpleClientset(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: defaultAnalyticsCredentialSourceSecretName, Namespace: "mcp-sentinel"},
				Data: map[string][]byte{
					defaultAnalyticsCredentialSourceKey: []byte("ingest-key-1"),
				},
			}),
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/servers", bytes.NewReader([]byte(`{
		"name": "demo",
		"scope": "public",
		"spec": {
			"image":"registry.example.com/public/demo",
			"analytics":{"disabled":true}
		}
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "mcp-team-acme",
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
	if current.Spec.Gateway == nil || !current.Spec.Gateway.Enabled {
		t.Fatalf("gateway = %#v, want enabled", current.Spec.Gateway)
	}
	if current.Spec.Analytics == nil || !current.Spec.Analytics.Disabled {
		t.Fatalf("analytics = %#v, want disabled", current.Spec.Analytics)
	}
	if current.Spec.Analytics.APIKeySecretRef != nil {
		t.Fatalf("analytics api key ref = %#v, want nil when analytics disabled", current.Spec.Analytics.APIKeySecretRef)
	}
}

func TestRuntimeServerApplyPublicScopeExpandsShortImage(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "public")
	t.Setenv("PLATFORM_TEAM_TRAEFIK_WATCH", "disabled")
	t.Setenv("MCP_REGISTRY_ENDPOINT", "10.96.223.152:5000")
	t.Setenv("MCP_REGISTRY_INGRESS_HOST", "registry.mcpruntime.org")
	t.Setenv("UI_API_KEY", "test-registry-pull-key")
	t.Setenv("MCP_MCP_INGRESS_HOST", "mcp.mcpruntime.org")
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
		"name": "go-example",
		"scope": "public",
		"spec": {"image":"go-example"}
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "mcp-team-acme",
	}))
	recorder := httptest.NewRecorder()

	server.HandleRuntimeServers(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	current, err := server.controlPlane().GetServer(context.Background(), defaultPublicCatalogNamespace, "go-example")
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if got, want := current.Spec.Image, "10.96.223.152:5000/public/go-example"; got != want {
		t.Fatalf("image = %q, want %q", got, want)
	}
	if got := envValue(current.Spec.EnvVars, "MCP_PATH"); got != "/go-example/mcp" {
		t.Fatalf("MCP_PATH = %q, want /go-example/mcp", got)
	}
	if got := current.Spec.IngressHost; got != "mcp.mcpruntime.org" {
		t.Fatalf("ingressHost = %q, want mcp.mcpruntime.org", got)
	}
}

func TestRuntimeServerApplyTenantScopeExpandsShortImageToTeamSlug(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "tenant")
	t.Setenv("PLATFORM_TEAM_TRAEFIK_WATCH", "disabled")
	t.Setenv("MCP_REGISTRY_ENDPOINT", "10.96.223.152:5000")
	t.Setenv("MCP_REGISTRY_INGRESS_HOST", "registry.mcpruntime.org")
	t.Setenv("UI_API_KEY", "test-registry-pull-key")
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
		"name": "go-example",
		"scope": "tenant",
		"spec": {"image":"go-example"}
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:    roleUser,
		Subject: "user-1",
		Teams: []principalTeam{
			{ID: "team-acme", Slug: "acme", Name: "Acme", Namespace: "mcp-team-acme", Role: "owner"},
		},
	}))
	recorder := httptest.NewRecorder()

	server.HandleRuntimeServers(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	current, err := server.controlPlane().GetServer(context.Background(), "mcp-team-acme", "go-example")
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if got, want := current.Spec.Image, "10.96.223.152:5000/acme/go-example"; got != want {
		t.Fatalf("image = %q, want %q", got, want)
	}
	if got := current.Spec.TeamID; got != "team-acme" {
		t.Fatalf("teamID = %q, want team-acme", got)
	}
	if got := envValue(current.Spec.EnvVars, "MCP_PATH"); got != "/go-example/mcp" {
		t.Fatalf("MCP_PATH = %q, want /go-example/mcp", got)
	}
}

func TestRuntimeServerApplyRejectsPublicScopeWhenModeDisabled(t *testing.T) {
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
		"scope": "public",
		"spec": {"image":"registry.example.com/public/demo"}
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
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d body = %s, want 403", recorder.Code, recorder.Body.String())
	}
}

func TestRuntimeServerApplyTenantScopeUsesTeamNamespaceInOrgMode(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "org")
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
		"scope": "tenant",
		"spec": {"image":"registry.example.com/acme/demo"}
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-id",
		Namespace: "mcp-team-acme",
		AllowedNamespaces: []string{
			"mcp-team-acme",
		},
		Teams: []principalTeam{
			{ID: "team-acme-id", Slug: "acme", Name: "Acme", Namespace: "mcp-team-acme"},
		},
	}))
	recorder := httptest.NewRecorder()
	server.HandleRuntimeServers(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	current, err := server.controlPlane().GetServer(context.Background(), "mcp-team-acme", "demo")
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if current.Labels[platformScopeLabel] != "tenant" {
		t.Fatalf("scope label = %q, want tenant", current.Labels[platformScopeLabel])
	}
	if current.Spec.TeamID != "team-acme-id" {
		t.Fatalf("teamID = %q, want team-acme-id", current.Spec.TeamID)
	}
	ns, err := server.k8sClients.Clientset.CoreV1().Namespaces().Get(context.Background(), "mcp-team-acme", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected team namespace to be created: %v", err)
	}
	if ns.Labels[platformManagedLabel] != "true" || ns.Labels[platformTeamSlugLabel] != "acme" {
		t.Fatalf("team namespace labels = %#v", ns.Labels)
	}
}

func TestRuntimeServerApplyTenantScopeRejectsUserNamespace(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "tenant")
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
		"scope": "tenant",
		"spec": {"image":"registry.example.com/user-1/demo"}
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-id",
		Namespace: "user-1",
		AllowedNamespaces: []string{
			"user-1",
		},
	}))
	recorder := httptest.NewRecorder()
	server.HandleRuntimeServers(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d body = %s, want 403", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "tenant scope requires team membership") {
		t.Fatalf("body = %s, want team membership error", recorder.Body.String())
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
		"update": true,
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

func TestRuntimeServerApplyRejectsExistingOwnedServerWithoutUpdate(t *testing.T) {
	t.Setenv(envActiveServerLimit, "0")
	t.Setenv(envPushCooldown, "0s")
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

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "use --update") {
		t.Fatalf("body = %q, want --update guidance", recorder.Body.String())
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
		"update": true,
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
		"update": true,
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

func envValue(envVars []mcpv1alpha1.EnvVar, name string) string {
	for _, envVar := range envVars {
		if envVar.Name == name {
			return envVar.Value
		}
	}
	return ""
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

func TestRuntimeGrantApplyRejectsTeamMemberForTeamServer(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	accessMgr := sentinelaccess.NewManager(dynamicfake.NewSimpleDynamicClient(scheme, &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: "mcp-team-acme",
			Labels: map[string]string{
				platformUserIDLabel: "owner-1",
			},
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
		"subject": {"humanID": "user-2"},
		"allowedSideEffects": ["read"],
		"maxTrust": "low"
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "mcp-team-acme",
		Teams: []principalTeam{{
			ID:        "team-acme-id",
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      teamRoleMember,
		}},
	}))
	server.handleRuntimeGrantApply(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestRuntimeGrantApplyAllowsServerOwnerInTeamNamespace(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	accessMgr := sentinelaccess.NewManager(dynamicfake.NewSimpleDynamicClient(scheme, &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: "mcp-team-acme",
			Labels: map[string]string{
				platformUserIDLabel: "user-1",
			},
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
		"subject": {"humanID": "user-2"},
		"allowedSideEffects": ["read"],
		"maxTrust": "low"
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "mcp-team-acme",
		Teams: []principalTeam{{
			ID:        "team-acme-id",
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      teamRoleMember,
		}},
	}))
	server.handleRuntimeGrantApply(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if _, err := accessMgr.GetGrant(ctx, "grant-team", "mcp-team-acme"); err != nil {
		t.Fatalf("expected grant in team namespace: %v", err)
	}
}

func TestRuntimeGrantApplyRejectsPublicCatalogReader(t *testing.T) {
	t.Setenv("PLATFORM_MODE", "public")
	accessMgr := newTestAccessManagerWithObjects(t, &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: defaultPublicCatalogNamespace,
		},
	})
	server := &RuntimeServer{accessMgr: accessMgr}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/grants", bytes.NewReader([]byte(`{
		"name": "grant-public",
		"namespace": "mcp-servers-public",
		"serverRef": {"name": "demo"},
		"subject": {"humanID": "user-1"},
		"allowedSideEffects": ["read"],
		"maxTrust": "low"
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), PublicCatalogPrincipal()))
	server.handleRuntimeGrantApply(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
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

func TestRuntimeSessionApplyRejectsNonAdminDirectApply(t *testing.T) {
	accessMgr := newTestAccessManagerWithObjects(t, &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: "user-1",
			Labels: map[string]string{
				platformUserIDLabel: "user-1",
			},
		},
	})
	server := &RuntimeServer{accessMgr: accessMgr}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/sessions", bytes.NewReader([]byte(`{
		"name": "session-user",
		"namespace": "user-1",
		"serverRef": {"name": "demo"},
		"subject": {"humanID": "user-1"},
		"consentedTrust": "low"
	}`)))
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "user-1",
	}))
	server.handleRuntimeSessionApply(recorder, request)
	if recorder.Code != http.StatusForbidden {
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

func TestRuntimeGrantListScopesTeamMemberAccessResources(t *testing.T) {
	ctx := context.Background()
	accessMgr := newTestAccessManagerWithObjects(t, &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: "mcp-team-acme",
			Labels: map[string]string{
				platformUserIDLabel: "owner-1",
			},
		},
		Spec: mcpv1alpha1.MCPServerSpec{TeamID: "team-acme-id"},
	})
	if _, err := accessMgr.ApplyGrant(ctx, &sentinelaccess.MCPAccessGrant{
		ObjectMeta: metav1.ObjectMeta{Name: "grant-team", Namespace: "mcp-team-acme"},
		Spec: sentinelaccess.MCPAccessGrantSpec{
			ServerRef:          sentinelaccess.ServerReference{Name: "demo"},
			Subject:            sentinelaccess.SubjectRef{HumanID: "other-user", TeamID: "team-acme-id"},
			MaxTrust:           sentinelaccess.TrustLevel("low"),
			AllowedSideEffects: []sentinelaccess.ToolSideEffect{"read"},
		},
	}); err != nil {
		t.Fatalf("seed grant: %v", err)
	}
	server := &RuntimeServer{accessMgr: accessMgr}

	memberReq := httptest.NewRequest(http.MethodGet, "/api/runtime/grants?namespace=mcp-team-acme", nil)
	memberReq = memberReq.WithContext(withPrincipal(memberReq.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "mcp-team-acme",
		Teams: []principalTeam{{
			ID:        "team-acme-id",
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      teamRoleMember,
		}},
	}))
	memberRec := httptest.NewRecorder()
	server.handleRuntimeGrantList(memberRec, memberReq)
	if memberRec.Code != http.StatusOK {
		t.Fatalf("member status = %d body=%s", memberRec.Code, memberRec.Body.String())
	}
	var memberPayload struct {
		Grants []sentinelaccess.GrantSummary `json:"grants"`
	}
	if err := json.NewDecoder(memberRec.Body).Decode(&memberPayload); err != nil {
		t.Fatalf("decode member grants: %v", err)
	}
	if len(memberPayload.Grants) != 0 {
		t.Fatalf("member grants = %#v, want no sensitive grants", memberPayload.Grants)
	}

	ownerReq := httptest.NewRequest(http.MethodGet, "/api/runtime/grants?namespace=mcp-team-acme", nil)
	ownerReq = ownerReq.WithContext(withPrincipal(ownerReq.Context(), principal{
		Role:      roleUser,
		Subject:   "owner-2",
		Namespace: "mcp-team-acme",
		Teams: []principalTeam{{
			ID:        "team-acme-id",
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      teamRoleOwner,
		}},
	}))
	ownerRec := httptest.NewRecorder()
	server.handleRuntimeGrantList(ownerRec, ownerReq)
	if ownerRec.Code != http.StatusOK {
		t.Fatalf("owner status = %d body=%s", ownerRec.Code, ownerRec.Body.String())
	}
	var ownerPayload struct {
		Grants []sentinelaccess.GrantSummary `json:"grants"`
	}
	if err := json.NewDecoder(ownerRec.Body).Decode(&ownerPayload); err != nil {
		t.Fatalf("decode owner grants: %v", err)
	}
	if len(ownerPayload.Grants) != 1 {
		t.Fatalf("owner grants = %#v, want grant visible", ownerPayload.Grants)
	}
}

func TestRuntimeGrantListPrefetchesServersForVisibility(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme, &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: "mcp-team-acme",
		},
		Spec: mcpv1alpha1.MCPServerSpec{TeamID: "team-acme-id"},
	})
	serverGets := 0
	serverLists := 0
	dynamicClient.Fake.PrependReactor("get", "mcpservers", func(ktesting.Action) (bool, runtime.Object, error) {
		serverGets++
		return false, nil, nil
	})
	dynamicClient.Fake.PrependReactor("list", "mcpservers", func(ktesting.Action) (bool, runtime.Object, error) {
		serverLists++
		return false, nil, nil
	})
	accessMgr := sentinelaccess.NewManager(dynamicClient, nil)
	for _, name := range []string{"grant-one", "grant-two"} {
		if _, err := accessMgr.ApplyGrant(ctx, &sentinelaccess.MCPAccessGrant{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "mcp-team-acme"},
			Spec: sentinelaccess.MCPAccessGrantSpec{
				ServerRef:          sentinelaccess.ServerReference{Name: "demo"},
				Subject:            sentinelaccess.SubjectRef{HumanID: "other-user", TeamID: "team-acme-id"},
				MaxTrust:           sentinelaccess.TrustLevel("low"),
				AllowedSideEffects: []sentinelaccess.ToolSideEffect{"read"},
			},
		}); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	server := &RuntimeServer{accessMgr: accessMgr}
	request := httptest.NewRequest(http.MethodGet, "/api/runtime/grants?namespace=mcp-team-acme", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "owner-1",
		Namespace: "mcp-team-acme",
		Teams: []principalTeam{{
			ID:        "team-acme-id",
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      teamRoleOwner,
		}},
	}))
	recorder := httptest.NewRecorder()
	server.handleRuntimeGrantList(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Grants []sentinelaccess.GrantSummary `json:"grants"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode grants: %v", err)
	}
	if len(payload.Grants) != 2 {
		t.Fatalf("grants = %#v, want both grants visible", payload.Grants)
	}
	if serverLists != 1 {
		t.Fatalf("server list calls = %d, want 1", serverLists)
	}
	if serverGets != 0 {
		t.Fatalf("server get calls = %d, want 0", serverGets)
	}
}

func TestRuntimeGrantGetRejectsTeamMemberForSensitiveGrant(t *testing.T) {
	ctx := context.Background()
	accessMgr := newTestAccessManagerWithObjects(t, &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: "mcp-team-acme",
			Labels: map[string]string{
				platformUserIDLabel: "owner-1",
			},
		},
		Spec: mcpv1alpha1.MCPServerSpec{TeamID: "team-acme-id"},
	})
	if _, err := accessMgr.ApplyGrant(ctx, &sentinelaccess.MCPAccessGrant{
		ObjectMeta: metav1.ObjectMeta{Name: "grant-team", Namespace: "mcp-team-acme"},
		Spec: sentinelaccess.MCPAccessGrantSpec{
			ServerRef:          sentinelaccess.ServerReference{Name: "demo"},
			Subject:            sentinelaccess.SubjectRef{HumanID: "other-user", TeamID: "team-acme-id"},
			MaxTrust:           sentinelaccess.TrustLevel("low"),
			AllowedSideEffects: []sentinelaccess.ToolSideEffect{"read"},
		},
	}); err != nil {
		t.Fatalf("seed grant: %v", err)
	}
	server := &RuntimeServer{accessMgr: accessMgr}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/runtime/grants/mcp-team-acme/grant-team", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "mcp-team-acme",
		Teams: []principalTeam{{
			ID:        "team-acme-id",
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      teamRoleMember,
		}},
	}))
	server.handleGrantGet(recorder, request, "mcp-team-acme", "grant-team")
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestRuntimePolicyRequiresServerAdministrator(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	mcpServer := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: "mcp-team-acme",
			Labels: map[string]string{
				platformUserIDLabel: "server-owner",
			},
		},
		Spec: mcpv1alpha1.MCPServerSpec{TeamID: "team-acme-id"},
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-gateway-policy",
			Namespace: "mcp-team-acme",
		},
		Data: map[string]string{
			"policy.json": `{"rules":[{"subject":{"humanID":"other-user"}}]}`,
		},
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme, mcpServer)
	clientset := kubernetesfake.NewSimpleClientset(cm)
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{Dynamic: dynamicClient, Clientset: clientset},
		accessMgr:  sentinelaccess.NewManager(dynamicClient, clientset),
	}

	memberReq := httptest.NewRequest(http.MethodGet, "/api/runtime/policy?namespace=mcp-team-acme&server=demo", nil)
	memberReq = memberReq.WithContext(withPrincipal(memberReq.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "mcp-team-acme",
		Teams: []principalTeam{{
			ID:        "team-acme-id",
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      teamRoleMember,
		}},
	}))
	memberRec := httptest.NewRecorder()
	server.HandleRuntimePolicy(memberRec, memberReq)
	if memberRec.Code != http.StatusForbidden {
		t.Fatalf("member status = %d body=%s", memberRec.Code, memberRec.Body.String())
	}

	ownerReq := httptest.NewRequest(http.MethodGet, "/api/runtime/policy?namespace=mcp-team-acme&server=demo", nil)
	ownerReq = ownerReq.WithContext(withPrincipal(ownerReq.Context(), principal{
		Role:      roleUser,
		Subject:   "owner-2",
		Namespace: "mcp-team-acme",
		Teams: []principalTeam{{
			ID:        "team-acme-id",
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      teamRoleOwner,
		}},
	}))
	ownerRec := httptest.NewRecorder()
	server.HandleRuntimePolicy(ownerRec, ownerReq)
	if ownerRec.Code != http.StatusOK {
		t.Fatalf("owner status = %d body=%s", ownerRec.Code, ownerRec.Body.String())
	}
	if strings.Contains(ownerRec.Body.String(), "other-user") == false {
		t.Fatalf("owner policy body = %s, want rendered policy", ownerRec.Body.String())
	}
}

func TestRuntimeServerEventsRejectsTeamMemberBeforeAnalyticsQuery(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme, &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: "mcp-team-acme",
			Labels: map[string]string{
				platformUserIDLabel: "server-owner",
			},
		},
		Spec: mcpv1alpha1.MCPServerSpec{TeamID: "team-acme-id"},
	})
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{Dynamic: dynamicClient, Clientset: kubernetesfake.NewSimpleClientset()},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/runtime/server-events?namespace=mcp-team-acme&server=demo", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "mcp-team-acme",
		Teams: []principalTeam{{
			ID:        "team-acme-id",
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      teamRoleMember,
		}},
	}))
	recorder := httptest.NewRecorder()
	server.HandleRuntimeServerEvents(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestRuntimeDashboardSummaryRejectsNonGet(t *testing.T) {
	server := &RuntimeServer{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/dashboard/summary", nil)

	server.HandleDashboardSummary(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("allow"); got != http.MethodGet {
		t.Fatalf("allow = %q, want GET", got)
	}
}

func TestRuntimePolicyRejectsNonGet(t *testing.T) {
	server := &RuntimeServer{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/policy?namespace=mcp-team-acme&server=demo", nil)

	server.HandleRuntimePolicy(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("allow"); got != http.MethodGet {
		t.Fatalf("allow = %q, want GET", got)
	}
}

func TestRuntimeComponentsRequiresAdmin(t *testing.T) {
	server := &RuntimeServer{}
	userReq := httptest.NewRequest(http.MethodGet, "/api/runtime/components", nil)
	userReq = userReq.WithContext(withPrincipal(userReq.Context(), principal{Role: roleUser, Subject: "user-1"}))
	userRec := httptest.NewRecorder()
	server.HandleRuntimeComponents(userRec, userReq)
	if userRec.Code != http.StatusForbidden {
		t.Fatalf("user status = %d body=%s", userRec.Code, userRec.Body.String())
	}

	adminReq := httptest.NewRequest(http.MethodGet, "/api/runtime/components", nil)
	adminReq = adminReq.WithContext(withPrincipal(adminReq.Context(), principal{Role: roleAdmin, Subject: "admin-1"}))
	adminRec := httptest.NewRecorder()
	server.HandleRuntimeComponents(adminRec, adminReq)
	if adminRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("admin status = %d body=%s", adminRec.Code, adminRec.Body.String())
	}
}

func TestRuntimeComponentsRejectsNonGet(t *testing.T) {
	server := &RuntimeServer{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/components", nil)

	server.HandleRuntimeComponents(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("allow"); got != http.MethodGet {
		t.Fatalf("allow = %q, want GET", got)
	}
}

func TestActionRestartRejectsNonPost(t *testing.T) {
	server := &RuntimeServer{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/runtime/actions/restart", nil)

	server.HandleActionRestart(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("allow"); got != http.MethodPost {
		t.Fatalf("allow = %q, want POST", got)
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

func TestRuntimeGrantDeleteAllowsNamespaceOwnerWithStaleServerRef(t *testing.T) {
	ctx := context.Background()
	accessMgr := newTestAccessManagerWithObjects(t)
	for _, name := range []string{"stale-owner", "stale-member"} {
		if _, err := accessMgr.ApplyGrant(ctx, &sentinelaccess.MCPAccessGrant{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "mcp-team-acme"},
			Spec: sentinelaccess.MCPAccessGrantSpec{
				ServerRef:          sentinelaccess.ServerReference{Name: "deleted"},
				Subject:            sentinelaccess.SubjectRef{HumanID: "other-user", TeamID: "team-acme-id"},
				MaxTrust:           sentinelaccess.TrustLevel("low"),
				AllowedSideEffects: []sentinelaccess.ToolSideEffect{"read"},
			},
		}); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	server := &RuntimeServer{accessMgr: accessMgr}

	memberReq := httptest.NewRequest(http.MethodDelete, "/api/runtime/grants/mcp-team-acme/stale-member", nil)
	memberReq = memberReq.WithContext(withPrincipal(memberReq.Context(), principal{
		Role:      roleUser,
		Subject:   "member-1",
		Namespace: "mcp-team-acme",
		Teams: []principalTeam{{
			ID:        "team-acme-id",
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      teamRoleMember,
		}},
	}))
	memberRec := httptest.NewRecorder()
	server.handleGrantDelete(memberRec, memberReq, "mcp-team-acme", "stale-member")
	if memberRec.Code != http.StatusForbidden {
		t.Fatalf("member status = %d body=%s", memberRec.Code, memberRec.Body.String())
	}

	ownerReq := httptest.NewRequest(http.MethodDelete, "/api/runtime/grants/mcp-team-acme/stale-owner", nil)
	ownerReq = ownerReq.WithContext(withPrincipal(ownerReq.Context(), principal{
		Role:      roleUser,
		Subject:   "owner-1",
		Namespace: "mcp-team-acme",
		Teams: []principalTeam{{
			ID:        "team-acme-id",
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      teamRoleOwner,
		}},
	}))
	ownerRec := httptest.NewRecorder()
	server.handleGrantDelete(ownerRec, ownerReq, "mcp-team-acme", "stale-owner")
	if ownerRec.Code != http.StatusOK {
		t.Fatalf("owner status = %d body=%s", ownerRec.Code, ownerRec.Body.String())
	}
	if _, err := accessMgr.GetGrant(ctx, "stale-owner", "mcp-team-acme"); !apierrors.IsNotFound(err) {
		t.Fatalf("stale-owner should be deleted, got err=%v", err)
	}
	if _, err := accessMgr.GetGrant(ctx, "stale-member", "mcp-team-acme"); err != nil {
		t.Fatalf("stale-member should remain after forbidden delete: %v", err)
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

func TestRuntimeSessionDeleteAllowsNamespaceOwnerWithStaleServerRef(t *testing.T) {
	ctx := context.Background()
	accessMgr := newTestAccessManagerWithObjects(t)
	if _, err := accessMgr.ApplySession(ctx, &sentinelaccess.MCPAgentSession{
		ObjectMeta: metav1.ObjectMeta{Name: "stale-session", Namespace: "mcp-team-acme"},
		Spec: sentinelaccess.MCPAgentSessionSpec{
			ServerRef:      sentinelaccess.ServerReference{Name: "deleted"},
			Subject:        sentinelaccess.SubjectRef{HumanID: "other-user", TeamID: "team-acme-id"},
			ConsentedTrust: sentinelaccess.TrustLevel("low"),
		},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	server := &RuntimeServer{accessMgr: accessMgr}
	request := httptest.NewRequest(http.MethodDelete, "/api/runtime/sessions/mcp-team-acme/stale-session", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "owner-1",
		Namespace: "mcp-team-acme",
		Teams: []principalTeam{{
			ID:        "team-acme-id",
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      teamRoleOwner,
		}},
	}))
	recorder := httptest.NewRecorder()
	server.handleSessionDelete(recorder, request, "mcp-team-acme", "stale-session")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if _, err := accessMgr.GetSession(ctx, "stale-session", "mcp-team-acme"); !apierrors.IsNotFound(err) {
		t.Fatalf("stale-session should be deleted, got err=%v", err)
	}
}

func TestRuntimeSessionListScopesTeamMemberAccessResources(t *testing.T) {
	ctx := context.Background()
	accessMgr := newTestAccessManagerWithObjects(t, &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: "mcp-team-acme",
			Labels: map[string]string{
				platformUserIDLabel: "owner-1",
			},
		},
		Spec: mcpv1alpha1.MCPServerSpec{TeamID: "team-acme-id"},
	})
	if _, err := accessMgr.ApplySession(ctx, &sentinelaccess.MCPAgentSession{
		ObjectMeta: metav1.ObjectMeta{Name: "session-team", Namespace: "mcp-team-acme"},
		Spec: sentinelaccess.MCPAgentSessionSpec{
			ServerRef:      sentinelaccess.ServerReference{Name: "demo"},
			Subject:        sentinelaccess.SubjectRef{HumanID: "other-user", TeamID: "team-acme-id"},
			ConsentedTrust: sentinelaccess.TrustLevel("low"),
		},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	server := &RuntimeServer{accessMgr: accessMgr}
	request := httptest.NewRequest(http.MethodGet, "/api/runtime/sessions?namespace=mcp-team-acme", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "mcp-team-acme",
		Teams: []principalTeam{{
			ID:        "team-acme-id",
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      teamRoleMember,
		}},
	}))
	recorder := httptest.NewRecorder()
	server.handleRuntimeSessionList(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Sessions []sentinelaccess.SessionSummary `json:"sessions"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	if len(payload.Sessions) != 0 {
		t.Fatalf("sessions = %#v, want no sensitive sessions", payload.Sessions)
	}
}

func TestRuntimeSessionListPrefetchesServersForVisibility(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme, &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: "mcp-team-acme",
		},
		Spec: mcpv1alpha1.MCPServerSpec{TeamID: "team-acme-id"},
	})
	serverGets := 0
	serverLists := 0
	dynamicClient.Fake.PrependReactor("get", "mcpservers", func(ktesting.Action) (bool, runtime.Object, error) {
		serverGets++
		return false, nil, nil
	})
	dynamicClient.Fake.PrependReactor("list", "mcpservers", func(ktesting.Action) (bool, runtime.Object, error) {
		serverLists++
		return false, nil, nil
	})
	accessMgr := sentinelaccess.NewManager(dynamicClient, nil)
	for _, name := range []string{"session-one", "session-two"} {
		if _, err := accessMgr.ApplySession(ctx, &sentinelaccess.MCPAgentSession{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "mcp-team-acme"},
			Spec: sentinelaccess.MCPAgentSessionSpec{
				ServerRef:      sentinelaccess.ServerReference{Name: "demo"},
				Subject:        sentinelaccess.SubjectRef{HumanID: "other-user", TeamID: "team-acme-id"},
				ConsentedTrust: sentinelaccess.TrustLevel("low"),
			},
		}); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	server := &RuntimeServer{accessMgr: accessMgr}
	request := httptest.NewRequest(http.MethodGet, "/api/runtime/sessions?namespace=mcp-team-acme", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "owner-1",
		Namespace: "mcp-team-acme",
		Teams: []principalTeam{{
			ID:        "team-acme-id",
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      teamRoleOwner,
		}},
	}))
	recorder := httptest.NewRecorder()
	server.handleRuntimeSessionList(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Sessions []sentinelaccess.SessionSummary `json:"sessions"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	if len(payload.Sessions) != 2 {
		t.Fatalf("sessions = %#v, want both sessions visible", payload.Sessions)
	}
	if serverLists != 1 {
		t.Fatalf("server list calls = %d, want 1", serverLists)
	}
	if serverGets != 0 {
		t.Fatalf("server get calls = %d, want 0", serverGets)
	}
}

func TestRuntimeSessionToggleRejectsTeamMemberForTeamServer(t *testing.T) {
	ctx := context.Background()
	accessMgr := newTestAccessManagerWithObjects(t, &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: "mcp-team-acme",
			Labels: map[string]string{
				platformUserIDLabel: "owner-1",
			},
		},
		Spec: mcpv1alpha1.MCPServerSpec{TeamID: "team-acme-id"},
	})
	if _, err := accessMgr.ApplySession(ctx, &sentinelaccess.MCPAgentSession{
		ObjectMeta: metav1.ObjectMeta{Name: "session-team", Namespace: "mcp-team-acme"},
		Spec: sentinelaccess.MCPAgentSessionSpec{
			ServerRef:      sentinelaccess.ServerReference{Name: "demo"},
			Subject:        sentinelaccess.SubjectRef{HumanID: "other-user", TeamID: "team-acme-id"},
			ConsentedTrust: sentinelaccess.TrustLevel("low"),
		},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	server := &RuntimeServer{accessMgr: accessMgr}
	request := httptest.NewRequest(http.MethodPost, "/api/runtime/sessions/mcp-team-acme/session-team/revoke", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{
		Role:      roleUser,
		Subject:   "user-1",
		Namespace: "mcp-team-acme",
		Teams: []principalTeam{{
			ID:        "team-acme-id",
			Slug:      "acme",
			Namespace: "mcp-team-acme",
			Role:      teamRoleMember,
		}},
	}))
	recorder := httptest.NewRecorder()
	server.handleSessionToggle(recorder, request, "mcp-team-acme", "session-team", true)
	if recorder.Code != http.StatusForbidden {
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
