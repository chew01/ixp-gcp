package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/chew01/ixp-gcp/auction/runner"
	localotel "github.com/chew01/ixp-gcp/shared/otel"
	"github.com/chew01/ixp-gcp/shared/scenario"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
)

func main() {
	ctx := context.Background()

	// Enable OTel SDK error logging to diagnose telemetry export failures
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		slog.Error("OpenTelemetry SDK Error", "error", err)
	}))

	// Set up OpenTelemetry.
	otelShutdown, err := localotel.SetupOTelSDK(ctx)
	if err != nil {
		log.Println("Failed to setup otel")
		return
	}

	// Initialize OTel-integrated logger (must be after OTel SDK setup)
	localotel.InitInstruments()

	// Start Prometheus metrics HTTP server if in direct mode
	if os.Getenv("TELEMETRY_MODE") == "direct" {
		go localotel.ServePrometheusMetrics(":9090")
		slog.Info("Started Prometheus metrics server on :9090")
	}

	ctx, span := localotel.Tracer.Start(ctx, "auction-runner-setup")
	defer span.End()
	// Handle shutdown properly so nothing leaks.
	defer func() {
		err = errors.Join(err, otelShutdown(ctx))
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
		span.SetStatus(codes.Error, "scenario setup error")
		span.RecordError(err)
		slog.ErrorContext(ctx, fmt.Sprintf("Error :%v", err), "error", err)
	}

	tlsCfg, err := newKafkaTLSConfig()
	if err != nil {
		log.Fatalf("Kafka TLS config: %v", err)
	}

	interval, err := time.ParseDuration(scene.AuctionInterval)
	if err != nil {
		span.SetStatus(codes.Error, "auction interval parse error")
		span.RecordError(err)
		slog.ErrorContext(ctx, fmt.Sprintf("Error :%v", err), "error", err)
	}

	writer := &kafka.Writer{
		Addr:                   kafka.TCP(kafkaBootstrap),
		Topic:                  scene.AuctionResultKafkaTopic,
		Balancer:               &kafka.LeastBytes{},
		AllowAutoTopicCreation: true,
		Transport:              kafkaTransport(tlsCfg),
	}
	defer writer.Close()

	r := runner.New(writer, interval, scene)

	slog.InfoContext(ctx, "Auction runner started")
	r.Run(ctx)
}
