package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/chew01/ixp-gcp/shared/scenario"
)

type DummyBidder struct {
	url      string
	http     *http.Client
	scenario *scenario.Scenario
}

type Bid struct {
	IngressPort *uint64 `json:"ingress_port"`
	EgressPort  *uint64 `json:"egress_port"` // maps to auction
	Units       *uint64 `json:"units"`       // bandwidth units (kbps)
	UnitPrice   *int    `json:"unit_price"`  // price per unit
}

func NewDummyBidder(url string, scenario *scenario.Scenario) *DummyBidder {
	return &DummyBidder{
		url:      url,
		http:     &http.Client{},
		scenario: scenario,
	}
}

func (b *DummyBidder) Run(ctx context.Context) {
	interval, err := time.ParseDuration(b.scenario.AuctionInterval)
	if err != nil {
		log.Fatal(err)
	}
	for {
		count := 0
		for _, inPort := range b.scenario.Switches[0].IngressPorts {
			for _, ePort := range b.scenario.Switches[0].EgressPorts {
				ingressPort := uint64(inPort)
				egressPort := uint64(ePort)
				units := uint64(RandRange(0, 100))
				unitPrice := RandRange(1, 100)

				bid := &Bid{
					IngressPort: &ingressPort,
					EgressPort:  &egressPort,
					Units:       &units,
					UnitPrice:   &unitPrice,
				}

				if err := b.SubmitBid(ctx, bid); err != nil {
					log.Printf("Failed to submit bid: %v", err)
				} else {
					count++
				}
			}
		}

		log.Printf("Submitted %d bids", count)
		time.Sleep(interval)
	}
}

func (b *DummyBidder) SubmitBid(ctx context.Context, bid *Bid) error {
	body, err := json.Marshal(bid)
	if err != nil {
		return fmt.Errorf("failed to marshal bid: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", b.url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.http.Do(req)
	if err != nil {
		return fmt.Errorf("failed to submit bid: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to submit bid: %v, response status: %v, body: %v", bid, resp.StatusCode, string(body))
	}

	return nil
}
