package main

import (
	"context"
	"log/slog"
	"sync"

	localotel "github.com/chew01/ixp-gcp/shared/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	telemetryMetricsOnce sync.Once
	flowsConsumedTotal   metric.Int64Counter
	flowsProcessedTotal  metric.Int64Counter
	flowMetricsPublished metric.Int64Counter
	kafkaConsumerErrors  metric.Int64Counter
)

// InitTelemetryMetrics initializes all telemetry service metrics
func InitTelemetryMetrics() {
	telemetryMetricsOnce.Do(func() {
		meter := localotel.Meter
		var err error

		flowsConsumedTotal, err = meter.Int64Counter(
			"telemetry_flows_consumed_total",
			metric.WithDescription("Total number of flow telemetry messages consumed from Kafka"),
		)
		if err != nil {
			slog.Error("failed to create flows_consumed_total metric", "error", err)
		}

		flowsProcessedTotal, err = meter.Int64Counter(
			"telemetry_flows_processed_total",
			metric.WithDescription("Total number of flow telemetry messages successfully processed"),
		)
		if err != nil {
			slog.Error("failed to create flows_processed_total metric", "error", err)
		}

		flowMetricsPublished, err = meter.Int64Counter(
			"telemetry_flow_metrics_published_total",
			metric.WithDescription("Total number of flow metrics published to Atomix storage"),
		)
		if err != nil {
			slog.Error("failed to create flow_metrics_published_total metric", "error", err)
		}

		kafkaConsumerErrors, err = meter.Int64Counter(
			"ixp.telemetry.kafka.consumer.errors",
			metric.WithDescription("Total number of Kafka consumer errors"),
		)
		if err != nil {
			slog.Error("failed to create kafka_consumer_errors_total metric", "error", err)
		}
	})
}

// MetricsRegistry holds all telemetry service metrics
type MetricsRegistry struct {
	FlowsConsumedTotal   metric.Int64Counter
	FlowsProcessedTotal  metric.Int64Counter
	FlowMetricsPublished metric.Int64Counter
	KafkaConsumerErrors  metric.Int64Counter
}

// GetMetricsRegistry returns the telemetry metrics after initialization
func GetMetricsRegistry() *MetricsRegistry {
	return &MetricsRegistry{
		FlowsConsumedTotal:   flowsConsumedTotal,
		FlowsProcessedTotal:  flowsProcessedTotal,
		FlowMetricsPublished: flowMetricsPublished,
		KafkaConsumerErrors:  kafkaConsumerErrors,
	}
}

// InitMetrics initializes all telemetry service metrics (deprecated: use InitTelemetryMetrics)
func InitMetrics(ctx context.Context) (*MetricsRegistry, error) {
	InitTelemetryMetrics()
	return GetMetricsRegistry(), nil
}
