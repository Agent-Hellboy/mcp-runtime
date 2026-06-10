package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	_ "go.uber.org/automaxprocs" // align GOMAXPROCS with container CPU quota

	"mcp-runtime/pkg/serviceutil"
)

func main() {
	port := serviceutil.EnvOr("PORT", "8091")
	upstream := serviceutil.EnvOr("UPSTREAM_URL", "http://127.0.0.1:8090")
	analyticsURL := strings.TrimSpace(os.Getenv("ANALYTICS_INGEST_URL"))
	apiKey := strings.TrimSpace(os.Getenv("ANALYTICS_API_KEY"))
	source := serviceutil.EnvOr("ANALYTICS_SOURCE", "mcp-gateway")
	eventType := serviceutil.EnvOr("ANALYTICS_EVENT_TYPE", "mcp.request")
	stripPrefix := strings.TrimSpace(os.Getenv("STRIP_PREFIX"))
	externalBaseURL, err := parseExternalBaseURL(strings.TrimSpace(os.Getenv("EXTERNAL_BASE_URL")))
	if err != nil {
		log.Fatalf("invalid EXTERNAL_BASE_URL: %v", err)
	}

	target, err := url.Parse(upstream)
	if err != nil {
		log.Fatalf("invalid UPSTREAM_URL: %v", err)
	}

	proxy := newUpstreamReverseProxy(target)
	proxy.Transport = otelhttp.NewTransport(http.DefaultTransport)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("gateway error: %v", err)
		http.Error(w, "upstream error", http.StatusBadGateway)
	}

	analyticsTransport := otelhttp.NewTransport(&http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	})

	sharedClient := &http.Client{
		Timeout:   3 * time.Second,
		Transport: analyticsTransport,
	}

	srv := &gatewayServer{
		proxy:                 proxy,
		analyticsURL:          analyticsURL,
		apiKey:                apiKey,
		source:                source,
		eventType:             eventType,
		stripPrefix:           stripPrefix,
		externalBaseURL:       externalBaseURL,
		httpClient:            sharedClient,
		policyFile:            strings.TrimSpace(os.Getenv("POLICY_FILE")),
		serverName:            strings.TrimSpace(os.Getenv("MCP_SERVER_NAME")),
		serverNamespace:       strings.TrimSpace(os.Getenv("MCP_SERVER_NAMESPACE")),
		clusterName:           strings.TrimSpace(os.Getenv("MCP_CLUSTER_NAME")),
		defaultHumanHeader:    serviceutil.EnvOr("HUMAN_ID_HEADER", defaultHumanHeader),
		defaultAgentHeader:    serviceutil.EnvOr("AGENT_ID_HEADER", defaultAgentHeader),
		defaultTeamHeader:     serviceutil.EnvOr("TEAM_ID_HEADER", defaultTeamHeader),
		defaultSessionHeader:  serviceutil.EnvOr("SESSION_ID_HEADER", defaultSessionHeader),
		defaultPolicyMode:     serviceutil.EnvOr("POLICY_MODE", defaultPolicyMode),
		defaultPolicyDecision: serviceutil.EnvOr("POLICY_DEFAULT_DECISION", defaultPolicyDecision),
		defaultPolicyVersion:  serviceutil.EnvOr("POLICY_VERSION", defaultPolicyVersion),
		oauthProviders:        map[string]*oauthProvider{},
	}
	if err := srv.startPolicyCache(); err != nil {
		log.Fatalf("initial policy load failed: %v", err)
	}

	mux := http.NewServeMux()
	// Liveness: always OK while the process is serving.
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	// Readiness: OK only after a valid policy snapshot has been activated.
	mux.HandleFunc("/ready", srv.handleReady)
	// Sanitized applied-policy metadata (schema version, revision, reload state).
	mux.HandleFunc("/config/status", srv.handleConfigStatus)
	// Prometheus metrics, including policy reload observability.
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/", srv.handleGateway)

	shutdown, err := initTracer("mcp-gateway")
	if err != nil {
		log.Printf("otel init failed: %v", err)
	} else {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = shutdown(ctx)
		}()
	}

	log.Printf("mcp-gateway listening on :%s -> %s", port, upstream)
	handler := otelhttp.NewHandler(mux, "http.server")
	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- httpServer.ListenAndServe()
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErr:
		srv.stopAnalyticsDispatcher()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			srv.stopAnalyticsDispatcher()
			log.Fatalf("server shutdown failed: %v", err)
		}
		srv.stopAnalyticsDispatcher()
	}
}

// initTracer initializes OpenTelemetry tracing for the service.
func initTracer(serviceName string) (func(context.Context) error, error) {
	return serviceutil.InitTracer(serviceName)
}
