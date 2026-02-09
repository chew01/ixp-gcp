package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/chew01/ixp-gcp/auction/runner"
	"github.com/chew01/ixp-gcp/shared/scenario"
	"github.com/segmentio/kafka-go"
)

func main() {
	intervalSeconds := 30
	if v := os.Getenv("AUCTION_INTERVAL_SECONDS"); v != "" {
		if parsed, err := time.ParseDuration(v + "s"); err == nil {
			intervalSeconds = int(parsed.Seconds())
		}
	}

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

	writer := &kafka.Writer{
		Addr:                   kafka.TCP(kafkaBootstrap),
		Topic:                  scene.AuctionResultKafkaTopic,
		Balancer:               &kafka.LeastBytes{},
		AllowAutoTopicCreation: true,
	}
	defer writer.Close()

	ctx := context.Background()
	r := runner.New(writer, time.Duration(intervalSeconds)*time.Second, scene)

	log.Println("Auction runner started")
	r.Run(ctx)
}
