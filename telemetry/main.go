package main

import (
	"context"
	"errors"
	"log"
	"os"

	localotel "github.com/chew01/ixp-gcp/shared/otel"
	"github.com/chew01/ixp-gcp/shared/scenario"
)

func main() {
	ctx := context.Background()

	otelShutdown, err := localotel.SetupOTelSDK(ctx)
	if err != nil {
		log.Printf("Failed to setup OTel SDK: %v", err)
		return
	}
	localotel.InitInstruments()
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
		log.Fatal(err)
	}

	tlsCfg, err := newKafkaTLSConfig()
	if err != nil {
		log.Fatalf("Kafka TLS config: %v", err)
	}

	consumer, err := NewConsumer(ctx, kafkaBootstrap, scene.TelemetryKafkaTopic, kafkaDialer(tlsCfg))
	if err != nil {
		log.Fatalf("Failed to create consumer: %v", err)
	}
	defer consumer.Close()

	log.Println("Telemetry service started, consuming from", scene.TelemetryKafkaTopic)
	consumer.Run(ctx)
}
