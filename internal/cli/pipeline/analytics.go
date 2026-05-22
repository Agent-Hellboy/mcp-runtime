package pipeline

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/cli/kube"
)

const (
	pipelineAnalyticsSourceSecretName = "mcp-sentinel-secrets"
	pipelineAnalyticsSourceSecretKey  = "INGEST_API_KEYS"
	pipelineAnalyticsSecretKey        = "api-key"
	pipelineAnalyticsSecretSuffix     = "-analytics-creds"
)

type analyticsSecretRequest struct {
	Namespace string
	Server    string
	Secret    string
}

func (m *manager) prepareManifestForDeploy(manifestBytes []byte, namespaceOverride string) ([]byte, error) {
	updated, requests, err := injectPipelineAnalyticsSecretRefs(manifestBytes, namespaceOverride)
	if err != nil {
		return nil, err
	}
	if len(requests) == 0 {
		return manifestBytes, nil
	}

	ingestKey, err := m.defaultIngestAPIKey()
	if err != nil {
		return nil, err
	}
	for _, req := range requests {
		if err := m.ensureAnalyticsSecret(req.Namespace, req.Secret, ingestKey); err != nil {
			return nil, fmt.Errorf("ensure analytics secret for %s/%s: %w", req.Namespace, req.Server, err)
		}
	}
	return updated, nil
}

func (m *manager) defaultIngestAPIKey() (string, error) {
	out, err := m.kubectl.CombinedOutput([]string{
		"get", "secret", pipelineAnalyticsSourceSecretName,
		"-n", core.DefaultAnalyticsNamespace,
		"-o", "jsonpath={.data." + pipelineAnalyticsSourceSecretKey + "}",
	})
	if err != nil {
		return "", fmt.Errorf("read %s/%s %s: %w", core.DefaultAnalyticsNamespace, pipelineAnalyticsSourceSecretName, pipelineAnalyticsSourceSecretKey, err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		return "", fmt.Errorf("decode %s/%s %s: %w", core.DefaultAnalyticsNamespace, pipelineAnalyticsSourceSecretName, pipelineAnalyticsSourceSecretKey, err)
	}
	for _, raw := range strings.Split(string(decoded), ",") {
		if value := strings.TrimSpace(raw); value != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf("%s/%s %s is empty", core.DefaultAnalyticsNamespace, pipelineAnalyticsSourceSecretName, pipelineAnalyticsSourceSecretKey)
}

func (m *manager) ensureAnalyticsSecret(namespace, name, ingestKey string) error {
	data := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
			"labels": map[string]string{
				"app.kubernetes.io/managed-by": "mcp-runtime",
			},
		},
		"type": "Opaque",
		"data": map[string]string{
			pipelineAnalyticsSecretKey: base64.StdEncoding.EncodeToString([]byte(ingestKey)),
		},
	}
	manifest, err := yaml.Marshal(data)
	if err != nil {
		return err
	}
	return kube.ApplyManifestContentWithNamespace(m.kubectl.CommandArgs, string(manifest), namespace)
}

func injectPipelineAnalyticsSecretRefs(manifestBytes []byte, namespaceOverride string) ([]byte, []analyticsSecretRequest, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(manifestBytes))
	docs := make([]map[string]any, 0)
	requests := make([]analyticsSecretRequest, 0)
	seen := map[string]struct{}{}
	changed := false

	for {
		var doc map[string]any
		err := decoder.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("decode manifest yaml: %w", err)
		}
		if len(doc) == 0 {
			continue
		}
		if req, ok := injectPipelineAnalyticsSecretRef(doc, namespaceOverride); ok {
			key := req.Namespace + "/" + req.Secret
			if _, exists := seen[key]; !exists {
				seen[key] = struct{}{}
				requests = append(requests, req)
			}
			changed = true
		}
		docs = append(docs, doc)
	}
	if !changed {
		return manifestBytes, nil, nil
	}

	sort.SliceStable(requests, func(i, j int) bool {
		if requests[i].Namespace != requests[j].Namespace {
			return requests[i].Namespace < requests[j].Namespace
		}
		return requests[i].Secret < requests[j].Secret
	})

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	for i, doc := range docs {
		if i > 0 {
			buf.WriteString("---\n")
		}
		if err := encoder.Encode(doc); err != nil {
			_ = encoder.Close()
			return nil, nil, fmt.Errorf("encode manifest yaml: %w", err)
		}
	}
	if err := encoder.Close(); err != nil {
		return nil, nil, fmt.Errorf("close yaml encoder: %w", err)
	}
	return buf.Bytes(), requests, nil
}

func injectPipelineAnalyticsSecretRef(doc map[string]any, namespaceOverride string) (analyticsSecretRequest, bool) {
	if strings.TrimSpace(mapString(doc, "kind")) != "MCPServer" {
		return analyticsSecretRequest{}, false
	}
	metadata := ensureMap(doc, "metadata")
	name := strings.TrimSpace(mapString(metadata, "name"))
	if name == "" {
		return analyticsSecretRequest{}, false
	}
	namespace := strings.TrimSpace(namespaceOverride)
	if namespace == "" {
		namespace = strings.TrimSpace(mapString(metadata, "namespace"))
	}
	if namespace == "" {
		namespace = "default"
	}
	if strings.TrimSpace(namespaceOverride) != "" {
		metadata["namespace"] = namespace
	}

	spec := ensureMap(doc, "spec")
	gateway := mapValue(spec, "gateway")
	if !mapBool(gateway, "enabled") {
		return analyticsSecretRequest{}, false
	}
	analytics := ensureMap(spec, "analytics")
	if mapBool(analytics, "disabled") {
		return analyticsSecretRequest{}, false
	}
	ref := mapValue(analytics, "apiKeySecretRef")
	if strings.TrimSpace(mapString(ref, "name")) != "" && strings.TrimSpace(mapString(ref, "key")) != "" {
		return analyticsSecretRequest{}, false
	}

	secretName := publishedServerAnalyticsSecretName(name)
	analytics["apiKeySecretRef"] = map[string]any{
		"name": secretName,
		"key":  pipelineAnalyticsSecretKey,
	}
	return analyticsSecretRequest{
		Namespace: namespace,
		Server:    name,
		Secret:    secretName,
	}, true
}

func publishedServerAnalyticsSecretName(name string) string {
	const (
		fallback = "analytics-creds"
		maxLen   = 63
	)
	name = strings.TrimSpace(name)
	if name == "" {
		return fallback
	}
	if len(name)+len(pipelineAnalyticsSecretSuffix) > maxLen {
		name = name[:maxLen-len(pipelineAnalyticsSecretSuffix)]
		name = strings.TrimRight(name, "-.")
		if name == "" {
			return fallback
		}
	}
	return name + pipelineAnalyticsSecretSuffix
}

func ensureMap(parent map[string]any, key string) map[string]any {
	if current := mapValue(parent, key); current != nil {
		return current
	}
	next := map[string]any{}
	parent[key] = next
	return next
}

func mapValue(parent map[string]any, key string) map[string]any {
	if parent == nil {
		return nil
	}
	switch value := parent[key].(type) {
	case map[string]any:
		return value
	case map[any]any:
		out := make(map[string]any, len(value))
		for k, v := range value {
			if keyString, ok := k.(string); ok {
				out[keyString] = v
			}
		}
		parent[key] = out
		return out
	default:
		return nil
	}
}

func mapString(parent map[string]any, key string) string {
	if parent == nil {
		return ""
	}
	if value, ok := parent[key].(string); ok {
		return value
	}
	return ""
}

func mapBool(parent map[string]any, key string) bool {
	if parent == nil {
		return false
	}
	if value, ok := parent[key].(bool); ok {
		return value
	}
	return false
}
