package platform

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"mcp-runtime/internal/cli/core"
	"mcp-runtime/internal/cli/kube"
	"mcp-runtime/internal/cli/registry"
	"mcp-runtime/internal/cli/setup/assetpath"
	"mcp-runtime/internal/cli/setup/ingressmanifest"
	setupplan "mcp-runtime/internal/cli/setup/plan"
)

func deployAnalyticsManifests(logger *zap.Logger, images AnalyticsImageSet, storageMode, platformMode string) error {
	return deployAnalyticsManifestsWithKubectl(core.DefaultKubectlClient(), logger, images, storageMode, platformMode)
}

func deployAnalyticsManifestsWithKubectl(kubectl core.KubectlRunner, logger *zap.Logger, images AnalyticsImageSet, storageMode, platformMode string) error {
	rolloutTimeout := analyticsRolloutTimeoutString()

	if err := ensureRepoManagedTraefikMiddlewareResources(kubectl, logger); err != nil {
		return err
	}

	core.Info("Applying mcp-sentinel namespace and config")
	manifests := []string{
		"k8s/00-namespace.yaml",
		"k8s/01-config.yaml",
	}
	for _, manifest := range manifests {
		if err := applyRenderedManifest(kubectl, manifest, images, "", platformMode); err != nil {
			return err
		}
	}
	if err := applyCatalogNamespaceForMode(kubectl, platformMode); err != nil {
		return err
	}

	core.Info("Applying mcp-sentinel managed secrets")
	secretManifest, err := renderAnalyticsSecretManifest(kubectl)
	if err != nil {
		return err
	}
	if err := kube.ApplyManifestContent(kubectl.CommandArgs, secretManifest); err != nil {
		return err
	}

	imagePullSecretName, err := ensureAnalyticsImagePullSecret(kubectl)
	if err != nil {
		return err
	}

	clickhouseManifest := "k8s/03-clickhouse.yaml"
	kafkaManifest := "k8s/05-kafka.yaml"
	postgresManifest := "k8s/20-postgres.yaml"
	if storageMode == setupplan.StorageModeHostpath {
		clickhouseManifest = "k8s/03-clickhouse-hostpath.yaml"
		kafkaManifest = "k8s/05-kafka-hostpath.yaml"
		postgresManifest = "k8s/20-postgres-hostpath.yaml"
	}

	core.Info("Applying analytics storage and messaging components")
	for _, manifest := range []string{
		clickhouseManifest,
		kafkaManifest,
	} {
		if err := applyRenderedManifest(kubectl, manifest, images, imagePullSecretName, platformMode); err != nil {
			return err
		}
	}

	if err := waitForRolloutStatusWithKubectl(kubectl, "statefulset", "clickhouse", core.DefaultAnalyticsNamespace, rolloutTimeout); err != nil {
		return mcpSentinelDependencyRolloutFailed(kubectl, err, "statefulset", "clickhouse", core.DefaultAnalyticsNamespace, "storage (clickhouse)")
	}
	if err := waitForRolloutStatusWithKubectl(kubectl, "deployment", "zookeeper", core.DefaultAnalyticsNamespace, rolloutTimeout); err != nil {
		return mcpSentinelDependencyRolloutFailed(kubectl, err, "deployment", "zookeeper", core.DefaultAnalyticsNamespace, "messaging (zookeeper)")
	}
	if err := waitForRolloutStatusWithKubectl(kubectl, "statefulset", "kafka", core.DefaultAnalyticsNamespace, rolloutTimeout); err != nil {
		return mcpSentinelDependencyRolloutFailed(kubectl, err, "statefulset", "kafka", core.DefaultAnalyticsNamespace, "messaging (kafka)")
	}

	core.Info("Initializing ClickHouse schema")
	if err := deleteJobIfExistsWithKubectl(kubectl, "clickhouse-init", core.DefaultAnalyticsNamespace); err != nil {
		return fmt.Errorf("delete existing clickhouse init job: %w", err)
	}
	if err := applyRenderedManifest(kubectl, "k8s/04-clickhouse-init.yaml", images, imagePullSecretName, platformMode); err != nil {
		return err
	}
	if err := waitForJobCompletionWithKubectl(kubectl, "clickhouse-init", core.DefaultAnalyticsNamespace, rolloutTimeout); err != nil {
		return mcpSentinelDependencyJobFailed(kubectl, err, "clickhouse-init", core.DefaultAnalyticsNamespace, "clickhouse init schema")
	}

	core.Info("Applying analytics services")
	for _, manifest := range []string{
		postgresManifest,
		"k8s/06-ingest.yaml",
		"k8s/07-processor.yaml",
		"k8s/08-api.yaml",
		"k8s/08-api-rbac.yaml",
		"k8s/09-ui.yaml",
		"k8s/10-gateway.yaml",
		"k8s/11-prometheus.yaml",
		"k8s/15-otel-collector.yaml",
		"k8s/16-tempo.yaml",
		"k8s/17-loki.yaml",
		"k8s/18-promtail.yaml",
		"k8s/19-grafana-datasources.yaml",
		"k8s/12-grafana.yaml",
	} {
		if err := applyRenderedManifest(kubectl, manifest, images, imagePullSecretName, platformMode); err != nil {
			return err
		}
	}

	if err := applyPlatformIngressIfConfigured(kubectl); err != nil {
		return err
	}

	core.Info(fmt.Sprintf("Waiting for mcp-sentinel workload rollouts (per-resource timeout %s; override with MCP_DEPLOYMENT_TIMEOUT)", rolloutTimeout))
	targets := []struct{ kind, name string }{
		{kind: "statefulset", name: "mcp-sentinel-postgres"},
		{kind: "deployment", name: "mcp-sentinel-ingest"},
		{kind: "deployment", name: "mcp-sentinel-processor"},
		{kind: "deployment", name: "mcp-sentinel-api"},
		{kind: "deployment", name: "mcp-sentinel-ui"},
		{kind: "deployment", name: "mcp-sentinel-gateway"},
		{kind: "deployment", name: "prometheus"},
		{kind: "deployment", name: "grafana"},
		{kind: "deployment", name: "otel-collector"},
		{kind: "statefulset", name: "tempo"},
		{kind: "statefulset", name: "loki"},
	}
	var rolloutFailures []string
	var failedForDebug []analyticsFailedRollout
	for _, target := range targets {
		rolloutLog, err := runRolloutWithOptionalDebugCapture(kubectl, target.kind, target.name, core.DefaultAnalyticsNamespace, rolloutTimeout)
		if err != nil {
			rolloutFailures = append(rolloutFailures, fmt.Sprintf("%s/%s: %v", target.kind, target.name, err))
			failedForDebug = append(failedForDebug, analyticsFailedRollout{
				kind: target.kind, name: target.name, rolloutLog: rolloutLog,
			})
		}
	}
	if len(rolloutFailures) == 0 {
		core.Success("mcp-sentinel manifests deployed successfully")
		return nil
	}

	printAnalyticsRolloutDiagnostics(kubectl)
	summary := strings.Join(rolloutFailures, "; ")
	cause := errors.New(summary)
	msg := fmt.Sprintf("analytics components failed to roll out: %s", summary)
	ctx := map[string]any{"component": "mcp-sentinel", "rollout_failures": summary}
	if core.IsDebugMode() {
		if diag := buildAnalyticsRolloutDebugDetail(kubectl, failedForDebug); diag != "" {
			ctx["diagnostics"] = trimDiagnosticsString(diag)
		}
	}
	return core.WrapWithSentinelAndContext(core.ErrOperatorDeploymentFailed, cause, msg, ctx)
}

func applyCatalogNamespaceForMode(kubectl core.KubectlRunner, platformMode string) error {
	namespace := setupplan.CatalogNamespaceForPlatformMode(platformMode)
	if strings.TrimSpace(namespace) == "" {
		return nil
	}
	core.Info(fmt.Sprintf("Applying platform catalog namespace %s", namespace))
	return kube.EnsureNamespaceWithLabels(kubectl.CommandArgs, namespace, catalogNamespaceLabels(platformMode))
}

func trimDiagnosticsString(s string) string {
	const maxBytes = 300 * 1024
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + "\n... [diagnostics truncated]\n"
}

// buildAnalyticsRolloutDebugDetail collects kubectl output for mcp-sentinel (describe + get) when --debug is set.
func buildAnalyticsRolloutDebugDetail(kubectl core.KubectlRunner, failed []analyticsFailedRollout) string {
	var b strings.Builder
	for _, w := range failed {
		if strings.TrimSpace(w.rolloutLog) != "" {
			b.WriteString(fmt.Sprintf("---- kubectl rollout status %s/%s\n", w.kind, w.name))
			b.WriteString(w.rolloutLog)
		}
		b.WriteString(fmt.Sprintf("---- describe %s %s\n", w.kind, w.name))
		out, err := kubectlText(kubectl, []string{
			"describe", w.kind, w.name, "-n", core.DefaultAnalyticsNamespace, "--request-timeout=30s",
		})
		if err != nil {
			b.WriteString(fmt.Sprintf("error: %v\n", err))
			continue
		}
		b.WriteString(out)
	}
	b.WriteString("---- get pods (wide)\n")
	if out, err := kubectlText(kubectl, []string{"get", "pods", "-n", core.DefaultAnalyticsNamespace, "-o", "wide", "--request-timeout=30s"}); err != nil {
		b.WriteString(fmt.Sprintf("error: %v\n", err))
	} else {
		b.WriteString(out)
	}
	b.WriteString("---- get events (sorted)\n")
	if out, err := kubectlText(kubectl, []string{
		"get", "events", "-n", core.DefaultAnalyticsNamespace, "--sort-by", ".lastTimestamp", "--request-timeout=30s",
	}); err != nil {
		b.WriteString(fmt.Sprintf("error: %v\n", err))
	} else {
		b.WriteString(out)
	}
	return b.String()
}

func applyRenderedManifest(kubectl core.KubectlRunner, manifestPath string, images AnalyticsImageSet, imagePullSecretName, platformMode string) error {
	resolvedManifestPath, err := assetpath.ResolveRepoAssetPath(manifestPath)
	if err != nil {
		return core.WrapWithSentinel(core.ErrReadManagerYAMLFailed, err, fmt.Sprintf("failed to resolve manifest %s: %v", manifestPath, err))
	}

	content, err := kube.ReadFileAtPath(resolvedManifestPath)
	if err != nil {
		return core.WrapWithSentinel(core.ErrReadManagerYAMLFailed, err, fmt.Sprintf("failed to read manifest %s: %v", resolvedManifestPath, err))
	}
	rendered := ""
	if manifestPath == "k8s/01-config.yaml" {
		rendered, err = renderAnalyticsConfigManifest(kubectl, string(content), platformMode, images)
	} else {
		rendered, err = renderAnalyticsManifest(string(content), images, imagePullSecretName, platformMode)
	}
	if err != nil {
		return fmt.Errorf("render manifest %s: %w", manifestPath, err)
	}
	return kube.ApplyManifestContent(kubectl.CommandArgs, rendered)
}

func applyPlatformIngressIfConfigured(kubectl core.KubectlRunner) error {
	host := strings.TrimSpace(core.GetPlatformIngressHost())
	if host == "" {
		return nil
	}
	manifest := ingressmanifest.RenderPlatformUIIngress(host, core.GetRegistryClusterIssuerName(), core.DefaultAnalyticsNamespace)
	core.Info(fmt.Sprintf("Applying platform UI ingress for %s", host))
	if err := kube.ApplyManifestContent(kubectl.CommandArgs, manifest); err != nil {
		return fmt.Errorf("apply platform UI ingress: %w", err)
	}
	if err := removePathBasedSentinelIngresses(kubectl); err != nil {
		return err
	}
	return nil
}

func removePathBasedSentinelIngresses(kubectl core.KubectlRunner) error {
	args := append([]string{"delete", "ingress"}, pathBasedSentinelIngressNames...)
	args = append(args, "-n", core.DefaultAnalyticsNamespace, "--ignore-not-found=true")
	if err := kubectl.RunWithOutput(args, os.Stdout, os.Stderr); err != nil {
		return fmt.Errorf("remove path-based sentinel ingresses for public platform host: %w", err)
	}
	return nil
}

func renderAnalyticsManifest(content string, images AnalyticsImageSet, imagePullSecretName, platformMode string) (string, error) {
	replacements := map[string]string{}
	if mode, ok := setupplan.NormalizePlatformMode(platformMode); ok && mode != "" {
		replacements[`PLATFORM_MODE: "tenant"`] = fmt.Sprintf(`PLATFORM_MODE: "%s"`, mode)
	}
	if strings.TrimSpace(images.Ingest) != "" {
		replacements["image: mcp-sentinel-ingest:latest"] = "image: " + images.Ingest
	}
	if strings.TrimSpace(images.API) != "" {
		replacements["image: mcp-sentinel-api:latest"] = "image: " + images.API
	}
	if strings.TrimSpace(images.Processor) != "" {
		replacements["image: mcp-sentinel-processor:latest"] = "image: " + images.Processor
	}
	if strings.TrimSpace(images.UI) != "" {
		replacements["image: mcp-sentinel-ui:latest"] = "image: " + images.UI
	}
	if strings.TrimSpace(images.Traefik) != "" {
		replacements["image: traefik:v3.0"] = "image: " + images.Traefik
	}
	if strings.TrimSpace(images.ClickHouse) != "" {
		replacements["image: clickhouse/clickhouse-server:23.8"] = "image: " + images.ClickHouse
	}
	if strings.TrimSpace(images.Zookeeper) != "" {
		replacements["image: confluentinc/cp-zookeeper:7.5.1"] = "image: " + images.Zookeeper
	}
	if strings.TrimSpace(images.Kafka) != "" {
		replacements["image: confluentinc/cp-kafka:7.5.1"] = "image: " + images.Kafka
	}
	if strings.TrimSpace(images.Prometheus) != "" {
		replacements["image: prom/prometheus:v2.49.1"] = "image: " + images.Prometheus
	}
	if strings.TrimSpace(images.OTelCollector) != "" {
		replacements["image: otel/opentelemetry-collector:0.92.0"] = "image: " + images.OTelCollector
	}
	if strings.TrimSpace(images.Tempo) != "" {
		replacements["image: grafana/tempo:2.3.1"] = "image: " + images.Tempo
	}
	if strings.TrimSpace(images.Loki) != "" {
		replacements["image: grafana/loki:2.9.4"] = "image: " + images.Loki
	}
	if strings.TrimSpace(images.Promtail) != "" {
		replacements["image: grafana/promtail:2.9.4"] = "image: " + images.Promtail
	}
	if strings.TrimSpace(images.Grafana) != "" {
		replacements["image: grafana/grafana:10.2.3"] = "image: " + images.Grafana
	}
	rendered := content
	for oldValue, newValue := range replacements {
		rendered = strings.ReplaceAll(rendered, oldValue, newValue)
	}
	if strings.TrimSpace(imagePullSecretName) == "" {
		return rendered, nil
	}

	rendered, err := injectImagePullSecretsIntoManifest(rendered, imagePullSecretName)
	if err != nil {
		return "", err
	}
	return rendered, nil
}

func renderAnalyticsConfigManifest(kubectl core.KubectlRunner, content, platformMode string, images AnalyticsImageSet) (string, error) {
	type configMapManifest struct {
		APIVersion string            `yaml:"apiVersion"`
		Kind       string            `yaml:"kind"`
		Metadata   map[string]any    `yaml:"metadata"`
		Data       map[string]string `yaml:"data"`
	}

	var manifest configMapManifest
	if err := yaml.Unmarshal([]byte(content), &manifest); err != nil {
		return "", fmt.Errorf("decode analytics config manifest: %w", err)
	}
	if manifest.Data == nil {
		manifest.Data = map[string]string{}
	}

	existingData, err := existingConfigMapData(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-config")
	if err != nil {
		return "", err
	}
	for _, key := range []string{
		"GOOGLE_CLIENT_ID",
		"MCP_SENTINEL_INGEST_URL",
		"OIDC_ISSUER",
		"OIDC_AUDIENCE",
		"OIDC_JWKS_URL",
		"MCP_PLATFORM_DOMAIN",
		"MCP_MCP_INGRESS_HOST",
		"MCP_REGISTRY_ENDPOINT",
		"MCP_REGISTRY_INGRESS_HOST",
		"PLATFORM_REGISTRY_URL",
		"PLATFORM_TRAEFIK_NAMESPACE",
	} {
		if envValue := setupAnalyticsConfigEnvValue(key); envValue != "" {
			manifest.Data[key] = envValue
			continue
		}
		if strings.TrimSpace(manifest.Data[key]) == "" && strings.TrimSpace(existingData[key]) != "" {
			manifest.Data[key] = existingData[key]
		}
	}
	applyGoogleOIDCDefaults(manifest.Data)
	if registryEndpoint := strings.TrimSpace(core.GetRegistryEndpoint()); registryEndpoint != "" {
		manifest.Data["MCP_REGISTRY_ENDPOINT"] = registryEndpoint
	}
	if registryIngressHost := strings.TrimSpace(core.GetRegistryIngressHost()); registryIngressHost != "" {
		manifest.Data["MCP_REGISTRY_INGRESS_HOST"] = registryIngressHost
	}
	if registryHost := platformRegistryHostForConfig(images); registryHost != "" {
		manifest.Data["PLATFORM_REGISTRY_URL"] = registryHost
	}
	if strings.TrimSpace(manifest.Data["PLATFORM_TRAEFIK_NAMESPACE"]) == "" {
		if namespace := activeTraefikNamespaceForPlatform(kubectl); namespace != "" {
			manifest.Data["PLATFORM_TRAEFIK_NAMESPACE"] = namespace
		}
	}
	if existingMode, ok := setupplan.NormalizePlatformMode(existingData["PLATFORM_MODE"]); ok {
		requestedMode, requestedOK := setupplan.NormalizePlatformMode(platformMode)
		switch {
		case !requestedOK:
			manifest.Data["PLATFORM_MODE"] = existingMode
		case requestedMode == setupplan.PlatformModeTenant && existingMode != setupplan.PlatformModeTenant:
			manifest.Data["PLATFORM_MODE"] = existingMode
		default:
			manifest.Data["PLATFORM_MODE"] = requestedMode
		}
	} else if mode, ok := setupplan.NormalizePlatformMode(platformMode); ok {
		manifest.Data["PLATFORM_MODE"] = mode
	}

	rendered, err := yaml.Marshal(manifest)
	if err != nil {
		return "", fmt.Errorf("encode analytics config manifest: %w", err)
	}
	return string(rendered), nil
}

func setupAnalyticsConfigEnvValue(key string) string {
	candidates := []string{key}
	if key == "GOOGLE_CLIENT_ID" {
		candidates = append(candidates, "MCP_GOOGLE_CLIENT_ID")
	} else if strings.HasPrefix(key, "OIDC_") {
		candidates = append(candidates, "MCP_"+key)
	}
	for _, candidate := range candidates {
		if value := strings.TrimSpace(os.Getenv(candidate)); value != "" {
			return value
		}
	}
	return ""
}

func applyGoogleOIDCDefaults(data map[string]string) {
	clientID := strings.TrimSpace(data["GOOGLE_CLIENT_ID"])
	if clientID == "" {
		return
	}
	if strings.TrimSpace(data["OIDC_ISSUER"]) == "" {
		data["OIDC_ISSUER"] = "https://accounts.google.com"
	}
	if strings.TrimSpace(data["OIDC_AUDIENCE"]) == "" {
		data["OIDC_AUDIENCE"] = clientID
	}
	if strings.TrimSpace(data["OIDC_JWKS_URL"]) == "" {
		data["OIDC_JWKS_URL"] = "https://www.googleapis.com/oauth2/v3/certs"
	}
}

func existingConfigMapData(kubectl core.KubectlRunner, namespace, name string) (map[string]string, error) {
	cmd, err := kubectl.CommandArgs([]string{"get", "configmap", name, "-n", namespace, "-o", "json"})
	if err != nil {
		return nil, err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.ToLower(strings.TrimSpace(string(out)))
		if strings.Contains(detail, "not found") || strings.Contains(detail, "notfound") {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read configmap %s/%s: %w", namespace, name, err)
	}
	if strings.TrimSpace(string(out)) == "" {
		return map[string]string{}, nil
	}
	var payload struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, fmt.Errorf("decode configmap %s/%s: %w", namespace, name, err)
	}
	if payload.Data == nil {
		return map[string]string{}, nil
	}
	return payload.Data, nil
}

func renderAnalyticsSecretManifest(kubectl core.KubectlRunner) (string, error) {
	apiKeys, err := existingSecretDataValueOrRandom(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "API_KEYS", 16)
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	ingestAPIKeys, err := existingSecretDataValueOrRandom(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "INGEST_API_KEYS", 16)
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	uiAPIKey, err := existingSecretDataValueOrRandom(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "UI_API_KEY", 16)
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	apiKeys = ensureCSVIncludes(apiKeys, uiAPIKey)
	adminAPIKeys, err := existingSecretDataValue(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "ADMIN_API_KEYS")
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	adminAPIKeys = ensureCSVIncludes(adminAPIKeys, uiAPIKey)
	grafanaPassword, err := existingSecretDataValueOrRandom(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "GRAFANA_ADMIN_PASSWORD", 16)
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	postgresUser, err := existingSecretDataValueOrDefault(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "POSTGRES_USER", "mcp_runtime")
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	postgresPassword, err := existingSecretDataValueOrRandom(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "POSTGRES_PASSWORD", 16)
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	postgresDB, err := existingSecretDataValueOrDefault(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "POSTGRES_DB", "mcp_runtime")
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	postgresDSN, err := existingSecretDataValue(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "POSTGRES_DSN")
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	if postgresDSN == "" {
		postgresDSN = fmt.Sprintf(
			"postgres://%s@mcp-sentinel-postgres.%s.svc.cluster.local:5432/%s?sslmode=disable",
			url.UserPassword(postgresUser, postgresPassword).String(),
			core.DefaultAnalyticsNamespace,
			postgresDB,
		)
	}
	platformJWTSecret, err := existingSecretDataValueOrRandom(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "PLATFORM_JWT_SECRET", 32)
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	platformAdminEmail, err := existingSecretDataValue(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "PLATFORM_ADMIN_EMAIL")
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	platformAdminPassword, err := existingSecretDataValue(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "PLATFORM_ADMIN_PASSWORD")
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	adminUsers, err := existingSecretDataValue(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "ADMIN_USERS")
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	envPlatformAdminEmail := setupSecretEnvValue("MCP_PLATFORM_ADMIN_EMAIL", "PLATFORM_ADMIN_EMAIL")
	envPlatformAdminPassword := setupSecretEnvValue("MCP_PLATFORM_ADMIN_PASSWORD", "PLATFORM_ADMIN_PASSWORD")
	if envPlatformAdminEmail != "" {
		platformAdminEmail = envPlatformAdminEmail
	}
	if envPlatformAdminPassword != "" {
		platformAdminPassword = envPlatformAdminPassword
	}
	if platformAdminEmail == "" || platformAdminPassword == "" {
		platformAdminEmail = ""
		platformAdminPassword = ""
	}
	adminUserCandidates := []string{setupSecretEnvValue("MCP_ADMIN_USERS", "ADMIN_USERS")}
	if envPlatformAdminEmail != "" {
		adminUserCandidates = append(adminUserCandidates, envPlatformAdminEmail)
	}
	if platformAdminEmail != "" {
		adminUserCandidates = append(adminUserCandidates, platformAdminEmail)
	}
	adminUsers = ensureCSVIncludesValues(adminUsers, adminUserCandidates...)
	platformDevLoginEnabled := ""
	platformDevUserEmail, err := existingSecretDataValue(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "PLATFORM_DEV_USER_EMAIL")
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	platformDevUserPassword, err := existingSecretDataValue(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "PLATFORM_DEV_USER_PASSWORD")
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	platformDevAdminEmail, err := existingSecretDataValue(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "PLATFORM_DEV_ADMIN_EMAIL")
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	platformDevAdminPassword, err := existingSecretDataValue(kubectl, core.DefaultAnalyticsNamespace, "mcp-sentinel-secrets", "PLATFORM_DEV_ADMIN_PASSWORD")
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to read analytics secrets: %v", err))
	}
	if os.Getenv("MCP_RUNTIME_TEST_MODE") == "1" {
		platformDevLoginEnabled = "true"
		if platformDevUserEmail == "" {
			platformDevUserEmail = defaultDevUserEmail
		}
		if platformDevUserPassword == "" {
			platformDevUserPassword = defaultDevUserPassword
		}
		if platformDevAdminEmail == "" {
			platformDevAdminEmail = defaultDevAdminEmail
		}
		if platformDevAdminPassword == "" {
			platformDevAdminPassword = defaultDevAdminPassword
		}
	} else {
		platformDevLoginEnabled = "false"
		platformDevUserEmail = ""
		platformDevUserPassword = ""
		platformDevAdminEmail = ""
		platformDevAdminPassword = ""
	}
	stringData := map[string]string{
		"API_KEYS":                apiKeys,
		"INGEST_API_KEYS":         ingestAPIKeys,
		"ADMIN_API_KEYS":          adminAPIKeys,
		"UI_API_KEY":              uiAPIKey,
		"ADMIN_USERS":             adminUsers,
		"PLATFORM_ADMIN_EMAIL":    platformAdminEmail,
		"PLATFORM_ADMIN_PASSWORD": platformAdminPassword,
		"POSTGRES_USER":           postgresUser,
		"POSTGRES_PASSWORD":       postgresPassword,
		"POSTGRES_DB":             postgresDB,
		"POSTGRES_DSN":            postgresDSN,
		"PLATFORM_JWT_SECRET":     platformJWTSecret,
		"GRAFANA_ADMIN_USER":      "admin",
		"GRAFANA_ADMIN_PASSWORD":  grafanaPassword,
	}
	if platformDevLoginEnabled != "" ||
		platformDevUserEmail != "" ||
		platformDevUserPassword != "" ||
		platformDevAdminEmail != "" ||
		platformDevAdminPassword != "" {
		stringData["PLATFORM_DEV_LOGIN_ENABLED"] = platformDevLoginEnabled
		stringData["PLATFORM_DEV_USER_EMAIL"] = platformDevUserEmail
		stringData["PLATFORM_DEV_USER_PASSWORD"] = platformDevUserPassword
		stringData["PLATFORM_DEV_ADMIN_EMAIL"] = platformDevAdminEmail
		stringData["PLATFORM_DEV_ADMIN_PASSWORD"] = platformDevAdminPassword
	}
	secretManifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]string{
			"name":      "mcp-sentinel-secrets",
			"namespace": core.DefaultAnalyticsNamespace,
		},
		"type":       "Opaque",
		"stringData": stringData,
	}
	rendered, err := yaml.Marshal(secretManifest)
	if err != nil {
		return "", core.WrapWithSentinel(core.ErrRenderSecretManifestFailed, err, fmt.Sprintf("failed to render analytics secrets: %v", err))
	}
	return string(rendered), nil
}

func ensureCSVIncludes(csv, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return strings.TrimSpace(csv)
	}
	parts := make([]string, 0)
	found := false
	for _, part := range strings.Split(csv, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if part == value {
			found = true
		}
		parts = append(parts, part)
	}
	if !found {
		parts = append(parts, value)
	}
	return strings.Join(parts, ",")
}

func ensureCSVIncludesValues(csv string, values ...string) string {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			csv = ensureCSVIncludes(csv, part)
		}
	}
	return csv
}

func setupSecretEnvValue(candidates ...string) string {
	for _, candidate := range candidates {
		if value := strings.TrimSpace(os.Getenv(candidate)); value != "" {
			return value
		}
	}
	return ""
}

func ensureAnalyticsImagePullSecret(kubectl core.KubectlRunner) (string, error) {
	extRegistry, err := registry.ResolveExternalRegistryConfig(nil)
	if err != nil {
		return "", err
	}
	if extRegistry == nil || extRegistry.URL == "" || (extRegistry.Username == "" && extRegistry.Password == "") {
		return "", nil
	}
	if err := ensureImagePullSecretWithKubectl(kubectl, core.DefaultAnalyticsNamespace, defaultRegistrySecretName, extRegistry.URL, extRegistry.Username, extRegistry.Password); err != nil {
		return "", err
	}
	return defaultRegistrySecretName, nil
}

func existingSecretDataValue(kubectl core.KubectlRunner, namespace, name, key string) (string, error) {
	cmd, err := kubectl.CommandArgs([]string{"get", "secret", name, "-n", namespace, "-o", "jsonpath={.data." + key + "}"})
	if err != nil {
		return "", err
	}

	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if err != nil {
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "not found") || strings.Contains(lower, "notfound") {
			return "", nil
		}
		return "", fmt.Errorf("read secret %s/%s key %s: %w", namespace, name, key, err)
	}
	if trimmed == "" {
		return "", nil
	}

	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return "", fmt.Errorf("decode secret %s/%s key %s: %w", namespace, name, key, err)
	}
	return string(decoded), nil
}

func existingSecretDataValueOrRandom(kubectl core.KubectlRunner, namespace, name, key string, size int) (string, error) {
	value, err := existingSecretDataValue(kubectl, namespace, name, key)
	if err != nil {
		return "", err
	}
	if value != "" {
		return value, nil
	}
	return randomHex(size)
}

func existingSecretDataValueOrDefault(kubectl core.KubectlRunner, namespace, name, key, fallback string) (string, error) {
	value, err := existingSecretDataValue(kubectl, namespace, name, key)
	if err != nil {
		return "", err
	}
	if value != "" {
		return value, nil
	}
	return fallback, nil
}

func injectImagePullSecretsIntoManifest(manifest, secretName string) (string, error) {
	decoder := yaml.NewDecoder(strings.NewReader(manifest))
	var renderedDocs []string

	for {
		var doc map[string]any
		err := decoder.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		if len(doc) == 0 {
			continue
		}

		injectImagePullSecretIntoDocument(doc, secretName)
		data, err := yaml.Marshal(doc)
		if err != nil {
			return "", err
		}
		renderedDocs = append(renderedDocs, strings.TrimRight(string(data), "\n"))
	}

	if len(renderedDocs) == 0 {
		return manifest, nil
	}
	return strings.Join(renderedDocs, "\n---\n") + "\n", nil
}

func injectImagePullSecretIntoDocument(doc map[string]any, secretName string) {
	podSpec := manifestPodSpec(doc)
	if podSpec == nil {
		return
	}

	if existing, ok := podSpec["imagePullSecrets"].([]any); ok {
		for _, item := range existing {
			entry, ok := item.(map[string]any)
			if ok && strings.TrimSpace(fmt.Sprint(entry["name"])) == secretName {
				return
			}
		}
		podSpec["imagePullSecrets"] = append(existing, map[string]any{"name": secretName})
		return
	}

	podSpec["imagePullSecrets"] = []map[string]any{{"name": secretName}}
}

func manifestPodSpec(doc map[string]any) map[string]any {
	kind := strings.ToLower(strings.TrimSpace(fmt.Sprint(doc["kind"])))
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		return nil
	}

	switch kind {
	case "deployment", "statefulset", "daemonset", "job":
		template := ensureMap(spec, "template")
		return ensureMap(template, "spec")
	case "cronjob":
		jobTemplate := ensureMap(spec, "jobTemplate")
		jobSpec := ensureMap(jobTemplate, "spec")
		template := ensureMap(jobSpec, "template")
		return ensureMap(template, "spec")
	default:
		return nil
	}
}

func ensureMap(root map[string]any, key string) map[string]any {
	if existing, ok := root[key].(map[string]any); ok && existing != nil {
		return existing
	}
	created := map[string]any{}
	root[key] = created
	return created
}

func randomHex(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}
