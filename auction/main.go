package main

import (
	"context"
	"log"
	"os"
	"time"

	pb "github.com/chew01/ixp-gcp/proto"
	"github.com/chew01/ixp-gcp/shared/scenario"
	"google.golang.org/grpc/credentials/insecure"

	"google.golang.org/grpc"
)

func main() {
	intervalSeconds := 30
	if v := os.Getenv("AUCTION_INTERVAL_SECONDS"); v != "" {
		if parsed, err := time.ParseDuration(v + "s"); err == nil {
			intervalSeconds = int(parsed.Seconds())
		}
	}

	grpcServerAddr := os.Getenv("GRPC_SERVER_ADDR")
	if grpcServerAddr == "" {
		log.Fatal("GRPC_SERVER_ADDR env var not set")
	}

	scenarioPath := os.Getenv("SCENARIO_PATH")
	if scenarioPath == "" {
		scenarioPath = "/etc/scenario/scenario.yaml"
	}

	scene, err := scenario.Load(scenarioPath)
	if err != nil {
		log.Fatal(err)
	}

	conn, err := grpc.NewClient(grpcServerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to create gRPC connection: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	runner := NewAuctionRunner(ctx, pb.NewVirtualCircuitClient(conn), time.Duration(intervalSeconds)*time.Second, scene)

	log.Println("Auction runner started")
	runner.Run(ctx)
}
