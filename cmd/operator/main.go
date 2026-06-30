package main

import (
	"flag"
	"os"
	"strconv"
	"strings"

	_ "go.uber.org/automaxprocs" // align GOMAXPROCS with container CPU quota
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	mcpv1alpha1 "mcp-runtime/api/v1alpha1"
	"mcp-runtime/internal/operator"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(mcpv1alpha1.AddToScheme(scheme))
}

func main() {
	cfg, err := parseConfig(flag.CommandLine, os.Args[1:])
	if err != nil {
		setupLog.Error(err, "failed to parse flags")
		os.Exit(1)
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&cfg.zapOptions)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), newManagerOptions(cfg))
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Build registry config from environment variables
	registryConfig := registryConfigFromEnv(os.Getenv)
	if registryConfig != nil {
		setupLog.Info("Provisioned registry configured", "url", registryConfig.URL)
	}
	ingressReadinessMode, ingressReadinessModeValid := ingressReadinessModeFromEnv(os.Getenv)
	if !ingressReadinessModeValid {
		setupLog.Info("Invalid MCP_INGRESS_READINESS_MODE; defaulting to strict", "value", os.Getenv("MCP_INGRESS_READINESS_MODE"))
	}

	if err = (&operator.MCPServerReconciler{
		Client:                           mgr.GetClient(),
		Scheme:                           mgr.GetScheme(),
		DefaultIngressHost:               os.Getenv("MCP_DEFAULT_INGRESS_HOST"),
		DefaultIngressEntryPoints:        strings.TrimSpace(os.Getenv("MCP_DEFAULT_INGRESS_ENTRYPOINTS")),
		DefaultIngressTLS:                boolFromEnv(os.Getenv("MCP_DEFAULT_INGRESS_TLS")),
		DefaultIngressTLSSecret:          strings.TrimSpace(os.Getenv("MCP_DEFAULT_INGRESS_TLS_SECRET")),
		DefaultIngressTLSSecretNamespace: strings.TrimSpace(os.Getenv("MCP_DEFAULT_INGRESS_TLS_SECRET_NAMESPACE")),
		IngressReadinessMode:             ingressReadinessMode,
		ProvisionedRegistry:              registryConfig,
		GatewayProxyImage:                gatewayProxyImageFromEnv(os.Getenv),
		GatewayOTLPEndpoint:              gatewayOTLPEndpointFromEnv(os.Getenv),
		DefaultAnalyticsIngestURL:        analyticsIngestURLFromEnv(os.Getenv),
		ClusterName:                      clusterNameFromEnv(os.Getenv),
		MTLSClusterIssuer:                strings.TrimSpace(os.Getenv("MCP_MTLS_CLUSTER_ISSUER")),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "MCPServer")
		os.Exit(1)
	}

	if webhooksEnabledFromEnv(os.Getenv) {
		mcpServerWebhookOptions := mcpv1alpha1.MCPServerDefaultOptions{
			DefaultIngressHost:        os.Getenv("MCP_DEFAULT_INGRESS_HOST"),
			DefaultAnalyticsIngestURL: analyticsIngestURLFromEnv(os.Getenv),
		}
		if err := (&mcpv1alpha1.MCPServer{}).SetupWebhookWithManagerWithOptions(mgr, mcpServerWebhookOptions); err != nil {
			setupLog.Error(err, "unable to create webhook")
			os.Exit(1)
		}
		for _, resource := range []interface {
			SetupWebhookWithManager(ctrl.Manager) error
		}{
			&mcpv1alpha1.MCPAccessGrant{},
			&mcpv1alpha1.MCPAgentSession{},
		} {
			if err := resource.SetupWebhookWithManager(mgr); err != nil {
				setupLog.Error(err, "unable to create webhook")
				os.Exit(1)
			}
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

type operatorConfig struct {
	metricsAddr          string
	probeAddr            string
	enableLeaderElection bool
	zapOptions           zap.Options
}

func parseConfig(fs *flag.FlagSet, args []string) (*operatorConfig, error) {
	cfg := operatorConfig{
		zapOptions: zap.Options{
			Development: true,
		},
	}

	fs.StringVar(&cfg.metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	fs.StringVar(&cfg.probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	fs.BoolVar(&cfg.enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")
	cfg.zapOptions.BindFlags(fs)

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func newManagerOptions(cfg *operatorConfig) ctrl.Options {
	return ctrl.Options{
		Scheme:                 scheme,
		Metrics:                server.Options{BindAddress: cfg.metricsAddr},
		HealthProbeBindAddress: cfg.probeAddr,
		LeaderElection:         cfg.enableLeaderElection,
		LeaderElectionID:       "mcp-runtime-operator.mcpruntime.org",
	}
}

func registryConfigFromEnv(getenv func(string) string) *operator.RegistryConfig {
	url := getenv("PROVISIONED_REGISTRY_URL")
	if url == "" {
		return nil
	}

	return &operator.RegistryConfig{
		URL:        url,
		Username:   getenv("PROVISIONED_REGISTRY_USERNAME"),
		Password:   getenv("PROVISIONED_REGISTRY_PASSWORD"),
		SecretName: getenv("PROVISIONED_REGISTRY_SECRET_NAME"),
	}
}

func gatewayProxyImageFromEnv(getenv func(string) string) string {
	return getenv("MCP_GATEWAY_PROXY_IMAGE")
}

func gatewayOTLPEndpointFromEnv(getenv func(string) string) string {
	return getenv("MCP_GATEWAY_OTEL_EXPORTER_OTLP_ENDPOINT")
}

func analyticsIngestURLFromEnv(getenv func(string) string) string {
	if value := getenv("MCP_SENTINEL_INGEST_URL"); value != "" {
		return value
	}
	return getenv("MCP_ANALYTICS_INGEST_URL")
}

func clusterNameFromEnv(getenv func(string) string) string {
	if value := getenv("MCP_CLUSTER_NAME"); value != "" {
		return value
	}
	return "local"
}

func ingressReadinessModeFromEnv(getenv func(string) string) (string, bool) {
	return operator.NormalizeIngressReadinessMode(getenv("MCP_INGRESS_READINESS_MODE"))
}

func webhooksEnabledFromEnv(getenv func(string) string) bool {
	value := getenv("MCP_ENABLE_WEBHOOKS")
	return value == "true" || value == "1"
}

func boolFromEnv(value string) bool {
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	return err == nil && parsed
}
