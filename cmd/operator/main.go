package main

import (
	"flag"
	"os"

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

	if err = (&operator.MCPServerReconciler{
		Client:              mgr.GetClient(),
		Scheme:              mgr.GetScheme(),
		DefaultIngressHost:  os.Getenv("MCP_DEFAULT_INGRESS_HOST"),
		ProvisionedRegistry: registryConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "MCPServer")
		os.Exit(1)
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
