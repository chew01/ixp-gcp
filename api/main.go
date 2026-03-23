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
	cs, err := NewAtomixCreditsStore(ctx)
	if err != nil {
		log.Fatalf("failed to create credits store: %v", err)
	}
	hs, err := NewAtomixAuctionHistoryStore(ctx)
	if err != nil {
		log.Fatalf("failed to create auction history store: %v", err)
	}
	// Ensure every customer from the scenario has a credits entry (total_spent=0) so GET /credits and Prometheus show them from the start.
	seen := make(map[string]bool)
	for _, c := range scen.Customers {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		if err := cs.InitCustomerIfMissing(ctx, c.ID, c.StartingBalance); err != nil {
			log.Fatalf("failed to init credits for customer %s: %v", c.ID, err)
		}
	}

	server := &Server{
		fs:       fs,
		bs:       bs,
		cs:       cs,
		hs:       hs,
		scenario: scen,
	}

	appMux := http.NewServeMux()
	appMux.HandleFunc("/flows", server.getFlows)
	appMux.HandleFunc("/bids", server.postBid)
	appMux.HandleFunc("/credits", server.getCredits)
	appMux.HandleFunc("/auctions", server.getAuctions)
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
