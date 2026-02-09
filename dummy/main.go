package main

import (
	"context"
	"log"
	"math/rand/v2"
	"net"
	"os"

	pb "github.com/chew01/ixp-gcp/proto"
	"github.com/chew01/ixp-gcp/shared/scenario"
	"github.com/segmentio/kafka-go"
	"google.golang.org/grpc"
)

const Topic = "switch-traffic-digests"
const ProduceWindowSec = 1
const BidWindowSec = 30

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

	writer := &kafka.Writer{
		Addr:                   kafka.TCP(kafkaBootstrap),
		Topic:                  Topic,
		Balancer:               &kafka.LeastBytes{},
		AllowAutoTopicCreation: true,
	}
	defer writer.Close()

	bidder := NewDummyBidder("http://api-gateway/bids", scene)

	producer := NewDummyProducer(writer, scene)
	s := grpc.NewServer()
	pb.RegisterVirtualCircuitServer(s, &DummySwitch{})

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	ctx := context.Background()
	go producer.Run(ctx)
	go bidder.Run(ctx)

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

// RandRange returns random number in range min to max inclusive
func RandRange(min int, max int) int {
	return rand.IntN(max+1-min) + min
}

func RandChoice(choices []int) int {
	return choices[rand.IntN(len(choices))]
}
