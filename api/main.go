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

	scenarioPath := os.Getenv("SCENARIO_PATH")
	if scenarioPath == "" {
		scenarioPath = "/etc/scenario/scenario.yaml"
	}
	scen, err := scenario.Load(scenarioPath)
	if err != nil {
		log.Fatalf("failed to load scenario: %v", err)
	}

	fs, err := NewAtomixFlowStore(ctx)
	if err != nil {
		log.Fatalf("failed to create flow store: %v", err)
	}
	bs := NewAtomixBidStore()

	server := &Server{
		fs:       fs,
		bs:       bs,
		scenario: scen,
	}

	appMux := http.NewServeMux()
	appMux.HandleFunc("/flows", server.getFlows)
	appMux.HandleFunc("/bids", server.postBid)
	appMux.HandleFunc("/metrics", server.getMetrics)

	metricsMux := http.NewServeMux()
	metricsMux.HandleFunc("/metrics", server.getMetrics)

	go func() {
		log.Println("Metrics server listening on :9090")
		log.Fatal(http.ListenAndServe(":9090", metricsMux))
	}()

	log.Println("API Gateway listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", appMux))
}
