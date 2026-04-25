package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
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

var (
	processorIntakePaused = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "processor_intake_paused",
		Help: "Whether Kafka intake is paused because the pending ClickHouse batch is full.",
	})
	processorIntakePauseTransitions = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "processor_intake_pause_transitions_total",
		Help: "Total number of times Kafka intake entered the paused state.",
	})
)

func init() {
	prometheus.MustRegister(processorIntakePaused, processorIntakePauseTransitions)
}

// main initializes and starts the MCP Sentinel Processor service.
// It sets up Kafka consumer connection, ClickHouse database connection,
// configures batch processing parameters, initializes tracing,
// and starts consuming events from Kafka to insert into ClickHouse.
func main() {
	brokers := strings.Split(envOr("KAFKA_BROKERS", "kafka:9092"), ",")
	topic := envOr("KAFKA_TOPIC", "mcp.events")
	groupID := envOr("KAFKA_GROUP", "mcp-sentinel-processor")
	metricsPort := envOr("METRICS_PORT", "9102")

	clickhouseAddr := envOr("CLICKHOUSE_ADDR", "clickhouse:9000")
	dbName := envOr("CLICKHOUSE_DB", "mcp")
	if err := validateDBName(dbName); err != nil {
		log.Fatalf("invalid CLICKHOUSE_DB: %v", err)
	}

	batchSize := envInt("BATCH_SIZE", 500)
	flushInterval := envDuration("FLUSH_INTERVAL", 2*time.Second)
	if batchSize <= 0 {
		log.Printf("invalid BATCH_SIZE=%d; using default 500", batchSize)
		batchSize = 500
	}
	if flushInterval <= 0 {
		log.Printf("invalid FLUSH_INTERVAL=%s; using default 2s", flushInterval)
		flushInterval = 2 * time.Second
	}

	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr:        []string{clickhouseAddr},
		Auth:        clickhouse.Auth{Database: dbName},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Fatalf("failed to connect to clickhouse: %v", err)
	}

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  brokers,
		Topic:    topic,
		GroupID:  groupID,
		MinBytes: 1,
		MaxBytes: 10e6,
	})
	defer reader.Close()

	go func() {
		metricsMux := http.NewServeMux()
		metricsMux.Handle("/metrics", promhttp.Handler())
		metricsMux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})
		metricsServer := &http.Server{
			Addr:              ":" + metricsPort,
			Handler:           metricsMux,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      15 * time.Second,
			IdleTimeout:       60 * time.Second,
		}
		if err := metricsServer.ListenAndServe(); err != nil {
			log.Printf("metrics server stopped: %v", err)
		}
	}()

	shutdown, err := initTracer("mcp-sentinel-processor")
	if err != nil {
		log.Printf("otel init failed: %v", err)
	} else {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = shutdown(ctx)
		}()
	}

	log.Printf("mcp-sentinel-processor started")
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	tracer := otel.Tracer("mcp-sentinel-processor")

	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	batch := make([]eventPayload, 0, batchSize)
	batchMessages := make([]kafka.Message, 0, batchSize)
	pausedForFlush := false

	flush := func() {
		if len(batch) == 0 {
			return
		}
		flushCtx, span := tracer.Start(ctx, "clickhouse.insert_batch")
		span.SetAttributes(attribute.Int("batch.size", len(batch)))
		if err := insertBatch(flushCtx, conn, dbName, batch); err != nil {
			log.Printf("insert failed: %v", err)
			span.RecordError(err)
			span.End()
			return
		}
		if err := reader.CommitMessages(ctx, batchMessages...); err != nil {
			log.Printf("commit failed: %v", err)
			span.RecordError(err)
		}
		span.End()
		batch = batch[:0]
		batchMessages = batchMessages[:0]
	}

	msgChan := make(chan kafka.Message, 100)
	errChan := make(chan error, 1)
	go func() {
		for {
			msg, err := reader.FetchMessage(ctx)
			if err != nil {
				select {
				case errChan <- err:
				default:
				}
				time.Sleep(500 * time.Millisecond)
				continue
			}
			select {
			case msgChan <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		messageInput := messageInputForBatch(len(batch), batchSize, msgChan)
		if messageInput == nil && !pausedForFlush {
			log.Printf("batch reached BATCH_SIZE=%d; pausing Kafka intake until ClickHouse insert succeeds", batchSize)
			pausedForFlush = true
			processorIntakePaused.Set(1)
			processorIntakePauseTransitions.Inc()
		} else if messageInput != nil {
			pausedForFlush = false
			processorIntakePaused.Set(0)
		}

		select {
		case <-ticker.C:
			flush()
		case err := <-errChan:
			log.Printf("read failed: %v", err)
		case <-ctx.Done():
			log.Printf("shutdown signal received, flushing final batch...")
			flush()
			return
		case msg := <-messageInput:
			_, span := tracer.Start(ctx, "kafka.consume")
			span.SetAttributes(
				attribute.String("kafka.topic", msg.Topic),
				attribute.Int("kafka.partition", msg.Partition),
				attribute.Int64("kafka.offset", msg.Offset),
			)

			var payload eventPayload
			if err := json.Unmarshal(msg.Value, &payload); err != nil {
				log.Printf("invalid message: %v", err)
				span.RecordError(err)
				span.End()
				if err := reader.CommitMessages(ctx, msg); err != nil {
					log.Printf("commit failed: %v", err)
				}
				continue
			}

			if payload.Timestamp == "" {
				payload.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
			}

			batch = append(batch, payload)
			batchMessages = append(batchMessages, msg)
			span.End()
			if len(batch) >= batchSize {
				flush()
			}
		}
	}
}

func messageInputForBatch(batchLen, batchSize int, input <-chan kafka.Message) <-chan kafka.Message {
	if batchLen >= batchSize {
		return nil
	}
	return input
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

// insertBatch performs bulk insert of MCP events into ClickHouse.
// It prepares a batch insert statement and executes it with the provided events.
// Returns an error if the batch insert fails.
func insertBatch(ctx context.Context, conn clickhouse.Conn, dbName string, batch []eventPayload) error {
	insert, err := conn.PrepareBatch(ctx, "INSERT INTO "+dbName+".events (timestamp, source, event_type, payload)")
	if err != nil {
		return err
	}

	for _, event := range batch {
		ts, err := time.Parse(time.RFC3339Nano, event.Timestamp)
		if err != nil {
			ts = time.Now().UTC()
		}
		if err := insert.Append(ts, event.Source, event.EventType, string(event.Payload)); err != nil {
			return err
		}
	}

	return insert.Send()
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

// validateDBName validates ClickHouse database name format.
// It ensures the database name contains only valid characters and is not empty.
// Returns an error if the database name is invalid.
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

// envInt parses an integer environment variable.
// It returns the parsed integer value or the fallback if parsing fails.
func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return val
}

// envDuration parses a duration environment variable.
// It parses values like "30s", "5m", "1h" and returns the parsed duration.
// Returns the fallback value if parsing fails.
func envDuration(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	val, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return val
}
