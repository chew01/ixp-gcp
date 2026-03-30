package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/chew01/ixp-gcp/shared/scenario"
)

func main() {
	ctx := context.Background()

	kafkaBootstrap := getenv("KAFKA_BOOTSTRAP", "ixp-kafka-kafka-bootstrap:9092")
	scenarioPath := getenv("SCENARIO_PATH", "/etc/scenario/scenario.yaml")
	addr := getenv("ADDR", ":8082")
	namespace := getenv("NAMESPACE", "default")

	// Load scenario for port topology and customer list.
	scen, err := scenario.Load(scenarioPath)
	if err != nil {
		log.Fatalf("load scenario: %v", err)
	}

	// Atomix stores.
	store, err := NewDashboardStore(ctx)
	if err != nil {
		log.Fatalf("atomix init: %v", err)
	}

	// WebSocket hub.
	hub := NewHub()
	go hub.Run()

	// Atomix change poller.
	poller := NewPoller(store, hub, scen)
	go poller.Run(ctx)

	// Kafka auction-results consumer.
	consumer, err := NewAuctionConsumer(kafkaBootstrap, scen.AuctionResultKafkaTopic, store, hub)
	if err != nil {
		log.Printf("kafka init warning: %v — auction events will be unavailable", err)
	} else {
		go consumer.Run(ctx)
		defer consumer.Close()
	}

	// Kubernetes pod poller.
	k8sPoller, err := NewK8sPoller(hub, namespace)
	if err != nil {
		log.Printf("k8s init warning: %v — pod counts will be unavailable", err)
	} else {
		go k8sPoller.Run(ctx)
	}

	// HTTP server.
	srv := NewServer(store, hub, poller, consumer)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	log.Printf("dashboard listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
