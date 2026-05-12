package main

import (
	"context"
	"errors"
	"log/slog"
	"os"

	localotel "github.com/chew01/ixp-gcp/shared/otel"
	"github.com/chew01/ixp-gcp/shared/scenario"
	"go.opentelemetry.io/otel"
)

func main() {
	ctx := context.Background()

	// Enable OTel SDK error logging to diagnose telemetry export failures
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		slog.Error("OpenTelemetry SDK Error", "error", err)
	}))

	otelShutdown, err := localotel.SetupOTelSDK(ctx)
	if err != nil {
		slog.Error("Failed to setup OTel SDK", "error", err)
		return
	}
	localotel.InitInstruments()
	InitTelemetryMetrics()
	localotel.InitAtomixMetrics("telemetry")
	localotel.InitKafkaMetrics("telemetry")

	// Start Prometheus metrics HTTP server if in direct mode
	if os.Getenv("TELEMETRY_MODE") == "direct" {
		go localotel.ServePrometheusMetrics(":9090")
		slog.Info("Started Prometheus metrics server on :9090")
	}

	slog.Info("Telemetry service initializing")
	defer func() {
		err = errors.Join(err, otelShutdown(context.Background()))
	}()

	kafkaBootstrap := os.Getenv("KAFKA_BOOTSTRAP")
	if kafkaBootstrap == "" {
		kafkaBootstrap = "ixp-kafka-kafka-bootstrap:9092"
	}

	scenarioPath := os.Getenv("SCENARIO_PATH")
	if scenarioPath == "" {
		scenarioPath = "/etc/scenario/scenario.yaml"
	}

	scene, err := scenario.Load(scenarioPath)
	if err != nil {
		slog.Error("Failed to load scenario", "path", scenarioPath, "error", err)
		os.Exit(1)
	}
	slog.Info("Scenario loaded", "path", scenarioPath, "kafka_topic", scene.TelemetryKafkaTopic)

	tlsCfg, err := newKafkaTLSConfig()
	if err != nil {
		slog.Error("Kafka TLS config failed", "error", err)
		os.Exit(1)
	}

	consumer, err := NewConsumer(ctx, kafkaBootstrap, scene.TelemetryKafkaTopic, kafkaDialer(tlsCfg))
	if err != nil {
		slog.Error("Failed to create consumer", "kafka_bootstrap", kafkaBootstrap, "topic", scene.TelemetryKafkaTopic, "error", err)
		os.Exit(1)
	}
	defer consumer.Close()

	slog.Info("Telemetry service started", "kafka_bootstrap", kafkaBootstrap, "topic", scene.TelemetryKafkaTopic)
	slog.Info("Consumer starting; listening for incoming flows...", "kafka_bootstrap", kafkaBootstrap, "topic", scene.TelemetryKafkaTopic)
	consumer.Run(ctx)
}
