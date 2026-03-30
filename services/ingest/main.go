package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/MicahParks/keyfunc"
	"github.com/golang-jwt/jwt/v4"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

type eventPayload struct {
	Timestamp string          `json:"timestamp"`
	Source    string          `json:"source"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}

type ingestServer struct {
	writer       *kafka.Writer
	brokers      []string
	apiKeys      map[string]struct{}
	jwks         *keyfunc.JWKS
	oidcIssuer   string
	oidcAudience string
}

// main initializes and starts the MCP Sentinel Ingest service.
// It sets up Kafka producer connection, configures authentication, initializes tracing,
// sets up HTTP routes, and starts the server on the configured port.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	port := envOr("PORT", "8081")
	metricsPort := envOr("METRICS_PORT", "9091")
	brokers := strings.Split(envOr("KAFKA_BROKERS", "kafka:9092"), ",")
	topic := envOr("KAFKA_TOPIC", "mcp.events")

	apiKeys := map[string]struct{}{}
	for _, key := range strings.Split(envOr("API_KEYS", ""), ",") {
		key = strings.TrimSpace(key)
		if key != "" {
			apiKeys[key] = struct{}{}
		}
	}

	oidcIssuer := strings.TrimSpace(os.Getenv("OIDC_ISSUER"))
	oidcAudience := strings.TrimSpace(os.Getenv("OIDC_AUDIENCE"))
	jwksURL := strings.TrimSpace(os.Getenv("OIDC_JWKS_URL"))
	if (oidcIssuer != "" || oidcAudience != "") && jwksURL == "" {
		log.Fatal("OIDC_JWKS_URL is required when OIDC_ISSUER or OIDC_AUDIENCE is configured")
	}
	jwks := (*keyfunc.JWKS)(nil)
	if jwksURL != "" {
		var err error
		jwks, err = keyfunc.Get(jwksURL, keyfunc.Options{RefreshInterval: 10 * time.Minute})
		if err != nil {
			log.Fatalf("failed to load JWKS: %v", err)
		}
	}

	writer := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        topic,
		BatchTimeout: 200 * time.Millisecond,
	}

	server := &ingestServer{
		writer:       writer,
		brokers:      brokers,
		apiKeys:      apiKeys,
		jwks:         jwks,
		oidcIssuer:   oidcIssuer,
		oidcAudience: oidcAudience,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/live", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/ready", server.handleReady)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.Handle("/events", server.auth(http.HandlerFunc(server.handleEvents)))

	shutdown, err := initTracer("mcp-sentinel-ingest")
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
	go func() {
		if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("metrics server stopped: %v", err)
		}
	}()

	log.Printf("mcp-sentinel-ingest listening on :%s", port)
	handler := otelhttp.NewHandler(logRequests(mux), "http.server")
	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	_ = metricsServer.Shutdown(shutdownCtx)
	_ = writer.Close()
}

const maxBodySize = 1 << 20 // 1MB

func (s *ingestServer) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.checkKafkaReady(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":    false,
			"error": "kafka_unavailable",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *ingestServer) checkKafkaReady(ctx context.Context) error {
	var lastErr error
	for _, broker := range s.brokers {
		broker = strings.TrimSpace(broker)
		if broker == "" {
			continue
		}
		conn, err := kafka.DialContext(ctx, "tcp", broker)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no Kafka brokers configured")
}

// handleEvents handles POST /events requests.
// It validates incoming MCP events, enriches them with metadata,
// and produces them to the configured Kafka topic.
// Returns success/error response based on validation and publishing status.
func (s *ingestServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var payload eventPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}

	payload.Source = strings.TrimSpace(payload.Source)
	payload.EventType = strings.TrimSpace(payload.EventType)
	if payload.Source == "" || payload.EventType == "" || len(payload.Payload) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_fields"})
		return
	}
	if bytes.Equal(bytes.TrimSpace(payload.Payload), []byte("null")) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_fields"})
		return
	}

	if payload.Timestamp == "" {
		payload.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encode_failed"})
		return
	}

	err = s.writer.WriteMessages(r.Context(), kafka.Message{Value: raw})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "enqueue_failed"})
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

// auth is middleware that enforces API key authentication.
// It checks for x-api-key header or supports optional OIDC JWT validation.
// If no API keys are configured, authentication is bypassed.
func (s *ingestServer) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(s.apiKeys) == 0 && s.jwks == nil {
			next.ServeHTTP(w, r)
			return
		}

		if len(s.apiKeys) > 0 {
			apiKey := strings.TrimSpace(r.Header.Get("x-api-key"))
			if apiKey != "" {
				if _, ok := s.apiKeys[apiKey]; ok {
					next.ServeHTTP(w, r)
					return
				}
			}
		}

		token := extractBearer(r.Header.Get("authorization"))
		if token != "" && s.jwks != nil {
			parser := jwt.NewParser(jwt.WithValidMethods([]string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512"}))
			parsed, err := parser.Parse(token, s.jwks.Keyfunc)
			if err == nil && parsed.Valid {
				if s.oidcIssuer != "" || s.oidcAudience != "" {
					claims, ok := parsed.Claims.(jwt.MapClaims)
					if !ok {
						writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_token"})
						return
					}
					if s.oidcIssuer != "" && claims["iss"] != s.oidcIssuer {
						writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_token"})
						return
					}
					if s.oidcAudience != "" {
						if !audienceMatches(claims["aud"], s.oidcAudience) {
							writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_token"})
							return
						}
					}
				}
				next.ServeHTTP(w, r)
				return
			}
		}

		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	})
}

// audienceMatches validates if the JWT audience claim matches the expected value.
// It handles both string and string slice audience claims as per JWT specifications.
func audienceMatches(audClaim any, expected string) bool {
	switch aud := audClaim.(type) {
	case string:
		return aud == expected
	case []any:
		for _, item := range aud {
			if s, ok := item.(string); ok && s == expected {
				return true
			}
		}
	}
	return false
}

// extractBearer extracts the JWT token from an Authorization header.
// It expects the format "Bearer <token>" and returns the token part.
// Returns empty string if the format is invalid.
func extractBearer(auth string) string {
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

// writeJSON writes a JSON response with the specified status code.
// It sets appropriate Content-Type headers and handles JSON marshaling errors.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
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
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, recorder.status, time.Since(start))
	})
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

// envOr returns the value of an environment variable or a fallback if not set.
// If the environment variable is set to a non-empty value, it returns that value.
// Otherwise, it returns the provided fallback value.
func envOr(key, fallback string) string {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		return val
	}
	return fallback
}

// otlpTraceOptions configures OTLP HTTP exporter options.
// It sets up the endpoint URL and configures secure/insecure connections
// based on whether the endpoint uses HTTPS or HTTP.
func otlpTraceOptions(endpoint string) []otlptracehttp.Option {
	insecure, insecureSet := boolEnv("OTEL_EXPORTER_OTLP_INSECURE")
	if u, err := url.Parse(endpoint); err == nil {
		// Handle URLs with schemes (http://host:port/path)
		if u.Scheme != "" && u.Host == "" {
			// This is a scheme-less endpoint, fall through to treat as host:port
		} else if u.Scheme != "" && u.Host != "" {
			opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(u.Host)}
			if u.Path != "" {
				opts = append(opts, otlptracehttp.WithURLPath(u.Path))
			}
			if insecureSet {
				if insecure {
					opts = append(opts, otlptracehttp.WithInsecure())
				}
				return opts
			}
			if u.Scheme == "http" {
				opts = append(opts, otlptracehttp.WithInsecure())
			}
			return opts
		}
	}

	// Fallback: treat entire endpoint as host:port
	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(endpoint)}
	if insecureSet {
		if insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		return opts
	}
	return opts
}

// boolEnv parses a boolean environment variable.
// It returns the parsed boolean value and true if parsing succeeded.
// Returns false, false if the variable is not set or parsing failed.
func boolEnv(key string) (bool, bool) {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		parsed, err := strconv.ParseBool(val)
		if err == nil {
			return parsed, true
		}
	}
	return false, false
}
