package main

import (
	"context"
	"log"
	"net"
	"os"

	pb "github.com/chew01/ixp-gcp/proto"
	"github.com/segmentio/kafka-go"
	"google.golang.org/grpc"
)

const Topic = "switch-traffic-digests"
const SwitchId = "sw-1"
const FlowsPerWindow = 5
const WindowSec = 1

func main() {
	kafkaBootstrap := os.Getenv("KAFKA_BOOTSTRAP")
	if kafkaBootstrap == "" {
		log.Fatal("KAFKA_BOOTSTRAP env var not set")
	}

	writer := kafka.Writer{
		Addr:     kafka.TCP(kafkaBootstrap),
		Topic:    Topic,
		Balancer: &kafka.LeastBytes{},
	}
	defer writer.Close()

	producer := DummyProducer{switchID: SwitchId, kafka: &writer}
	s := grpc.NewServer()
	pb.RegisterVirtualCircuitServer(s, &DummySwitch{})

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	ctx := context.Background()
	go producer.Run(ctx)

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
