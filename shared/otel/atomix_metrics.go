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
	atomixMetricsOnce sync.Once
	AtomixOpErrors    metric.Int64Counter
	AtomixOpLatency   metric.Int64Histogram
)

// InitAtomixMetrics initializes Atomix operation metrics (counter + histogram)
// Call this once during app startup to register the instruments
func InitAtomixMetrics() {
	atomixMetricsOnce.Do(func() {
		var err error

		// Initialize Atomix Operation Latency Histogram
		AtomixOpLatency, err = Meter.Int64Histogram(
			"ixp.atomix.operation.duration",
			metric.WithDescription("Latency of Atomix read/write operations - detects congestion and timeouts"),
			metric.WithUnit("ms"),
		)
		if err != nil {
			slog.Error("Failed to initialize AtomixOpLatency", "error", err)
		} else {
			slog.Info("Atomix operation latency histogram initialized", "metric", "ixp_atomix_operation_duration_milliseconds")
		}

		// Initialize Atomix Operation Error Counter
		AtomixOpErrors, err = Meter.Int64Counter(
			"ixp.atomix.operation.errors",
			metric.WithDescription("Count of failed Atomix operations - detects store unavailability/degradation"),
			metric.WithUnit("1"),
		)
		if err != nil {
			slog.Error("Failed to initialize AtomixOpErrors", "error", err)
			AtomixOpErrors = nil
		} else {
			slog.Info("Atomix operation error counter initialized successfully", "metric", "ixp_atomix_operation_errors_total")
			// Bootstrap emit to ensure counter exists in Prometheus from startup (use value 1, not 0)
			AtomixOpErrors.Add(context.Background(), 1, metric.WithAttributes(
				attribute.String("operation", "bootstrap"),
				attribute.String("error.type", "none"),
			))
			slog.Info("Bootstrap increment emitted for AtomixOpErrors", "metric", "ixp_atomix_operation_errors_total")
		}
	})
}

// RecordAtomixOperation records both latency histogram and error counter for an Atomix operation
// Call this after every Atomix read/write operation
//
// Parameters:
//   - ctx: Parent request context (trace ID will be preserved, timeout will be removed)
//   - operation: name of operation (e.g., "read_flows", "read_auctions", "write_bid")
//   - startTime: when the operation started (use time.Now() before the operation)
//   - err: error from the operation (nil if successful)
//
// Example:
//
//	startTime := time.Now()
//	value, err := store.Get(ctx, key)
//	RecordAtomixOperation(ctx, "read_flows", startTime, err)
func RecordAtomixOperation(ctx context.Context, operation string, startTime time.Time, err error) {
	// Create a safe context: Keeps the Trace ID, but removes the timeout!
	// This ensures metrics are independent of request lifecycle while preserving correlation
	safeCtx := context.WithoutCancel(ctx)

	latencyMs := int64(time.Since(startTime).Milliseconds())
	errorType := classifyAtomixError(err)

	// DEBUG: Log immediately to verify function is called
	slog.InfoContext(safeCtx, "🔍 RecordAtomixOperation called", "operation", operation, "error", err != nil, "latencyMs", latencyMs)

	// Record latency histogram - per OTel spec, only include error.type on failures
	if AtomixOpLatency != nil {
		if err != nil {
			// Failed operation: include error.type attribute
			AtomixOpLatency.Record(safeCtx, latencyMs,
				metric.WithAttributes(
					attribute.String("operation", operation),
					attribute.String("error.type", errorType),
				))
		} else {
			// Successful operation: no error.type attribute
			AtomixOpLatency.Record(safeCtx, latencyMs,
				metric.WithAttributes(attribute.String("operation", operation)))
		}
	}

	// Record error counter for error rate tracking
	if err != nil {
		if AtomixOpErrors != nil {
			// Use safeCtx to preserve trace correlation while removing timeout
			slog.InfoContext(safeCtx, "Recording Atomix operation error", "operation", operation, "error.type", errorType, "latency_ms", latencyMs)
			AtomixOpErrors.Add(safeCtx, 1,
				metric.WithAttributes(
					attribute.String("operation", operation),
					attribute.String("error.type", errorType),
				))
			slog.InfoContext(safeCtx, "Error counter incremented successfully", "operation", operation, "error.type", errorType)
		} else {
			slog.ErrorContext(safeCtx, "❌ CRITICAL: AtomixOpErrors counter is nil - metrics will not be recorded!", "operation", operation)
		}
		slog.ErrorContext(safeCtx, "Atomix operation failed", "operation", operation, "duration_ms", latencyMs, "error", err)
	} else {
		slog.DebugContext(safeCtx, "Atomix operation succeeded", "operation", operation, "duration_ms", latencyMs)
	}
}

func classifyAtomixError(err error) string {
	if err == nil {
		return "none"
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded") {
		return "timeout"
	}
	if strings.Contains(msg, "connection") || strings.Contains(msg, "unavailable") || strings.Contains(msg, "refused") {
		return "connection"
	}
	if strings.Contains(msg, "not found") {
		return "not_found"
	}
	return "other"
}
