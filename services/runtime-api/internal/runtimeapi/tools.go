package runtimeapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
	"mcp-runtime/pkg/controlplane"
)

type runtimeToolRow struct {
	ToolName      string            `json:"tool_name"`
	Description   string            `json:"description,omitempty"`
	ServerName    string            `json:"server_name"`
	Namespace     string            `json:"namespace"`
	TeamID        string            `json:"team_id,omitempty"`
	EndpointURL   string            `json:"endpoint_url,omitempty"`
	Declared      bool              `json:"declared"`
	Live          bool              `json:"live"`
	DriftStatus   string            `json:"drift_status"`
	RequiredTrust string            `json:"required_trust,omitempty"`
	SideEffect    string            `json:"side_effect,omitempty"`
	RiskLevel     string            `json:"risk_level,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	ConnectConfig map[string]any    `json:"connect_config,omitempty"`
}

// HandleRuntimeTools returns a scoped, filterable catalog of tools across visible MCP servers.
func (s *RuntimeServer) HandleRuntimeTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("allow", "GET")
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	control := s.controlPlane()
	if control == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "kubernetes not available")
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	servers, err := s.visibleServers(ctx, control, p, strings.TrimSpace(r.URL.Query().Get("namespace")))
	if err != nil {
		if errors.Is(err, errForbiddenNamespace) {
			writeAPIError(w, http.StatusForbidden, "forbidden namespace")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to list servers")
		return
	}
	rows := s.toolRowsFromServers(ctx, servers, r)
	rows = filterRuntimeToolRows(rows, r)
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Namespace != rows[j].Namespace {
			return rows[i].Namespace < rows[j].Namespace
		}
		if rows[i].ServerName != rows[j].ServerName {
			return rows[i].ServerName < rows[j].ServerName
		}
		return rows[i].ToolName < rows[j].ToolName
	})
	writeJSON(w, http.StatusOK, map[string]any{"tools": rows})
}

func (s *RuntimeServer) visibleServers(ctx context.Context, control *controlplane.Manager, p principal, namespace string) ([]controlplane.ServerInfo, error) {
	namespaces := []string{namespace}
	adminAllNamespaces := false
	if p.Role != roleAdmin {
		if namespace == "" {
			namespaces = catalogNamespacesForPrincipal(p)
		} else if !principalCanReadNamespace(p, namespace) {
			return nil, errForbiddenNamespace
		}
	} else if namespace == "" {
		adminAllNamespaces = true
		namespaces = []string{metav1.NamespaceAll}
	}
	if !adminAllNamespaces {
		namespaces = dedupeNonEmptyStrings(namespaces)
	}
	if len(namespaces) == 0 && !adminAllNamespaces {
		return []controlplane.ServerInfo{}, nil
	}

	servers := make([]controlplane.ServerInfo, 0)
	for _, namespace := range namespaces {
		if p.Role != roleAdmin && !principalCanReadNamespace(p, namespace) {
			return nil, errForbiddenNamespace
		}
		result, err := control.ListServers(ctx, namespace)
		if err != nil {
			return nil, err
		}
		if result.CRDError != nil && !apierrors.IsNotFound(result.CRDError) {
			log.Printf("runtime servers: list MCPServers failed in namespace %q: %v", namespace, result.CRDError)
		}
		servers = append(servers, result.Servers...)
	}
	return servers, nil
}

func (s *RuntimeServer) toolRowsFromServers(ctx context.Context, servers []controlplane.ServerInfo, r *http.Request) []runtimeToolRow {
	rows := make([]runtimeToolRow, 0)
	cache := s.liveInventory()
	for _, server := range servers {
		info := serverInfoWithAccessJSON(server, r)
		connectEndpoint := publicMCPConnectEndpoint(server.Endpoint, r)
		declared := map[string]mcpv1alpha1.ToolConfig{}
		for _, tool := range server.Tools {
			name := strings.TrimSpace(tool.Name)
			if name == "" {
				continue
			}
			declared[name] = tool
		}

		live := map[string]liveInventoryTool{}
		haveLiveInventory := false
		if cache != nil {
			if inventory, _ := cache.getOrStart(ctx, server); inventory != nil {
				haveLiveInventory = true
				for _, tool := range inventory.Tools {
					name := strings.TrimSpace(tool.Name)
					if name != "" {
						live[name] = tool
					}
				}
			}
		}

		seen := map[string]struct{}{}
		for name, tool := range declared {
			_, isLive := live[name]
			drift := "declared"
			if haveLiveInventory && !isLive {
				drift = "missing"
			}
			rows = append(rows, runtimeToolRow{
				ToolName:      name,
				Description:   strings.TrimSpace(tool.Description),
				ServerName:    server.Name,
				Namespace:     server.Namespace,
				TeamID:        server.TeamID,
				EndpointURL:   connectEndpoint,
				Declared:      true,
				Live:          isLive,
				DriftStatus:   drift,
				RequiredTrust: string(defaultRuntimeToolTrust(tool.RequiredTrust)),
				SideEffect:    string(tool.SideEffect),
				RiskLevel:     computeRuntimeToolRisk(tool.RiskLevel, tool.RequiredTrust, tool.SideEffect),
				Labels:        copyRuntimeLabels(tool.Labels),
				ConnectConfig: info.AccessJSON,
			})
			seen[name] = struct{}{}
		}
		for name, tool := range live {
			if _, ok := seen[name]; ok {
				continue
			}
			rows = append(rows, runtimeToolRow{
				ToolName:      name,
				Description:   strings.TrimSpace(tool.Description),
				ServerName:    server.Name,
				Namespace:     server.Namespace,
				TeamID:        server.TeamID,
				EndpointURL:   connectEndpoint,
				Declared:      false,
				Live:          true,
				DriftStatus:   "ungoverned",
				ConnectConfig: info.AccessJSON,
			})
		}
	}
	return rows
}

func filterRuntimeToolRows(rows []runtimeToolRow, r *http.Request) []runtimeToolRow {
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("query")))
	if query == "" {
		query = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	}
	namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
	team := strings.TrimSpace(r.URL.Query().Get("team"))
	server := strings.TrimSpace(r.URL.Query().Get("server"))
	trust := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("trust")))
	sideEffect := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("side_effect")))
	if sideEffect == "" {
		sideEffect = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sideEffect")))
	}
	risk := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("risk")))
	drift := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("drift")))

	out := rows[:0]
	for _, row := range rows {
		if namespace != "" && row.Namespace != namespace {
			continue
		}
		if team != "" && row.TeamID != team {
			continue
		}
		if server != "" && row.ServerName != server {
			continue
		}
		if trust != "" && strings.ToLower(row.RequiredTrust) != trust {
			continue
		}
		if sideEffect != "" && strings.ToLower(row.SideEffect) != sideEffect {
			continue
		}
		if risk != "" && strings.ToLower(row.RiskLevel) != risk {
			continue
		}
		if drift != "" && strings.ToLower(row.DriftStatus) != drift {
			continue
		}
		if query != "" && !strings.Contains(toolRowSearchText(row), query) {
			continue
		}
		out = append(out, row)
	}
	return out
}

func toolRowSearchText(row runtimeToolRow) string {
	parts := []string{
		row.ToolName,
		row.Description,
		row.ServerName,
		row.Namespace,
		row.TeamID,
		row.EndpointURL,
		row.RequiredTrust,
		row.SideEffect,
		row.RiskLevel,
		row.DriftStatus,
	}
	for key, value := range row.Labels {
		parts = append(parts, key, value)
	}
	return strings.ToLower(strings.Join(parts, " "))
}

func defaultRuntimeToolTrust(value mcpv1alpha1.TrustLevel) mcpv1alpha1.TrustLevel {
	if value == "" {
		return mcpv1alpha1.TrustLevelLow
	}
	return value
}

func computeRuntimeToolRisk(risk mcpv1alpha1.ToolRiskLevel, trust mcpv1alpha1.TrustLevel, sideEffect mcpv1alpha1.ToolSideEffect) string {
	switch risk {
	case mcpv1alpha1.ToolRiskLevelLow, mcpv1alpha1.ToolRiskLevelMedium, mcpv1alpha1.ToolRiskLevelHigh:
		return string(risk)
	}
	trust = defaultRuntimeToolTrust(trust)
	switch {
	case sideEffect == mcpv1alpha1.ToolSideEffectDestructive || trust == mcpv1alpha1.TrustLevelHigh:
		return string(mcpv1alpha1.ToolRiskLevelHigh)
	case sideEffect == mcpv1alpha1.ToolSideEffectWrite || trust == mcpv1alpha1.TrustLevelMedium:
		return string(mcpv1alpha1.ToolRiskLevelMedium)
	case sideEffect == mcpv1alpha1.ToolSideEffectRead && trust == mcpv1alpha1.TrustLevelLow:
		return string(mcpv1alpha1.ToolRiskLevelLow)
	default:
		return ""
	}
}

func copyRuntimeLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	out := make(map[string]string, len(labels))
	for key, value := range labels {
		out[key] = value
	}
	return out
}
