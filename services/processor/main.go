package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	clickhousepkg "mcp-runtime/pkg/clickhouse"
	"mcp-runtime/pkg/events"
	"mcp-runtime/pkg/serviceutil"
)

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
	brokers := strings.Split(serviceutil.EnvOr("KAFKA_BROKERS", "kafka:9092"), ",")
	topic := serviceutil.EnvOr("KAFKA_TOPIC", "mcp.events")
	groupID := serviceutil.EnvOr("KAFKA_GROUP", "mcp-sentinel-processor")
	metricsPort := serviceutil.EnvOr("METRICS_PORT", "9102")

	clickhouseAddr := serviceutil.EnvOr("CLICKHOUSE_ADDR", "clickhouse:9000")
	dbName := serviceutil.EnvOr("CLICKHOUSE_DB", "mcp")
	if err := clickhousepkg.ValidateDBName(dbName); err != nil {
		log.Fatalf("invalid CLICKHOUSE_DB: %v", err)
	}

	batchSize := serviceutil.EnvInt("BATCH_SIZE", 500)
	flushInterval := serviceutil.EnvDuration("FLUSH_INTERVAL", 2*time.Second)
	if batchSize <= 0 {
		log.Printf("invalid BATCH_SIZE=%d; using default 500", batchSize)
		batchSize = 500
	}
	if flushInterval <= 0 {
		log.Printf("invalid FLUSH_INTERVAL=%s; using default 2s", flushInterval)
		flushInterval = 2 * time.Second
	}

	clickhouseClient, err := clickhousepkg.NewClient(clickhousepkg.Config{
		Addr:        clickhouseAddr,
		Database:    dbName,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Fatalf("failed to connect to clickhouse: %v", err)
	}
	defer clickhouseClient.Close()

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

	shutdown, err := serviceutil.InitTracer("mcp-sentinel-processor")
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

	batch := make([]events.Envelope, 0, batchSize)
	batchMessages := make([]kafka.Message, 0, batchSize)
	pausedForFlush := false

	flush := func() {
		if len(batch) == 0 {
			return
		}
		flushCtx, span := tracer.Start(ctx, "clickhouse.insert_batch")
		span.SetAttributes(attribute.Int("batch.size", len(batch)))
		if err := clickhouseClient.InsertEvents(flushCtx, batch); err != nil {
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

			var payload events.Envelope
			if err := json.Unmarshal(msg.Value, &payload); err != nil {
				log.Printf("invalid message: %v", err)
				span.RecordError(err)
				span.End()
				if err := reader.CommitMessages(ctx, msg); err != nil {
					log.Printf("commit failed: %v", err)
				}
				continue
			}

			payload.EnsureTimestamp(time.Now().UTC())

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
