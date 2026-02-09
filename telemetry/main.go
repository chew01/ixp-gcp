package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/atomix/go-sdk/pkg/atomix"
	"github.com/atomix/go-sdk/pkg/generic"
	"github.com/chew01/ixp-gcp/shared"
	"github.com/chew01/ixp-gcp/shared/scenario"
	"github.com/segmentio/kafka-go"
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

	// TODO: make sure kube telemetry depends on kafka health
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{kafkaBootstrap},
		Topic:   scene.TelemetryKafkaTopic,
		GroupID: "telemetry-service",
	})
	defer reader.Close()

	log.Println("Telemetry service started, consuming from", scene.TelemetryKafkaTopic)
	ctx := context.Background()

	throughputMap, err := atomix.Map[string, string]("throughput-map").
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		log.Printf("Error getting throughput map: %v", err)
	}

	// TODO: dump into TSDB

	for {
		msg, err := reader.ReadMessage(ctx)
		if err != nil {
			log.Println("Error reading message:", err)
			continue
		}

		var record shared.TelemetryRecord
		if err := json.Unmarshal(msg.Value, &record); err != nil {
			log.Println("Error parsing JSON:", err)
			continue
		}

		durationSec := float64(record.WindowEndNS-record.WindowStartNS) / 1e9
		if durationSec <= 0 {
			continue
		}

		for _, flow := range record.Flows {
			throughputKbps := (float64(flow.Bytes*8) / 1e3) / durationSec

			flowKey := fmt.Sprintf(
				"%s|%d|%d",
				record.SwitchID,
				flow.IngressPort,
				flow.EgressPort,
			)

			throughputMap.Put(ctx, flowKey, fmt.Sprintf("%.f", throughputKbps))

			log.Printf(
				"[switch=%s window=%d] flow %d→%d: %.f Kbps",
				record.SwitchID,
				record.WindowStartNS,
				flow.IngressPort,
				flow.EgressPort,
				throughputKbps,
			)
		}
	}
}
