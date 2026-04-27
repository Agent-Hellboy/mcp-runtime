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
GET /api/events/filter?source=mcp-server&event_type=tool.call&server=payments&decision=deny&agent_id=agent-42&limit=50

# Monitor API health
GET /metrics

# Health check
GET /health
*/
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/MicahParks/keyfunc"
	"github.com/golang-jwt/jwt/v4"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"

	"mcp-runtime/pkg/serviceutil"
)

type eventRow struct {
	Timestamp time.Time       `json:"timestamp"`
	Source    string          `json:"source"`
	EventType string          `json:"event_type"`
	Server    string          `json:"server,omitempty"`
	Namespace string          `json:"namespace,omitempty"`
	Cluster   string          `json:"cluster,omitempty"`
	HumanID   string          `json:"human_id,omitempty"`
	AgentID   string          `json:"agent_id,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Decision  string          `json:"decision,omitempty"`
	ToolName  string          `json:"tool_name,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

type apiServer struct {
	db           clickhouse.Conn
	dbName       string
	apiKeys      map[string]struct{}
	adminAPIKeys map[string]struct{}
	adminUsers   map[string]struct{}
	jwks         *keyfunc.JWKS
	oidcIssuer   string
	oidcAudience string
	userKeys     userAPIKeyStore
	platform     *platformStore
	runtime      *RuntimeServer
	runtimeInit  string
}

const eventSelectColumns = "timestamp, source, event_type, server, namespace, cluster, human_id, agent_id, session_id, decision, tool_name, payload"

var auditFieldColumns = map[string]string{
	"server":     "server",
	"namespace":  "namespace",
	"cluster":    "cluster",
	"human_id":   "human_id",
	"agent_id":   "agent_id",
	"session_id": "session_id",
	"decision":   "decision",
	"tool_name":  "tool_name",
}

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
	if err := validateDBName(dbName); err != nil {
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

	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{clickhouseAddr},
		Auth: clickhouse.Auth{
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
		apiKeys:      apiKeys,
		adminAPIKeys: adminAPIKeys,
		adminUsers:   adminUsers,
		jwks:         jwks,
		oidcIssuer:   oidcIssuer,
		oidcAudience: oidcAudience,
	}
	var store *platformStore
	if dsn := platformDSNFromEnv(); dsn != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		secretValue := strings.TrimSpace(os.Getenv("PLATFORM_JWT_SECRET"))
		if secretValue == "" {
			log.Fatal("PLATFORM_JWT_SECRET is required when POSTGRES_DSN or DATABASE_URL is configured")
		}
		var err error
		store, err = newPlatformStore(ctx, dsn, []byte(secretValue))
		if err != nil {
			log.Fatalf("failed to initialize platform identity database: %v", err)
		}
		if err := seedPlatformAdminFromEnv(ctx, store); err != nil {
			log.Fatalf("failed to seed platform admin: %v", err)
		}
		server.platform = store
		server.userKeys = store
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
	mux.HandleFunc("/api/auth/login", server.handleLogin)
	mux.HandleFunc("/api/auth/signup", server.handleSignup)
	mux.Handle("/api/events", server.auth(server.requireRole(roleAdmin, http.HandlerFunc(server.handleEvents))))
	mux.Handle("/api/stats", server.auth(server.requireRole(roleAdmin, http.HandlerFunc(server.handleStats))))
	mux.Handle("/api/sources", server.auth(server.requireRole(roleAdmin, http.HandlerFunc(server.handleSources))))
	mux.Handle("/api/event-types", server.auth(server.requireRole(roleAdmin, http.HandlerFunc(server.handleEventTypes))))
	mux.Handle("/api/events/filter", server.auth(server.requireRole(roleAdmin, http.HandlerFunc(server.handleEventsFilter))))
	mux.Handle("/api/auth/me", server.auth(http.HandlerFunc(server.handleAuthMe)))
	mux.Handle("/api/user/registry-credentials", server.auth(http.HandlerFunc(server.handleRegistryCredentials)))
	mux.Handle("/api/user/registry-credentials/", server.auth(http.HandlerFunc(server.handleRegistryCredentialItem)))

	// Initialize and register runtime server with Kubernetes support
	runtimeServer, err := NewRuntimeServer(conn, dbName, apiKeys)
	if err != nil {
		server.runtimeInit = err.Error()
		log.Printf("ERROR: runtime server initialization failed: %v", err)
	} else {
		server.runtime = runtimeServer
		if server.userKeys == nil {
			server.userKeys = runtimeServer
		}
		// Register all runtime endpoints with auth
		mux.Handle("/api/dashboard/summary", server.auth(server.requireRole(roleAdmin, http.HandlerFunc(runtimeServer.handleDashboardSummary))))
		mux.Handle("/api/runtime/servers", server.auth(http.HandlerFunc(runtimeServer.handleRuntimeServers)))
		mux.Handle("/api/deployments", server.auth(http.HandlerFunc(runtimeServer.handleDeployments)))
		mux.Handle("/api/deployments/", server.auth(http.HandlerFunc(runtimeServer.handleDeploymentItem)))
		mux.Handle("/api/admin/namespaces", server.auth(server.requireRole(roleAdmin, http.HandlerFunc(server.handleAdminNamespaces))))
		mux.Handle("/api/admin/deployments", server.auth(server.requireRole(roleAdmin, http.HandlerFunc(runtimeServer.handleAdminDeployments))))
		mux.Handle("/api/runtime/grants", server.auth(http.HandlerFunc(runtimeServer.handleRuntimeGrants)))
		mux.Handle("/api/runtime/sessions", server.auth(http.HandlerFunc(runtimeServer.handleRuntimeSessions)))
		mux.Handle("/api/runtime/components", server.auth(http.HandlerFunc(runtimeServer.handleRuntimeComponents)))
		mux.Handle("/api/runtime/policy", server.auth(http.HandlerFunc(runtimeServer.handleRuntimePolicy)))
		mux.Handle("/api/runtime/actions/restart", server.auth(server.requireRole(roleAdmin, http.HandlerFunc(runtimeServer.handleActionRestart))))
		// Grant item (POST /api/runtime/grants/{ns}/{name}/disable|enable, DELETE /api/runtime/grants/{ns}/{name})
		mux.Handle("/api/runtime/grants/", server.auth(http.HandlerFunc(runtimeServer.handleGrantItemPath)))
		// Session item (POST /api/runtime/sessions/{ns}/{name}/revoke|unrevoke, DELETE /api/runtime/sessions/{ns}/{name})
		mux.Handle("/api/runtime/sessions/", server.auth(http.HandlerFunc(runtimeServer.handleSessionItemPath)))
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

	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())
	metricsServer := &http.Server{
		Addr:              ":" + metricsPort,
		Handler:           metricsMux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
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
		if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
	if err := metricsServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("metrics shutdown error: %v", err)
	}
	if store != nil {
		store.close()
	}
}

// initTracer initializes OpenTelemetry tracing for the service.
// It configures OTLP HTTP exporter and sets up the tracer provider.
// Returns a shutdown function to clean up resources and any initialization error.
// If no OTEL_EXPORTER_OTLP_ENDPOINT is configured, returns a no-op shutdown function.
func initTracer(serviceName string) (func(context.Context) error, error) {
	if envName := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME")); envName != "" {
		serviceName = envName
	}
	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	opts := otlpTraceOptions(endpoint)
	exporter, err := otlptracehttp.New(context.Background(), opts...)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(context.Background(),
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, err
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}

// handleEvents handles GET /api/events requests.
// It queries the ClickHouse database for MCP events with optional limit.
// Returns events in descending timestamp order (newest first).
func (s *apiServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	limit := clampInt(queryInt(r, "limit", 100), 1, 1000)

	query := "SELECT " + eventSelectColumns + " FROM " + s.dbName + ".events ORDER BY timestamp DESC LIMIT ?"
	rows, err := s.db.Query(r.Context(), query, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query_failed"})
		return
	}
	defer rows.Close()

	events := make([]eventRow, 0, limit)
	for rows.Next() {
		var row eventRow
		if err := scanEventRow(rows, &row); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "scan_failed"})
			return
		}
		events = append(events, row)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "iteration_failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

// handleStats handles GET /api/stats requests.
// It queries the ClickHouse database for total event count.
// Returns the total number of MCP events in the system.
func (s *apiServer) handleStats(w http.ResponseWriter, r *http.Request) {
	query := "SELECT count() FROM " + s.dbName + ".events"
	row := s.db.QueryRow(r.Context(), query)
	var count uint64
	if err := row.Scan(&count); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query_failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"events_total": count})
}

// handleSources handles GET /api/sources requests.
// It queries the ClickHouse database for event counts grouped by source.
// Returns a list of sources with their event counts, ordered by count descending.
func (s *apiServer) handleSources(w http.ResponseWriter, r *http.Request) {
	query := "SELECT source, count() as count FROM " + s.dbName + ".events GROUP BY source ORDER BY count DESC"
	rows, err := s.db.Query(r.Context(), query)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query_failed"})
		return
	}
	defer rows.Close()

	type sourceStat struct {
		Source string `json:"source"`
		Count  uint64 `json:"count"`
	}

	var sources []sourceStat
	for rows.Next() {
		var stat sourceStat
		if err := rows.Scan(&stat.Source, &stat.Count); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "scan_failed"})
			return
		}
		sources = append(sources, stat)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "iteration_failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"sources": sources})
}

// handleEventTypes handles GET /api/event-types requests.
// It queries the ClickHouse database for event counts grouped by event type.
// Returns a list of event types with their counts, ordered by count descending.
func (s *apiServer) handleEventTypes(w http.ResponseWriter, r *http.Request) {
	query := "SELECT event_type, count() as count FROM " + s.dbName + ".events GROUP BY event_type ORDER BY count DESC"
	rows, err := s.db.Query(r.Context(), query)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query_failed"})
		return
	}
	defer rows.Close()

	type eventTypeStat struct {
		EventType string `json:"event_type"`
		Count     uint64 `json:"count"`
	}

	var eventTypes []eventTypeStat
	for rows.Next() {
		var stat eventTypeStat
		if err := rows.Scan(&stat.EventType, &stat.Count); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "scan_failed"})
			return
		}
		eventTypes = append(eventTypes, stat)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "iteration_failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"event_types": eventTypes})
}

// handleEventsFilter handles GET /api/events/filter requests.
// It queries events filtered by optional source, event_type, and audit payload fields.
// Supports query parameters: source, event_type, server, namespace, cluster, human_id, agent_id, session_id, decision, tool_name, limit.
// Returns filtered events ordered by timestamp descending.
func (s *apiServer) handleEventsFilter(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	eventType := r.URL.Query().Get("event_type")
	server := r.URL.Query().Get("server")
	namespace := r.URL.Query().Get("namespace")
	cluster := r.URL.Query().Get("cluster")
	humanID := r.URL.Query().Get("human_id")
	agentID := r.URL.Query().Get("agent_id")
	sessionID := r.URL.Query().Get("session_id")
	decision := r.URL.Query().Get("decision")
	toolName := r.URL.Query().Get("tool_name")
	limit := clampInt(queryInt(r, "limit", 100), 1, 1000)

	var conditions []string
	var args []any

	if source != "" {
		conditions = append(conditions, "source = ?")
		args = append(args, source)
	}

	if eventType != "" {
		conditions = append(conditions, "event_type = ?")
		args = append(args, eventType)
	}
	appendAuditFieldFilter(&conditions, &args, "server", server)
	appendAuditFieldFilter(&conditions, &args, "namespace", namespace)
	appendAuditFieldFilter(&conditions, &args, "cluster", cluster)
	appendAuditFieldFilter(&conditions, &args, "human_id", humanID)
	appendAuditFieldFilter(&conditions, &args, "agent_id", agentID)
	appendAuditFieldFilter(&conditions, &args, "session_id", sessionID)
	appendAuditFieldFilter(&conditions, &args, "decision", decision)
	appendAuditFieldFilter(&conditions, &args, "tool_name", toolName)

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	query := "SELECT " + eventSelectColumns + " FROM " + s.dbName + ".events " + whereClause + " ORDER BY timestamp DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query_failed"})
		return
	}
	defer rows.Close()

	events := make([]eventRow, 0, limit)
	for rows.Next() {
		var row eventRow
		if err := scanEventRow(rows, &row); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "scan_failed"})
			return
		}
		events = append(events, row)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "iteration_failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func appendAuditFieldFilter(conditions *[]string, args *[]any, fieldName, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	columnName, ok := auditFieldColumns[fieldName]
	if !ok {
		return
	}
	*conditions = append(*conditions, fmt.Sprintf("%s = ?", columnName))
	*args = append(*args, value)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEventRow(scanner rowScanner, row *eventRow) error {
	var payloadStr string
	if err := scanner.Scan(
		&row.Timestamp,
		&row.Source,
		&row.EventType,
		&row.Server,
		&row.Namespace,
		&row.Cluster,
		&row.HumanID,
		&row.AgentID,
		&row.SessionID,
		&row.Decision,
		&row.ToolName,
		&payloadStr,
	); err != nil {
		return err
	}
	if json.Valid([]byte(payloadStr)) {
		row.Payload = json.RawMessage(payloadStr)
		return nil
	}
	raw, _ := json.Marshal(payloadStr)
	row.Payload = raw
	return nil
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
			ctx := context.WithValue(r.Context(), principalContextKey{}, p)
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

func (s *apiServer) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	p, ok := principalFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	type authPrincipal struct {
		Role      string `json:"role"`
		Subject   string `json:"subject,omitempty"`
		Email     string `json:"email,omitempty"`
		Namespace string `json:"namespace,omitempty"`
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"principal": authPrincipal{
			Role:      p.Role,
			Subject:   p.Subject,
			Email:     p.Email,
			Namespace: p.Namespace,
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

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// logRequests is middleware that logs HTTP requests.
// It logs the HTTP method, URL path, response status, and duration.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		// #nosec G706 -- request method/path are operational logs, not used for command execution.
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, recorder.status, time.Since(start))
	})
}

// otlpTraceOptions configures OTLP HTTP exporter options.
func otlpTraceOptions(endpoint string) []otlptracehttp.Option {
	return serviceutil.OTLPTraceOptions(endpoint)
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

// validateDBName validates ClickHouse database name format.
func validateDBName(name string) error {
	if name == "" {
		return fmt.Errorf("empty")
	}
	matched, err := regexp.MatchString(`^[A-Za-z_][A-Za-z0-9_]*$`, name)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("must match ^[A-Za-z_][A-Za-z0-9_]*$")
	}
	return nil
}
