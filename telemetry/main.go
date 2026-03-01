package main

import (
	"context"
	"log"
	"os"

	"github.com/chew01/ixp-gcp/shared/scenario"
)

func main() {
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
		log.Fatal(err)
	}

	ctx := context.Background()

	consumer, err := NewConsumer(ctx, kafkaBootstrap, scene.TelemetryKafkaTopic)
	if err != nil {
		log.Fatalf("Failed to create consumer: %v", err)
	}
	defer consumer.Close()

	log.Println("Telemetry service started, consuming from", scene.TelemetryKafkaTopic)
	consumer.Run(ctx)
}
