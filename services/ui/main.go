package main

import (
	"context"
	"embed"
	"encoding/json"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

//go:embed static/*
var staticFS embed.FS

// main initializes and starts the MCP Sentinel UI server.
// It serves static web assets and provides a dynamic /config.js endpoint
// with API configuration for the frontend. Includes tracing support.
func main() {
	port := envOr("PORT", "8082")
	apiBase := envOr("API_BASE", "/api")
	apiKey := strings.TrimSpace(os.Getenv("API_KEY"))

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/config.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/javascript")
		baseJSON, _ := json.Marshal(apiBase)
		config := "window.MCP_API_BASE = " + string(baseJSON) + ";"
		if apiKey != "" {
			keyJSON, _ := json.Marshal(apiKey)
			config += "window.MCP_API_KEY = " + string(keyJSON) + ";"
		}
		_, _ = w.Write([]byte(config))
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "static/index.html"
		} else {
			path = filepath.ToSlash(filepath.Join("static", path))
		}

		data, err := staticFS.ReadFile(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		if ext := filepath.Ext(path); ext != "" {
			if ct := mime.TypeByExtension(ext); ct != "" {
				w.Header().Set("content-type", ct)
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})

	shutdown, err := initTracer("mcp-sentinel-ui")
	if err != nil {
		log.Printf("otel init failed: %v", err)
	} else {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = shutdown(ctx)
		}()
	}

	log.Printf("mcp-sentinel-ui listening on :%s", port)
	handler := otelhttp.NewHandler(logRequests(mux), "http.server")
	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if err := httpServer.ListenAndServe(); err != nil {
		log.Fatalf("server failed: %v", err)
	}
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

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
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
