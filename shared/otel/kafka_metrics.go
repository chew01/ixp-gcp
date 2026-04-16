package otel

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	kafkaMetricsOnce sync.Once

	// per-service Kafka operation metrics
	KafkaOpLatency map[string]metric.Int64Histogram = make(map[string]metric.Int64Histogram)
	KafkaOpErrors  map[string]metric.Int64Counter   = make(map[string]metric.Int64Counter)
)

// InitKafkaMetrics initializes service-scoped Kafka operation metrics
// serviceName should be the service name (e.g., "telemetry", "auction")
func InitKafkaMetrics(serviceName string) {
	kafkaMetricsOnce.Do(func() {
		meter := Meter
		var err error

		// Latency histogram for Kafka operations (producer/consumer)
		latencyHist, err := meter.Int64Histogram(
			"ixp."+serviceName+".kafka.operation.latency",
			metric.WithDescription("Kafka operation latency (producer/consumer) in milliseconds"),
			metric.WithUnit("ms"),
		)
		if err != nil {
			slog.Error("failed to create kafka operation latency histogram", "service", serviceName, "error", err)
		} else {
			KafkaOpLatency[serviceName] = latencyHist
		}

		// Error counter for Kafka operations
		errorCounter, err := meter.Int64Counter(
			"ixp."+serviceName+".kafka.operation.errors",
			metric.WithDescription("Kafka operation errors (producer/consumer)"),
		)
		if err != nil {
			slog.Error("failed to create kafka operation errors counter", "service", serviceName, "error", err)
		} else {
			KafkaOpErrors[serviceName] = errorCounter
		}
	})
}

// classifyKafkaError categorizes Kafka errors into meaningful types for observability
func classifyKafkaError(err error) string {
	if err == nil {
		return "success"
	}

	errStr := err.Error()

	// Check for common Kafka error patterns
	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "Timeout") {
		return "timeout"
	}
	if strings.Contains(errStr, "connection") || strings.Contains(errStr, "Connection") ||
		strings.Contains(errStr, "dial failed") || strings.Contains(errStr, "connection refused") {
		return "connection"
	}
	if strings.Contains(errStr, "offset") || strings.Contains(errStr, "Offset") {
		return "offset_out_of_range"
	}
	if strings.Contains(errStr, "illegal argument") || strings.Contains(errStr, "Illegal") {
		return "validation"
	}
	if strings.Contains(errStr, "broker") || strings.Contains(errStr, "Broker") ||
		strings.Contains(errStr, "unavailable") || strings.Contains(errStr, "Unavailable") {
		return "broker_unavailable"
	}

	return "other"
}

// RecordKafkaOperation records both latency and errors for Kafka producer/consumer operations
// serviceName should match the service (e.g., "telemetry", "auction")
// operation should describe the operation (e.g., "produce", "consume", "fetch_metadata")
func RecordKafkaOperation(ctx context.Context, serviceName, operation string, startTime time.Time, err error) {
	safeCtx := context.WithoutCancel(ctx)
	latencyMs := int64(time.Since(startTime).Milliseconds())
	errorType := classifyKafkaError(err)

	// Record latency
	if latencyHist, ok := KafkaOpLatency[serviceName]; ok && latencyHist != nil {
		latencyHist.Record(safeCtx, latencyMs, metric.WithAttributes(
			attribute.String("operation", operation),
		))
	}

	// Record error if present
	if err != nil {
		if errCounter, ok := KafkaOpErrors[serviceName]; ok && errCounter != nil {
			errCounter.Add(safeCtx, 1, metric.WithAttributes(
				attribute.String("operation", operation),
				attribute.String("error.type", errorType),
			))
		}
	}
}
