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
	_ "go.uber.org/automaxprocs" // align GOMAXPROCS with container CPU quota

	clickhousepkg "mcp-runtime/pkg/clickhouse"
	"mcp-runtime/pkg/serviceutil"
	"mcp-sentinel-api/internal/runtimeapi"
	"mcp-sentinel-api/registry"
)

type apiServer struct {
	db                chdriver.Conn
	dbName            string
	apiKeys           map[string]struct{}
	adminAPIKeys      map[string]struct{}
	legacyAdminKeys   bool
	adminUsers        map[string]struct{}
	jwks              *keyfunc.JWKS
	oidcIssuer        string
	oidcAudience      string
	userKeys          userAPIKeyStore
	registryAuth      registryCredentialAuthenticator
	registryAuthz     *registry.AuthzConfig
	registryAuthzOnce sync.Once
	platform          *platformStore
	runtime           *runtimeapi.RuntimeServer
	runtimeInit       string
	internalAuthToken string
}

const (
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
	legacyAdminKeys := legacyAdminAPIKeyFallbackEnabled()
	adminUsers := splitCSVSet(envOr("ADMIN_USERS", ""))
	for entry := range adminUsers {
		normalized := strings.ToLower(strings.TrimSpace(entry))
		if normalized != "" {
			adminUsers[normalized] = struct{}{}
		}
	}
	if len(adminAPIKeys) == 0 && len(apiKeys) > 0 {
		if legacyAdminKeys {
			log.Printf("warning: ADMIN_API_KEYS is unset; API_KEYS entries authenticate as role=admin because legacy dev/test fallback is enabled")
		} else {
			log.Printf("warning: ADMIN_API_KEYS is unset; API_KEYS entries authenticate as role=user")
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
		db:                conn,
		dbName:            dbName,
		apiKeys:           apiKeys,
		adminAPIKeys:      adminAPIKeys,
		legacyAdminKeys:   legacyAdminKeys,
		adminUsers:        adminUsers,
		jwks:              jwks,
		oidcIssuer:        oidcIssuer,
		oidcAudience:      oidcAudience,
		registryAuthz:     registry.NewAuthzConfigFromEnv(),
		internalAuthToken: strings.TrimSpace(os.Getenv("INTERNAL_AUTH_TOKEN")),
	}
	var store *platformStore
	if dsn := platformDSNFromEnv(); dsn != "" {
		startupTimeout := serviceutil.EnvDuration("PLATFORM_DB_STARTUP_TIMEOUT", defaultPlatformStoreStartupTimeout)
		ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
		defer cancel()
		jwtSecret, err := jwtSecretFromEnv()
		if err != nil {
			log.Fatal(err.Error())
		}
		store, err := openPlatformStoreWithRetry(ctx, dsn, jwtSecret, newPlatformStore)
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
	server.registerRoutes(mux)

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

func (s *apiServer) handleRegistryCredentials(w http.ResponseWriter, r *http.Request) {
	registry.HandleRegistryCredentials(w, r, registry.CredentialDependencies{
		Platform:             s.platform,
		PrincipalFromContext: principalFromContext,
		WriteJSON:            writeJSON,
		WriteBodyDecodeError: writeBodyDecodeError,
		RequestIP:            requestIP,
		AuditSource:          auditSource,
		AuditIdentityLabel:   auditIdentityLabel,
	})
}

func (s *apiServer) handleRegistryCredentialItem(w http.ResponseWriter, r *http.Request) {
	registry.HandleRegistryCredentialItem(w, r, registry.CredentialDependencies{
		Platform:             s.platform,
		PrincipalFromContext: principalFromContext,
		WriteJSON:            writeJSON,
		RequestIP:            requestIP,
		AuditSource:          auditSource,
		AuditIdentityLabel:   auditIdentityLabel,
	})
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
			role := roleUser
			if len(s.adminAPIKeys) > 0 {
				// When ADMIN_API_KEYS is configured, API_KEYS values not present in
				// ADMIN_API_KEYS are intentionally demoted to role=user.
				if _, admin := s.adminAPIKeys[apiKey]; admin {
					role = roleAdmin
				}
			} else if s.legacyAdminKeys {
				// Explicit dev/test compatibility path for legacy local setups that
				// predate ADMIN_API_KEYS.
				role = roleAdmin
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

func legacyAdminAPIKeyFallbackEnabled() bool {
	for _, key := range []string{"MCP_LEGACY_ADMIN_API_KEY_FALLBACK", "LEGACY_ADMIN_API_KEY_FALLBACK"} {
		if enabled, ok := boolEnv(key); ok {
			return enabled
		}
	}
	return strings.TrimSpace(os.Getenv("MCP_RUNTIME_TEST_MODE")) == "1"
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
