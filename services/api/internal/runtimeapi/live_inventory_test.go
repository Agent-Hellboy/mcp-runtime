package runtimeapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
	"mcp-runtime/pkg/controlplane"
	"mcp-runtime/pkg/k8sclient"
)

func TestMCPLiveInventoryProberFetchesLists(t *testing.T) {
	var sawProtocol atomic.Bool
	var sawIdentity atomic.Bool
	fakeMCP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(mcpProtocolHeader) == liveInventoryProtocolVersion {
			sawProtocol.Store(true)
		}
		if r.Header.Get("X-MCP-Agent-ID") == "mcp-runtime-live-inventory" {
			sawIdentity.Store(true)
		}
		var req struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("content-type", "application/json")
		w.Header().Set(mcpSessionHeader, "probe-session")
		switch req.Method {
		case "initialize":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"protocolVersion": liveInventoryProtocolVersion,
				},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"tools": []map[string]any{{
						"name":        "live-add",
						"description": "Add numbers",
						"inputSchema": map[string]any{"type": "object"},
					}},
				},
			})
		case "prompts/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"prompts": []map[string]any{{
						"name":        "summarize",
						"description": "Summarize text",
						"arguments": []map[string]any{{
							"name":     "text",
							"required": true,
						}},
					}},
				},
			})
		case "resources/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"resources": []map[string]any{{
						"uri":      "repo://README.md",
						"name":     "README",
						"mimeType": "text/markdown",
					}},
				},
			})
		default:
			t.Errorf("unexpected method %q", req.Method)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer fakeMCP.Close()

	prober := &mcpLiveInventoryProber{
		client: fakeMCP.Client(),
		baseURLForServer: func(controlplane.ServerInfo) string {
			return fakeMCP.URL
		},
		now: func() time.Time {
			return time.Date(2026, 5, 20, 1, 2, 3, 0, time.UTC)
		},
	}
	got, err := prober.probe(context.Background(), controlplane.ServerInfo{Name: "demo", Namespace: "mcp-servers"})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !sawProtocol.Load() {
		t.Fatal("probe did not send MCP protocol header")
	}
	if !sawIdentity.Load() {
		t.Fatal("probe did not send service identity headers")
	}
	if got.ProtocolVersion != liveInventoryProtocolVersion {
		t.Fatalf("protocolVersion = %q", got.ProtocolVersion)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "live-add" || len(got.Tools[0].InputSchema) == 0 {
		t.Fatalf("tools = %#v", got.Tools)
	}
	if len(got.Prompts) != 1 || got.Prompts[0].Arguments[0].Name != "text" {
		t.Fatalf("prompts = %#v", got.Prompts)
	}
	if len(got.Resources) != 1 || got.Resources[0].URI != "repo://README.md" {
		t.Fatalf("resources = %#v", got.Resources)
	}
	if got.FetchedAt.Format(time.RFC3339) != "2026-05-20T01:02:03Z" {
		t.Fatalf("fetchedAt = %s", got.FetchedAt.Format(time.RFC3339))
	}
}

func TestRuntimeServersReturnsLiveInventoryAfterAsyncProbe(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	srv := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "demo-one",
			Namespace:  "mcp-servers",
			Generation: 7,
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Image:       "demo:latest",
			ServicePort: 80,
			Tools: []mcpv1alpha1.ToolConfig{{
				Name:          "declared-only",
				RequiredTrust: mcpv1alpha1.TrustLevelHigh,
				SideEffect:    mcpv1alpha1.ToolSideEffectRead,
			}},
		},
	}
	fakeMCP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("content-type", "application/json")
		w.Header().Set(mcpSessionHeader, "probe-session")
		result := map[string]any{}
		switch req.Method {
		case "initialize":
			result["protocolVersion"] = liveInventoryProtocolVersion
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
			return
		case "tools/list":
			result["tools"] = []map[string]any{{"name": "live-add", "description": "Add numbers"}}
		case "prompts/list":
			result["prompts"] = []map[string]any{{"name": "summarize"}}
		case "resources/list":
			result["resources"] = []map[string]any{{"uri": "repo://README.md", "name": "README"}}
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
	}))
	defer fakeMCP.Close()

	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic:   dynamicfake.NewSimpleDynamicClient(scheme, srv),
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
		liveInventoryProbe: &mcpLiveInventoryProber{
			client: fakeMCP.Client(),
			baseURLForServer: func(controlplane.ServerInfo) string {
				return fakeMCP.URL
			},
		},
	}

	first := requestRuntimeServers(t, server)
	if len(first.Servers) != 1 {
		t.Fatalf("servers = %d, want 1", len(first.Servers))
	}
	if first.Servers[0].LiveInventory != nil {
		t.Fatalf("first response liveInventory = %#v, want nil cache miss", first.Servers[0].LiveInventory)
	}
	if first.Servers[0].LiveInventoryError != "live inventory pending" {
		t.Fatalf("liveInventoryError = %q", first.Servers[0].LiveInventoryError)
	}

	var got struct {
		Servers []serverInfo `json:"servers"`
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got = requestRuntimeServers(t, server)
		if len(got.Servers) == 1 && got.Servers[0].LiveInventory != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(got.Servers) != 1 || got.Servers[0].LiveInventory == nil {
		t.Fatalf("live inventory was not cached: %#v", got.Servers)
	}
	info := got.Servers[0]
	if len(info.Tools) != 1 || info.Tools[0].Name != "declared-only" {
		t.Fatalf("CRD tools = %#v", info.Tools)
	}
	if len(info.LiveInventory.Tools) != 1 || info.LiveInventory.Tools[0].Name != "live-add" {
		t.Fatalf("live tools = %#v", info.LiveInventory.Tools)
	}
	if len(info.LiveInventory.Prompts) != 1 || info.LiveInventory.Prompts[0].Name != "summarize" {
		t.Fatalf("live prompts = %#v", info.LiveInventory.Prompts)
	}
	if len(info.LiveInventory.Resources) != 1 || info.LiveInventory.Resources[0].URI != "repo://README.md" {
		t.Fatalf("live resources = %#v", info.LiveInventory.Resources)
	}
}

func TestRuntimeServerGetReturnsSingleServerShape(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	srv := &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-one", Namespace: "mcp-servers"},
		Spec:       mcpv1alpha1.MCPServerSpec{Image: "demo:latest", ServicePort: 80},
	}
	server := &RuntimeServer{
		k8sClients: &k8sclient.Clients{
			Dynamic:   dynamicfake.NewSimpleDynamicClient(scheme, srv),
			Clientset: kubernetesfake.NewSimpleClientset(),
		},
		liveInventoryProbe: &countingLiveInventoryProber{},
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/runtime/servers/mcp-servers/demo-one", nil)
	request = request.WithContext(withPrincipal(request.Context(), principal{Role: roleAdmin, Subject: "admin-1"}))
	server.HandleRuntimeServerItem(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Server serverInfo `json:"server"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Server.Name != "demo-one" || payload.Server.Namespace != "mcp-servers" {
		t.Fatalf("server = %#v", payload.Server)
	}
	if payload.Server.LiveInventory != nil || payload.Server.LiveInventoryError != "live inventory pending" {
		t.Fatalf("live inventory = (%#v, %q), want pending", payload.Server.LiveInventory, payload.Server.LiveInventoryError)
	}
}

func TestLiveInventoryCacheInvalidatesOnGenerationBump(t *testing.T) {
	prober := &countingLiveInventoryProber{}
	cache := newLiveInventoryCache(time.Minute, prober)
	info := controlplane.ServerInfo{Name: "demo", Namespace: "mcp-servers", Generation: 1}
	if inv, reason := cache.getOrStart(context.Background(), info); inv != nil || reason != "live inventory pending" {
		t.Fatalf("first get = (%#v, %q), want pending miss", inv, reason)
	}
	waitForCachedInventory(t, cache, info)
	if got := prober.calls.Load(); got != 1 {
		t.Fatalf("probe calls = %d, want 1", got)
	}
	info.Generation = 2
	if inv, reason := cache.getOrStart(context.Background(), info); inv != nil || reason != "live inventory pending" {
		t.Fatalf("generation bump get = (%#v, %q), want pending miss", inv, reason)
	}
	waitForCachedInventory(t, cache, info)
	if got := prober.calls.Load(); got != 2 {
		t.Fatalf("probe calls = %d, want 2 after generation bump", got)
	}
}

type countingLiveInventoryProber struct {
	calls atomic.Int32
}

func (p *countingLiveInventoryProber) probe(context.Context, controlplane.ServerInfo) (*liveInventory, error) {
	call := p.calls.Add(1)
	return &liveInventory{
		FetchedAt:       time.Now().UTC(),
		ProtocolVersion: liveInventoryProtocolVersion,
		Tools:           []liveInventoryTool{{Name: "tool-" + string(rune('0'+call))}},
		Prompts:         []liveInventoryPrompt{},
		Resources:       []liveInventoryResource{},
	}, nil
}

func waitForCachedInventory(t *testing.T, cache *liveInventoryCache, info controlplane.ServerInfo) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if inv, _ := cache.getOrStart(context.Background(), info); inv != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for live inventory cache")
}

func requestRuntimeServers(t *testing.T, server *RuntimeServer) struct {
	Servers []serverInfo `json:"servers"`
} {
	t.Helper()
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
	return payload
}
