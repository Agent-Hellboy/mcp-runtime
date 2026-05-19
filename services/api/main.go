/*
This is the API server for the MCP Sentinel project.

# Recent tool calls
GET /api/events?limit=100

# Total MCP activity
GET /api/stats

# Source usage statistics
GET /api/sources

# Event type statistics
GET /api/event-types

# Filter events by source/type or audit fields
GET /api/events/filter?trace_id=<trace>&source=mcp-server&event_type=tool.call&server=payments&team_id=team-acme&decision=deny&agent_id=agent-42&limit=50

# Monitor API health
GET /metrics

# Health check
GET /health
*/
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	chdriver "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/MicahParks/keyfunc"
	"github.com/golang-jwt/jwt/v4"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	clickhousepkg "mcp-runtime/pkg/clickhouse"
	"mcp-runtime/pkg/serviceutil"
	"mcp-sentinel-api/internal/runtimeapi"
)

type analyticsServerUsage struct {
	Server       string    `json:"server"`
	Namespace    string    `json:"namespace"`
	TeamID       string    `json:"team_id,omitempty"`
	Events       uint64    `json:"events"`
	Allowed      uint64    `json:"allowed"`
	Denied       uint64    `json:"denied"`
	UniqueHumans uint64    `json:"unique_humans"`
	UniqueAgents uint64    `json:"unique_agents"`
	LastSeen     time.Time `json:"last_seen"`
}

type analyticsActorUsage struct {
	HumanID       string    `json:"human_id"`
	AgentID       string    `json:"agent_id"`
	Events        uint64    `json:"events"`
	UniqueServers uint64    `json:"unique_servers"`
	UniqueTools   uint64    `json:"unique_tools"`
	Denied        uint64    `json:"denied"`
	LastSeen      time.Time `json:"last_seen"`
}

type analyticsToolUsage struct {
	Server   string    `json:"server"`
	ToolName string    `json:"tool_name"`
	Events   uint64    `json:"events"`
	Denied   uint64    `json:"denied"`
	LastSeen time.Time `json:"last_seen"`
}

type analyticsDecisionUsage struct {
	Decision string `json:"decision"`
	Events   uint64 `json:"events"`
}

type analyticsTimePoint struct {
	Bucket  time.Time `json:"bucket"`
	Events  uint64    `json:"events"`
	Allowed uint64    `json:"allowed"`
	Denied  uint64    `json:"denied"`
}

type analyticsRecentActivity struct {
	Timestamp time.Time `json:"timestamp"`
	Server    string    `json:"server,omitempty"`
	Namespace string    `json:"namespace,omitempty"`
	TeamID    string    `json:"team_id,omitempty"`
	HumanID   string    `json:"human_id,omitempty"`
	AgentID   string    `json:"agent_id,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	Decision  string    `json:"decision,omitempty"`
	ToolName  string    `json:"tool_name,omitempty"`
	EventType string    `json:"event_type,omitempty"`
}

type analyticsTotals struct {
	Events         uint64 `json:"events"`
	Allowed        uint64 `json:"allowed"`
	Denied         uint64 `json:"denied"`
	UniqueServers  uint64 `json:"unique_servers"`
	UniqueHumans   uint64 `json:"unique_humans"`
	UniqueAgents   uint64 `json:"unique_agents"`
	UniqueSessions uint64 `json:"unique_sessions"`
}

type analyticsUsageResponse struct {
	Totals     analyticsTotals           `json:"totals"`
	Servers    []analyticsServerUsage    `json:"servers"`
	Actors     []analyticsActorUsage     `json:"actors"`
	Tools      []analyticsToolUsage      `json:"tools"`
	Decisions  []analyticsDecisionUsage  `json:"decisions"`
	Series     []analyticsTimePoint      `json:"series,omitempty"`
	Recent     []analyticsRecentActivity `json:"recent,omitempty"`
	WindowDays int                       `json:"window_days"`
	Filters    analyticsUsageFilters     `json:"filters,omitempty"`
}

type analyticsUsageFilters struct {
	Namespaces []string `json:"namespaces,omitempty"`
	TeamIDs    []string `json:"team_ids,omitempty"`
	Server     string   `json:"server,omitempty"`
	Decision   string   `json:"decision,omitempty"`
	ToolName   string   `json:"tool_name,omitempty"`
}

type apiServer struct {
	db           chdriver.Conn
	dbName       string
	events       *clickhousepkg.Client
	apiKeys      map[string]struct{}
	adminAPIKeys map[string]struct{}
	adminUsers   map[string]struct{}
	jwks         *keyfunc.JWKS
	oidcIssuer   string
	oidcAudience string
	userKeys     userAPIKeyStore
	registryAuth registryCredentialAuthenticator
	platform     *platformStore
	runtime      *runtimeapi.RuntimeServer
	runtimeInit  string
}

const (
	analyticsDefaultWindowDays = 30
	analyticsMaxWindowDays     = 365
	analyticsTeamIDExpression  = "JSONExtractString(payload, 'team_id')"

	defaultPlatformStoreStartupTimeout = 90 * time.Second
)

type platformStoreOpener func(context.Context, string, []byte) (*platformStore, error)

var (
	platformStoreConnectAttemptTimeout = 10 * time.Second
	platformStoreConnectRetryInterval  = 2 * time.Second
)

// main initializes and starts the MCP Sentinel API server.
// It sets up database connections, configures authentication, initializes tracing,
// sets up HTTP routes, and starts the server on the configured port.
func main() {
	port := envOr("PORT", "8080")
	metricsPort := envOr("METRICS_PORT", "9090")
	clickhouseAddr := envOr("CLICKHOUSE_ADDR", "clickhouse:9000")
	dbName := envOr("CLICKHOUSE_DB", "mcp")
	if bootstrapOnly, ok := boolEnv("PLATFORM_ADMIN_BOOTSTRAP_ONLY"); ok && bootstrapOnly {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := runPlatformAdminBootstrap(ctx); err != nil {
			log.Fatalf("platform admin bootstrap failed: %v", err)
		}
		log.Printf("platform admin bootstrap complete")
		return
	}
	if err := clickhousepkg.ValidateDBName(dbName); err != nil {
		log.Fatalf("invalid CLICKHOUSE_DB: %v", err)
	}

	apiKeys := map[string]struct{}{}
	for _, key := range strings.Split(envOr("API_KEYS", ""), ",") {
		key = strings.TrimSpace(key)
		if key != "" {
			apiKeys[key] = struct{}{}
		}
	}
	adminAPIKeys := splitCSVSet(envOr("ADMIN_API_KEYS", ""))
	adminUsers := splitCSVSet(envOr("ADMIN_USERS", ""))
	for entry := range adminUsers {
		normalized := strings.ToLower(strings.TrimSpace(entry))
		if normalized != "" {
			adminUsers[normalized] = struct{}{}
		}
	}
	if len(adminAPIKeys) > 0 {
		demoted := make([]string, 0, len(apiKeys))
		for key := range apiKeys {
			if _, ok := adminAPIKeys[key]; !ok {
				demoted = append(demoted, maskCredentialForLog(key))
			}
		}
		if len(demoted) > 0 {
			log.Printf("warning: ADMIN_API_KEYS is set; %d API_KEYS value(s) not listed in ADMIN_API_KEYS will authenticate as role=user (demoted_keys=%v)", len(demoted), demoted)
		}
	}

	oidcIssuer := strings.TrimSpace(os.Getenv("OIDC_ISSUER"))
	oidcAudience := strings.TrimSpace(os.Getenv("OIDC_AUDIENCE"))
	jwksURL := strings.TrimSpace(os.Getenv("OIDC_JWKS_URL"))
	if (oidcIssuer != "" || oidcAudience != "") && jwksURL == "" {
		log.Fatal("OIDC_JWKS_URL is required when OIDC_ISSUER or OIDC_AUDIENCE is configured")
	}
	if jwksURL != "" && (oidcIssuer == "" || oidcAudience == "") {
		log.Fatal("OIDC_ISSUER and OIDC_AUDIENCE are required when OIDC_JWKS_URL is configured")
	}
	jwks := (*keyfunc.JWKS)(nil)
	if jwksURL != "" {
		var err error
		jwks, err = keyfunc.Get(jwksURL, keyfunc.Options{RefreshInterval: 10 * time.Minute})
		if err != nil {
			log.Fatalf("failed to load JWKS: %v", err)
		}
	}

	conn, err := chdriver.Open(&chdriver.Options{
		Addr: []string{clickhouseAddr},
		Auth: chdriver.Auth{
			Database: dbName,
		},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Fatalf("failed to connect to clickhouse: %v", err)
	}

	server := &apiServer{
		db:           conn,
		dbName:       dbName,
		events:       &clickhousepkg.Client{Conn: conn, DBName: dbName},
		apiKeys:      apiKeys,
		adminAPIKeys: adminAPIKeys,
		adminUsers:   adminUsers,
		jwks:         jwks,
		oidcIssuer:   oidcIssuer,
		oidcAudience: oidcAudience,
	}
	var store *platformStore
	if dsn := platformDSNFromEnv(); dsn != "" {
		startupTimeout := serviceutil.EnvDuration("PLATFORM_DB_STARTUP_TIMEOUT", defaultPlatformStoreStartupTimeout)
		ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
		defer cancel()
		secretValue := strings.TrimSpace(os.Getenv("PLATFORM_JWT_SECRET"))
		if secretValue == "" {
			log.Fatal("PLATFORM_JWT_SECRET is required when POSTGRES_DSN or DATABASE_URL is configured")
		}
		var err error
		store, err = openPlatformStoreWithRetry(ctx, dsn, []byte(secretValue), newPlatformStore)
		if err != nil {
			log.Fatalf("failed to initialize platform identity database: %v", err)
		}
		seedCtx, seedCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer seedCancel()
		if err := seedPlatformAdminFromEnv(seedCtx, store); err != nil {
			log.Fatalf("failed to seed platform admin: %v", err)
		}
		if err := seedPlatformDevUsersFromEnv(seedCtx, store); err != nil {
			log.Fatalf("failed to seed platform dev users: %v", err)
		}
		server.platform = store
		server.userKeys = store
		server.registryAuth = store
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		if strings.TrimSpace(server.runtimeInit) != "" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"ok":                  false,
				"runtime_initialized": false,
				"runtime_error":       server.runtimeInit,
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":                  true,
			"runtime_initialized": true,
		})
	})
	mux.HandleFunc("/api/registry/authz", server.handleRegistryAuthz)
	mux.HandleFunc("/api/auth/login", server.handleLogin)
	mux.HandleFunc("/api/auth/oidc", server.handleOIDCLogin)
	mux.HandleFunc("/api/auth/signup", server.handleSignup)
	mux.Handle("/api/events", server.auth(server.requireRole(roleAdmin, http.HandlerFunc(server.handleEvents))))
	mux.Handle("/api/stats", server.auth(server.requireRole(roleAdmin, http.HandlerFunc(server.handleStats))))
	mux.Handle("/api/sources", server.auth(server.requireRole(roleAdmin, http.HandlerFunc(server.handleSources))))
	mux.Handle("/api/event-types", server.auth(server.requireRole(roleAdmin, http.HandlerFunc(server.handleEventTypes))))
	mux.Handle("/api/analytics/usage", server.auth(server.requireRole(roleAdmin, http.HandlerFunc(server.handleAnalyticsUsage))))
	mux.Handle("/api/events/filter", server.auth(server.requireRole(roleAdmin, http.HandlerFunc(server.handleEventsFilter))))
	mux.Handle("/api/auth/me", server.auth(http.HandlerFunc(server.handleAuthMe)))
	mux.Handle("/api/user/analytics/usage", server.auth(http.HandlerFunc(server.handleUserAnalyticsUsage)))
	mux.Handle("/api/user/registry-credentials", server.auth(http.HandlerFunc(server.handleRegistryCredentials)))
	mux.Handle("/api/user/registry-credentials/", server.auth(http.HandlerFunc(server.handleRegistryCredentialItem)))
	mux.Handle("/api/user/activity/image-publish", server.auth(http.HandlerFunc(server.handleUserImagePublishActivity)))

	// Initialize and register runtime server with Kubernetes support
	runtimeServer, err := runtimeapi.NewRuntimeServer(conn, dbName, apiKeys, server.platform)
	if err != nil {
		server.runtimeInit = err.Error()
		log.Printf("ERROR: runtime server initialization failed: %v", err)
	} else {
		server.runtime = runtimeServer
		runtimeServer.SetAuditWriter(server.platform)
		if server.userKeys == nil {
			server.userKeys = runtimeServer
		}
		// Register all runtime endpoints with auth
		mux.Handle("/api/dashboard/summary", server.auth(server.requireRole(roleAdmin, http.HandlerFunc(runtimeServer.HandleDashboardSummary))))
		mux.Handle("/api/runtime/servers", server.authOrPublicCatalog(http.HandlerFunc(runtimeServer.HandleRuntimeServers)))
		mux.Handle("/api/runtime/servers/", server.auth(http.HandlerFunc(runtimeServer.HandleRuntimeServerItem)))
		mux.Handle("/api/runtime/server-events", server.auth(http.HandlerFunc(runtimeServer.HandleRuntimeServerEvents)))
		mux.Handle("/api/runtime/observability/links", server.auth(http.HandlerFunc(runtimeServer.HandleRuntimeObservabilityLinks)))
		mux.Handle("/api/runtime/observability/grafana/dashboard", server.auth(http.HandlerFunc(runtimeServer.HandleRuntimeObservabilityGrafanaDashboard)))
		mux.Handle("/api/runtime/observability/prometheus/query", server.auth(http.HandlerFunc(runtimeServer.HandleRuntimeObservabilityPrometheusQuery)))
		mux.Handle("/api/runtime/teams", server.auth(http.HandlerFunc(runtimeServer.HandleRuntimeTeams)))
		mux.Handle("/api/runtime/teams/", server.auth(http.HandlerFunc(runtimeServer.HandleRuntimeTeamItemPath)))
		mux.Handle("/api/runtime/namespaces", server.auth(http.HandlerFunc(runtimeServer.HandleRuntimeNamespaces)))
		mux.Handle("/api/runtime/namespaces/", server.auth(http.HandlerFunc(runtimeServer.HandleRuntimeNamespaceItem)))
		mux.Handle("/api/deployments", server.auth(http.HandlerFunc(runtimeServer.HandleDeployments)))
		mux.Handle("/api/deployments/", server.auth(http.HandlerFunc(runtimeServer.HandleDeploymentItem)))
		mux.Handle("/api/admin/namespaces", server.auth(server.requireRole(roleAdmin, http.HandlerFunc(server.handleAdminNamespaces))))
		mux.Handle("/api/admin/audit", server.auth(server.requireRole(roleAdmin, http.HandlerFunc(server.handleAdminAudit))))
		mux.Handle("/api/admin/operations", server.auth(server.requireRole(roleAdmin, http.HandlerFunc(server.handleAdminOperations))))
		mux.Handle("/api/admin/deployments", server.auth(server.requireRole(roleAdmin, http.HandlerFunc(runtimeServer.HandleAdminDeployments))))
		mux.Handle("/api/runtime/grants", server.auth(http.HandlerFunc(runtimeServer.HandleRuntimeGrants)))
		mux.Handle("/api/runtime/sessions", server.auth(http.HandlerFunc(runtimeServer.HandleRuntimeSessions)))
		mux.Handle("/api/runtime/adapter/sessions", server.auth(http.HandlerFunc(runtimeServer.HandleAdapterSession)))
		mux.Handle("/api/runtime/components", server.auth(http.HandlerFunc(runtimeServer.HandleRuntimeComponents)))
		mux.Handle("/api/runtime/policy", server.auth(http.HandlerFunc(runtimeServer.HandleRuntimePolicy)))
		mux.Handle("/api/runtime/actions/restart", server.auth(server.requireRole(roleAdmin, http.HandlerFunc(runtimeServer.HandleActionRestart))))
		// Grant item (POST /api/runtime/grants/{namespace}/{name}/disable|enable, DELETE /api/runtime/grants/{namespace}/{name})
		mux.Handle("/api/runtime/grants/", server.auth(http.HandlerFunc(runtimeServer.HandleGrantItemPath)))
		// Session item (POST /api/runtime/sessions/{namespace}/{name}/revoke|unrevoke, DELETE /api/runtime/sessions/{namespace}/{name})
		mux.Handle("/api/runtime/sessions/", server.auth(http.HandlerFunc(runtimeServer.HandleSessionItemPath)))
		// User-scoped API key lifecycle.
		mux.Handle("/api/user/api-keys", server.auth(http.HandlerFunc(server.handleUserAPIKeys)))
		mux.Handle("/api/user/api-keys/", server.auth(http.HandlerFunc(server.handleUserAPIKeyItem)))
	}

	shutdown, err := initTracer("mcp-sentinel-api")
	if err != nil {
		log.Printf("otel init failed: %v", err)
	} else {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = shutdown(ctx)
		}()
	}

	metricsShutdown, metricsErrs := serviceutil.StartMetricsServer(metricsPort)
	log.Printf("mcp-sentinel-api listening on :%s", port)
	handler := otelhttp.NewHandler(logRequests(mux), "http.server")
	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownSignals, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	serverErrs := make(chan error, 2)
	go func() {
		if err, ok := <-metricsErrs; ok {
			serverErrs <- fmt.Errorf("metrics server failed: %w", err)
		}
	}()
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrs <- fmt.Errorf("api server failed: %w", err)
		}
	}()

	select {
	case <-shutdownSignals.Done():
		log.Printf("shutdown signal received")
	case err := <-serverErrs:
		log.Printf("%v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("api shutdown error: %v", err)
	}
	if err := metricsShutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("metrics shutdown error: %v", err)
	}
	if store != nil {
		store.Close()
	}
}

func openPlatformStoreWithRetry(ctx context.Context, dsn string, jwtSecret []byte, open platformStoreOpener) (*platformStore, error) {
	if open == nil {
		open = newPlatformStore
	}
	var lastErr error
	for attempt := 1; ; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, platformStoreConnectAttemptTimeout)
		store, err := open(attemptCtx, dsn, jwtSecret)
		cancel()
		if err == nil {
			if attempt > 1 {
				log.Printf("platform identity database initialized after %d attempt(s)", attempt)
			}
			return store, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, lastErr
		}
		log.Printf("platform identity database not ready (attempt %d): %v", attempt, err)
		timer := time.NewTimer(platformStoreConnectRetryInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil, lastErr
		case <-timer.C:
		}
	}
}

// initTracer initializes OpenTelemetry tracing for the service.
// It configures OTLP HTTP exporter and sets up the tracer provider.
// Returns a shutdown function to clean up resources and any initialization error.
// If no OTEL_EXPORTER_OTLP_ENDPOINT is configured, returns a no-op shutdown function.
func initTracer(serviceName string) (func(context.Context) error, error) {
	return serviceutil.InitTracer(serviceName)
}

// handleEvents handles GET /api/events requests.
// It queries the ClickHouse database for MCP events with optional limit.
// Returns events in descending timestamp order (newest first).
func (s *apiServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	limit := clampInt(queryInt(r, "limit", 100), 1, 1000)

	events, err := s.events.QueryEvents(r.Context(), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query_failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

// handleStats handles GET /api/stats requests.
// It queries the ClickHouse database for total event count.
// Returns the total number of MCP events in the system.
func (s *apiServer) handleStats(w http.ResponseWriter, r *http.Request) {
	count, err := s.events.QueryStats(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query_failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"events_total": count})
}

// handleSources handles GET /api/sources requests.
// It queries the ClickHouse database for event counts grouped by source.
// Returns a list of sources with their event counts, ordered by count descending.
func (s *apiServer) handleSources(w http.ResponseWriter, r *http.Request) {
	sources, err := s.events.QuerySources(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query_failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"sources": sources})
}

// handleEventTypes handles GET /api/event-types requests.
// It queries the ClickHouse database for event counts grouped by event type.
// Returns a list of event types with their counts, ordered by count descending.
func (s *apiServer) handleEventTypes(w http.ResponseWriter, r *http.Request) {
	eventTypes, err := s.events.QueryEventTypes(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query_failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"event_types": eventTypes})
}

func (s *apiServer) handleAnalyticsUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	limit := clampInt(queryInt(r, "limit", 10), 1, 50)
	windowDays := clampInt(queryInt(r, "window_days", analyticsDefaultWindowDays), 1, analyticsMaxWindowDays)
	scope := analyticsScopeFromRequest(r, windowDays, limit)
	applyAdminAnalyticsScopeFilters(r, &scope)

	response, err := s.queryAnalyticsUsage(r.Context(), scope)
	if err != nil {
		log.Printf("analytics usage query failed window_days=%d limit=%d since=%s filters=%+v err=%v", scope.WindowDays, scope.Limit, scope.Since.Format(time.RFC3339), scope.Filters(), err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query_failed"})
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *apiServer) handleUserAnalyticsUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	limit := clampInt(queryInt(r, "limit", 10), 1, 50)
	windowDays := clampInt(queryInt(r, "window_days", analyticsDefaultWindowDays), 1, analyticsMaxWindowDays)
	scope := analyticsScopeFromRequest(r, windowDays, limit)

	requestedNamespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
	if p.Role == roleAdmin {
		applyAdminAnalyticsScopeFilters(r, &scope)
	} else {
		var principalScope analyticsPrincipalScope
		var allowed bool
		if requestedNamespace != "" {
			principalScope, allowed = analyticsPrincipalScopeForNamespace(p, requestedNamespace)
			if !allowed {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden namespace"})
				return
			}
		} else {
			principalScope = analyticsPrincipalOwnedScope(p)
			allowed = len(principalScope.Namespaces) > 0 || len(principalScope.TeamIDs) > 0
		}
		if !allowed {
			writeJSON(w, http.StatusOK, emptyAnalyticsUsageResponse(scope))
			return
		}
		scope.Namespaces = principalScope.Namespaces
		scope.TeamIDs = principalScope.TeamIDs
	}

	response, err := s.queryAnalyticsUsage(r.Context(), scope)
	if err != nil {
		log.Printf("user analytics usage query failed window_days=%d limit=%d since=%s filters=%+v err=%v", scope.WindowDays, scope.Limit, scope.Since.Format(time.RFC3339), scope.Filters(), err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query_failed"})
		return
	}

	writeJSON(w, http.StatusOK, response)
}

type analyticsQueryScope struct {
	Since      time.Time
	WindowDays int
	Limit      int
	Namespaces []string
	TeamIDs    []string
	Server     string
	Decision   string
	ToolName   string
}

type analyticsPrincipalScope struct {
	Namespaces []string
	TeamIDs    []string
}

func analyticsScopeFromRequest(r *http.Request, windowDays, limit int) analyticsQueryScope {
	decision := strings.TrimSpace(r.URL.Query().Get("decision"))
	if decision == "" {
		decision = strings.TrimSpace(r.URL.Query().Get("status"))
	}
	if decision == "" {
		decision = strings.TrimSpace(r.URL.Query().Get("outcome"))
	}
	toolName := strings.TrimSpace(r.URL.Query().Get("tool_name"))
	if toolName == "" {
		toolName = strings.TrimSpace(r.URL.Query().Get("tool"))
	}
	return analyticsQueryScope{
		Since:      time.Now().AddDate(0, 0, -windowDays),
		WindowDays: windowDays,
		Limit:      limit,
		Server:     strings.TrimSpace(r.URL.Query().Get("server")),
		Decision:   decision,
		ToolName:   toolName,
	}
}

func applyAdminAnalyticsScopeFilters(r *http.Request, scope *analyticsQueryScope) {
	namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
	if namespace != "" {
		scope.Namespaces = []string{namespace}
	}
	teamID := strings.TrimSpace(r.URL.Query().Get("team_id"))
	if teamID != "" {
		scope.TeamIDs = []string{teamID}
	}
}

func analyticsPrincipalOwnedScope(p principal) analyticsPrincipalScope {
	var scope analyticsPrincipalScope
	addNamespace := func(namespace string) {
		namespace = strings.TrimSpace(namespace)
		if namespace == "" || namespace == sharedCatalogNamespace {
			return
		}
		scope.Namespaces = append(scope.Namespaces, namespace)
	}
	addNamespace(p.Namespace)
	for _, team := range p.Teams {
		addNamespace(team.Namespace)
		if teamID := strings.TrimSpace(team.ID); teamID != "" {
			scope.TeamIDs = append(scope.TeamIDs, teamID)
		}
	}
	for _, namespace := range p.AllowedNamespaces {
		addNamespace(namespace)
	}
	scope.Namespaces = dedupeAnalyticsStrings(scope.Namespaces)
	scope.TeamIDs = dedupeAnalyticsStrings(scope.TeamIDs)
	return scope
}

func analyticsPrincipalScopeForNamespace(p principal, namespace string) (analyticsPrincipalScope, bool) {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" || namespace == sharedCatalogNamespace {
		return analyticsPrincipalScope{}, false
	}
	if strings.TrimSpace(p.Namespace) == namespace {
		return analyticsPrincipalScope{Namespaces: []string{namespace}}, true
	}
	for _, team := range p.Teams {
		if strings.TrimSpace(team.Namespace) == namespace {
			scope := analyticsPrincipalScope{Namespaces: []string{namespace}}
			if teamID := strings.TrimSpace(team.ID); teamID != "" {
				scope.TeamIDs = []string{teamID}
			}
			return scope, true
		}
	}
	for _, allowed := range p.AllowedNamespaces {
		if strings.TrimSpace(allowed) == namespace {
			return analyticsPrincipalScope{Namespaces: []string{namespace}}, true
		}
	}
	return analyticsPrincipalScope{}, false
}

func emptyAnalyticsUsageResponse(scope analyticsQueryScope) analyticsUsageResponse {
	return analyticsUsageResponse{
		Servers:    []analyticsServerUsage{},
		Actors:     []analyticsActorUsage{},
		Tools:      []analyticsToolUsage{},
		Decisions:  []analyticsDecisionUsage{},
		Series:     []analyticsTimePoint{},
		Recent:     []analyticsRecentActivity{},
		WindowDays: scope.WindowDays,
		Filters:    scope.Filters(),
	}
}

func (scope analyticsQueryScope) Filters() analyticsUsageFilters {
	return analyticsUsageFilters{
		Namespaces: dedupeAnalyticsStrings(scope.Namespaces),
		TeamIDs:    dedupeAnalyticsStrings(scope.TeamIDs),
		Server:     scope.Server,
		Decision:   scope.Decision,
		ToolName:   scope.ToolName,
	}
}

func (s *apiServer) queryAnalyticsUsage(parent context.Context, scope analyticsQueryScope) (analyticsUsageResponse, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	var (
		totals    analyticsTotals
		servers   []analyticsServerUsage
		actors    []analyticsActorUsage
		tools     []analyticsToolUsage
		decisions []analyticsDecisionUsage
		series    []analyticsTimePoint
		recent    []analyticsRecentActivity

		wg          sync.WaitGroup
		errOnce     sync.Once
		firstErr    error
		firstErrKey string
	)

	recordErr := func(key string, err error) {
		if err == nil {
			return
		}
		errOnce.Do(func() {
			firstErr = err
			firstErrKey = key
			cancel()
		})
	}

	wg.Add(7)
	go func() {
		defer wg.Done()
		var err error
		totals, err = s.queryAnalyticsTotals(ctx, scope)
		recordErr("totals", err)
	}()
	go func() {
		defer wg.Done()
		var err error
		servers, err = s.queryAnalyticsServers(ctx, scope)
		recordErr("servers", err)
	}()
	go func() {
		defer wg.Done()
		var err error
		actors, err = s.queryAnalyticsActors(ctx, scope)
		recordErr("actors", err)
	}()
	go func() {
		defer wg.Done()
		var err error
		tools, err = s.queryAnalyticsTools(ctx, scope)
		recordErr("tools", err)
	}()
	go func() {
		defer wg.Done()
		var err error
		decisions, err = s.queryAnalyticsDecisions(ctx, scope)
		recordErr("decisions", err)
	}()
	go func() {
		defer wg.Done()
		var err error
		series, err = s.queryAnalyticsSeries(ctx, scope)
		recordErr("series", err)
	}()
	go func() {
		defer wg.Done()
		var err error
		recent, err = s.queryAnalyticsRecent(ctx, scope)
		recordErr("recent", err)
	}()
	wg.Wait()

	if firstErr != nil {
		return analyticsUsageResponse{}, fmt.Errorf("%s: %w", firstErrKey, firstErr)
	}

	return analyticsUsageResponse{
		Totals:     totals,
		Servers:    servers,
		Actors:     actors,
		Tools:      tools,
		Decisions:  decisions,
		Series:     series,
		Recent:     recent,
		WindowDays: scope.WindowDays,
		Filters:    scope.Filters(),
	}, nil
}

func (s *apiServer) queryAnalyticsTotals(ctx context.Context, scope analyticsQueryScope) (analyticsTotals, error) {
	where, args := analyticsWhereClause(scope)
	query := "SELECT count(), countIf(decision = 'allow'), countIf(decision = 'deny'), uniqIf(server, server != ''), uniqIf(human_id, human_id != ''), uniqIf(agent_id, agent_id != ''), uniqIf(session_id, session_id != '') FROM " + s.dbName + ".events " + where
	var totals analyticsTotals
	err := s.db.QueryRow(ctx, query, args...).Scan(
		&totals.Events,
		&totals.Allowed,
		&totals.Denied,
		&totals.UniqueServers,
		&totals.UniqueHumans,
		&totals.UniqueAgents,
		&totals.UniqueSessions,
	)
	return totals, err
}

func (s *apiServer) queryAnalyticsServers(ctx context.Context, scope analyticsQueryScope) ([]analyticsServerUsage, error) {
	where, args := analyticsWhereClause(scope, "server != ''")
	query := analyticsServersQuery(s.dbName, where)
	args = append(args, scope.Limit)
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]analyticsServerUsage, 0, scope.Limit)
	for rows.Next() {
		var row analyticsServerUsage
		if err := rows.Scan(&row.Server, &row.Namespace, &row.TeamID, &row.Events, &row.Allowed, &row.Denied, &row.UniqueHumans, &row.UniqueAgents, &row.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func analyticsServersQuery(dbName, where string) string {
	return "SELECT server, namespace, " + analyticsTeamIDExpression + " AS team_id, count() AS events, countIf(decision = 'allow') AS allowed, countIf(decision = 'deny') AS denied, uniqIf(human_id, human_id != '') AS unique_humans, uniqIf(agent_id, agent_id != '') AS unique_agents, max(timestamp) AS last_seen FROM " + dbName + ".events " + where + " GROUP BY server, namespace, team_id ORDER BY events DESC LIMIT ?"
}

func (s *apiServer) queryAnalyticsActors(ctx context.Context, scope analyticsQueryScope) ([]analyticsActorUsage, error) {
	where, args := analyticsWhereClause(scope, "(human_id != '' OR agent_id != '')")
	query := "SELECT human_id, agent_id, count() AS events, uniqIf(server, server != '') AS unique_servers, uniqIf(tool_name, tool_name != '') AS unique_tools, countIf(decision = 'deny') AS denied, max(timestamp) AS last_seen FROM " + s.dbName + ".events " + where + " GROUP BY human_id, agent_id ORDER BY events DESC LIMIT ?"
	args = append(args, scope.Limit)
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]analyticsActorUsage, 0, scope.Limit)
	for rows.Next() {
		var row analyticsActorUsage
		if err := rows.Scan(&row.HumanID, &row.AgentID, &row.Events, &row.UniqueServers, &row.UniqueTools, &row.Denied, &row.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *apiServer) queryAnalyticsTools(ctx context.Context, scope analyticsQueryScope) ([]analyticsToolUsage, error) {
	where, args := analyticsWhereClause(scope, "tool_name != ''")
	query := "SELECT server, tool_name, count() AS events, countIf(decision = 'deny') AS denied, max(timestamp) AS last_seen FROM " + s.dbName + ".events " + where + " GROUP BY server, tool_name ORDER BY events DESC LIMIT ?"
	args = append(args, scope.Limit)
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]analyticsToolUsage, 0, scope.Limit)
	for rows.Next() {
		var row analyticsToolUsage
		if err := rows.Scan(&row.Server, &row.ToolName, &row.Events, &row.Denied, &row.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *apiServer) queryAnalyticsDecisions(ctx context.Context, scope analyticsQueryScope) ([]analyticsDecisionUsage, error) {
	where, args := analyticsWhereClause(scope)
	query := "SELECT if(decision = '', 'unknown', decision) AS decision_label, count() AS events FROM " + s.dbName + ".events " + where + " GROUP BY decision_label ORDER BY events DESC"
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]analyticsDecisionUsage, 0)
	for rows.Next() {
		var row analyticsDecisionUsage
		if err := rows.Scan(&row.Decision, &row.Events); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *apiServer) queryAnalyticsSeries(ctx context.Context, scope analyticsQueryScope) ([]analyticsTimePoint, error) {
	where, args := analyticsWhereClause(scope)
	query := "SELECT " + analyticsBucketExpression(scope.WindowDays) + " AS bucket, count() AS events, countIf(decision = 'allow') AS allowed, countIf(decision = 'deny') AS denied FROM " + s.dbName + ".events " + where + " GROUP BY bucket ORDER BY bucket ASC"
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]analyticsTimePoint, 0)
	for rows.Next() {
		var row analyticsTimePoint
		if err := rows.Scan(&row.Bucket, &row.Events, &row.Allowed, &row.Denied); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *apiServer) queryAnalyticsRecent(ctx context.Context, scope analyticsQueryScope) ([]analyticsRecentActivity, error) {
	where, args := analyticsWhereClause(scope)
	query := "SELECT timestamp, server, namespace, " + analyticsTeamIDExpression + " AS team_id, human_id, agent_id, session_id, decision, tool_name, event_type FROM " + s.dbName + ".events " + where + " ORDER BY timestamp DESC LIMIT ?"
	args = append(args, scope.Limit)
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]analyticsRecentActivity, 0, scope.Limit)
	for rows.Next() {
		var row analyticsRecentActivity
		if err := rows.Scan(&row.Timestamp, &row.Server, &row.Namespace, &row.TeamID, &row.HumanID, &row.AgentID, &row.SessionID, &row.Decision, &row.ToolName, &row.EventType); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func analyticsBucketExpression(windowDays int) string {
	if windowDays <= 2 {
		return "toStartOfHour(timestamp)"
	}
	return "toStartOfDay(timestamp)"
}

func analyticsWhereClause(scope analyticsQueryScope, extraConditions ...string) (string, []any) {
	conditions := []string{"timestamp >= ?"}
	args := []any{scope.Since}

	scopeFilters, scopeArgs := analyticsScopeConditions(scope)
	if len(scopeFilters) > 0 {
		conditions = append(conditions, "("+strings.Join(scopeFilters, " OR ")+")")
		args = append(args, scopeArgs...)
	}
	if scope.Server != "" {
		conditions = append(conditions, "server = ?")
		args = append(args, scope.Server)
	}
	if scope.Decision != "" {
		conditions = append(conditions, "decision = ?")
		args = append(args, scope.Decision)
	}
	if scope.ToolName != "" {
		conditions = append(conditions, "tool_name = ?")
		args = append(args, scope.ToolName)
	}
	for _, condition := range extraConditions {
		if condition = strings.TrimSpace(condition); condition != "" {
			conditions = append(conditions, condition)
		}
	}
	return "WHERE " + strings.Join(conditions, " AND "), args
}

func analyticsScopeConditions(scope analyticsQueryScope) ([]string, []any) {
	var conditions []string
	var args []any
	namespaces := dedupeAnalyticsStrings(scope.Namespaces)
	if len(namespaces) > 0 {
		conditions = append(conditions, "namespace IN "+sqlPlaceholders(len(namespaces)))
		args = appendStringArgs(args, namespaces)
	}
	teamIDs := dedupeAnalyticsStrings(scope.TeamIDs)
	if len(teamIDs) > 0 {
		conditions = append(conditions, analyticsTeamIDExpression+" IN "+sqlPlaceholders(len(teamIDs)))
		args = appendStringArgs(args, teamIDs)
	}
	return conditions, args
}

func sqlPlaceholders(count int) string {
	if count <= 0 {
		return "()"
	}
	parts := make([]string, count)
	for i := range parts {
		parts[i] = "?"
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func appendStringArgs(args []any, values []string) []any {
	for _, value := range values {
		args = append(args, value)
	}
	return args
}

func dedupeAnalyticsStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// handleEventsFilter handles GET /api/events/filter requests.
// It queries events filtered by optional source, event_type, and audit payload fields.
// Supports query parameters: trace_id, source, event_type, server, namespace, team_id, cluster, human_id, agent_id, session_id, decision, tool_name, limit.
// Returns filtered events ordered by timestamp descending.
func (s *apiServer) handleEventsFilter(w http.ResponseWriter, r *http.Request) {
	filters := clickhousepkg.EventFilters{
		TraceID:   r.URL.Query().Get("trace_id"),
		Source:    r.URL.Query().Get("source"),
		EventType: r.URL.Query().Get("event_type"),
		Server:    r.URL.Query().Get("server"),
		Namespace: r.URL.Query().Get("namespace"),
		TeamID:    r.URL.Query().Get("team_id"),
		Cluster:   r.URL.Query().Get("cluster"),
		HumanID:   r.URL.Query().Get("human_id"),
		AgentID:   r.URL.Query().Get("agent_id"),
		SessionID: r.URL.Query().Get("session_id"),
		Decision:  r.URL.Query().Get("decision"),
		ToolName:  r.URL.Query().Get("tool_name"),
		Limit:     clampInt(queryInt(r, "limit", 100), 1, 1000),
	}
	events, err := s.events.QueryEventsFiltered(r.Context(), filters)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query_failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

// auth is middleware that authenticates via:
//  1. Static service keys (API_KEYS / ADMIN_API_KEYS)
//  2. User-generated API keys (runtime store)
//  3. OIDC JWT Bearer tokens
func (s *apiServer) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p, ok, err := s.authenticateRequest(r); err != nil {
			log.Printf("auth error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth_failed"})
			return
		} else if ok {
			ctx := withPrincipal(r.Context(), p)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	})
}

func (s *apiServer) authOrPublicCatalog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p, ok, err := s.authenticateRequest(r); err != nil {
			log.Printf("auth error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth_failed"})
			return
		} else if ok {
			ctx := withPrincipal(r.Context(), p)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		if runtimeapi.PublicCatalogEnabled() && r.Method == http.MethodGet {
			ctx := withPrincipal(r.Context(), runtimeapi.PublicCatalogPrincipal())
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	})
}

func (s *apiServer) authenticateRequest(r *http.Request) (principal, bool, error) {
	apiKey := strings.TrimSpace(r.Header.Get("x-api-key"))
	if apiKey != "" {
		if _, ok := s.apiKeys[apiKey]; ok {
			role := roleAdmin // backward-compatible default when ADMIN_API_KEYS is unset.
			if len(s.adminAPIKeys) > 0 {
				// When ADMIN_API_KEYS is configured, API_KEYS values not present in
				// ADMIN_API_KEYS are intentionally demoted to role=user.
				role = roleUser
				if _, admin := s.adminAPIKeys[apiKey]; admin {
					role = roleAdmin
				}
			}
			return principal{Role: role, AuthType: "service_api_key", IsService: true}, true, nil
		}
		if s.userKeys != nil {
			p, ok, err := s.userKeys.AuthenticateUserAPIKey(r.Context(), apiKey)
			if err != nil {
				return principal{}, false, err
			}
			if ok {
				return p, true, nil
			}
		}
	}

	token := extractBearer(r.Header.Get("authorization"))
	if token == "" {
		return principal{}, false, nil
	}
	if s.platform != nil {
		if p, ok := s.platform.AuthenticateJWT(token); ok {
			return p, true, nil
		}
	}
	if s.jwks == nil {
		return principal{}, false, nil
	}
	parser := jwt.NewParser(jwt.WithValidMethods([]string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512"}))
	parsed, err := parser.Parse(token, s.jwks.Keyfunc)
	if err != nil || !parsed.Valid {
		return principal{}, false, nil
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return principal{}, false, nil
	}
	if s.oidcIssuer == "" || s.oidcAudience == "" {
		return principal{}, false, nil
	}
	if claims["iss"] != s.oidcIssuer {
		return principal{}, false, nil
	}
	if !audienceMatches(claims["aud"], s.oidcAudience) {
		return principal{}, false, nil
	}
	sub := strings.TrimSpace(fmt.Sprint(claims["sub"]))
	email := strings.ToLower(strings.TrimSpace(fmt.Sprint(claims["email"])))
	emailVerified, emailVerifiedPresent := emailVerifiedClaim(claims["email_verified"])
	role := roleUser
	if _, ok := s.adminUsers[sub]; ok {
		role = roleAdmin
	}
	if email != "" {
		if !emailVerifiedPresent || emailVerified {
			if _, ok := s.adminUsers[email]; ok {
				role = roleAdmin
			}
		}
	}
	if s.platform != nil {
		u, err := s.platform.EnsureOIDCUser(r.Context(), oidcProvider(s.oidcIssuer), sub, email, role)
		if err != nil {
			return principal{}, false, err
		}
		p, err := s.platform.PrincipalForUserID(r.Context(), u.ID)
		if err != nil {
			return principal{}, false, err
		}
		p.AuthType = "oidc_jwt"
		return p, true, nil
	}
	return principal{
		Role:     role,
		Subject:  sub,
		Email:    email,
		AuthType: "oidc_jwt",
	}, true, nil
}

func (s *apiServer) requireRole(role string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := principalFromContext(r.Context())
		if !ok || p.Role != role {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func oidcProvider(issuer string) string {
	issuer = strings.TrimSpace(issuer)
	if issuer == "" {
		return oidcProviderPrefix + "default"
	}
	return oidcProviderPrefix + issuer
}

func (s *apiServer) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	type authPrincipal struct {
		Role              string          `json:"role"`
		Subject           string          `json:"subject,omitempty"`
		Email             string          `json:"email,omitempty"`
		Namespace         string          `json:"namespace,omitempty"`
		AllowedNamespaces []string        `json:"allowedNamespaces,omitempty"`
		Teams             []principalTeam `json:"teams,omitempty"`
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated":          true,
		"sharedCatalogNamespace": sharedCatalogNamespace,
		"principal": authPrincipal{
			Role:              p.Role,
			Subject:           p.Subject,
			Email:             p.Email,
			Namespace:         p.Namespace,
			AllowedNamespaces: p.AllowedNamespaces,
			Teams:             p.Teams,
		},
	})
}

// audienceMatches validates if the JWT audience claim matches the expected value.
func audienceMatches(audClaim any, expected string) bool {
	return serviceutil.AudienceMatches(audClaim, expected)
}

func emailVerifiedClaim(claim any) (bool, bool) {
	switch v := claim.(type) {
	case bool:
		return v, true
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes":
			return true, true
		case "false", "0", "no":
			return false, true
		}
	}
	return false, false
}

// extractBearer extracts the JWT token from an Authorization header.
func extractBearer(auth string) string {
	return serviceutil.ExtractBearer(auth)
}

// writeJSON writes a JSON response with the specified status code.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	serviceutil.WriteJSON(w, status, payload)
}

// logRequests is middleware that logs HTTP requests.
// It logs the HTTP method, URL path, response status, and duration.
func logRequests(next http.Handler) http.Handler {
	return serviceutil.LogRequests(next)
}

// boolEnv parses a boolean environment variable.
func boolEnv(key string) (bool, bool) {
	return serviceutil.BoolEnv(key)
}

// envOr returns the value of an environment variable or a fallback if not set.
func envOr(key, fallback string) string {
	return serviceutil.EnvOr(key, fallback)
}

func splitCSVSet(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out[part] = struct{}{}
	}
	return out
}

func maskCredentialForLog(value string) string {
	v := strings.TrimSpace(value)
	if len(v) <= 6 {
		return "***"
	}
	return v[:3] + "..." + v[len(v)-2:]
}

// queryInt extracts an integer value from URL query parameters.
// It parses the query parameter with the given key and returns the parsed integer.
// If the parameter is missing or invalid, it returns the fallback value.
func queryInt(r *http.Request, key string, fallback int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

// clampInt constrains an integer value within specified bounds.
// It returns minVal if value is less than minVal, maxVal if value is greater than maxVal,
// otherwise returns value unchanged.
func clampInt(value, minVal, maxVal int) int {
	if value < minVal {
		return minVal
	}
	if value > maxVal {
		return maxVal
	}
	return value
}
