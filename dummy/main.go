package main

import (
	"context"
	"log"
	"log/slog"
	"math/rand/v2"
	"os"

	"github.com/chew01/ixp-gcp/shared/otel"
	"github.com/chew01/ixp-gcp/shared/scenario"
	"github.com/segmentio/kafka-go"
)

func main() {
	// Setting up otel SDK
	ctx := context.Background()
	shutdown, err := otel.SetupOTelSDK(ctx)
	if err != nil {
		log.Fatalf("failed to setup otel: %v", err)
	}
	defer shutdown(ctx)

	otel.InitInstruments()

	// Start root span for the dummy service
	// ctx, rootSpan := otel.Tracer.Start(ctx, "dummy-service-setup")
	// defer rootSpan.End()

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
		slog.ErrorContext(ctx, "Failed to load scenario",
			"error", err,
			"path", scenarioPath,
		)
	}

	writer := &kafka.Writer{
		Addr:                   kafka.TCP(kafkaBootstrap),
		Topic:                  scene.TelemetryKafkaTopic,
		Balancer:               &kafka.LeastBytes{},
		AllowAutoTopicCreation: true,
	}
	defer writer.Close()

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{kafkaBootstrap},
		Topic:   scene.AuctionResultKafkaTopic,
		GroupID: "dummy-switch",
	})
	defer reader.Close()

	bidder := NewDummyBidder("http://api-gateway/bids", scene)
	producer := NewDummyProducer(writer, scene)
	sw := NewDummySwitch(reader)

	go producer.Run(ctx)

	// Dummy bidder is demo-only. It is disabled by default and can be enabled
	// explicitly via ENABLE_DUMMY_BIDDER for legacy/demo scenarios.
	if os.Getenv("ENABLE_DUMMY_BIDDER") == "true" || os.Getenv("ENABLE_DUMMY_BIDDER") == "1" {
		log.Println("starting dummy bidder (ENABLE_DUMMY_BIDDER enabled)")
		go bidder.Run(ctx)
	} else {
		log.Println("dummy bidder disabled; use customer agents instead")
	}

	sw.Run(ctx)
}

// RandRange returns random number in range min to max inclusive
func RandRange(min int, max int) int {
	return rand.IntN(max+1-min) + min
}

func RandChoice(choices []int) int {
	return choices[rand.IntN(len(choices))]
}
