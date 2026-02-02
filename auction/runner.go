package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/atomix/go-sdk/pkg/atomix"
	"github.com/atomix/go-sdk/pkg/generic"
	"github.com/chew01/ixp-gcp/auction/algo"
	"github.com/chew01/ixp-gcp/auction/models"
	"github.com/chew01/ixp-gcp/proto"
	"github.com/chew01/ixp-gcp/shared/scenario"
)

// AuctionRunner owns the auction loop
type AuctionRunner struct {
	client   proto.VirtualCircuitClient // TODO: for multiple switches, use multiple clients
	interval time.Duration
	scenario *scenario.Scenario
}

func NewAuctionRunner(_ context.Context, client proto.VirtualCircuitClient, interval time.Duration, scenario *scenario.Scenario) *AuctionRunner {
	return &AuctionRunner{
		client:   client,
		interval: interval,
		scenario: scenario,
	}
}

func (r *AuctionRunner) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			for _, port := range r.scenario.Switches[0].EgressPorts {
				r.runOnce(ctx, r.scenario.Switches[0].MaxCapacity, uint64(port))
			}
		case <-ctx.Done():
			log.Println("Auction runner shutting down")
			return
		}
		// TODO: remove previous allocs each round
	}
}

func (r *AuctionRunner) runOnce(ctx context.Context, capacity uint64, egressPort uint64) {
	intervalID := currentIntervalID(r.interval)

	log.Printf("[Auction %d] Interval %s running", egressPort, intervalID)

	var bids []models.AuctionBid

	mapID := fmt.Sprintf("bids-%d", egressPort)
	bidMap, err := atomix.Map[string, string](mapID).
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		log.Printf("Error getting bid map: %v", err)
	}

	list, err := bidMap.List(ctx)
	if err != nil {
		log.Printf("Error listing bids: %v", err)
		return
	}

	for {
		entry, err := list.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("Error getting next bid: %v", err)
			}
			break
		}

		key := any(entry.Key).(string)
		value := any(entry.Value).(string)
		keyParts := strings.Split(key, "|")
		valueParts := strings.Split(value, "|")

		ingressPort, err := strconv.ParseUint(keyParts[0], 10, 64)
		if err != nil {
			log.Printf("Error parsing ingress port: %v", err)
		}
		units, err := strconv.ParseUint(valueParts[0], 10, 64)
		if err != nil {
			log.Printf("Error parsing units: %v", err)
			continue
		}
		unitPrice, err := strconv.Atoi(valueParts[1])
		if err != nil {
			log.Printf("Error parsing unit price: %v", err)
			continue
		}

		bids = append(bids, models.AuctionBid{
			IngressPort: ingressPort,
			EgressPort:  egressPort,
			Units:       units,
			UnitPrice:   unitPrice,
		})
	}

	if capacity <= 0 || len(bids) == 0 {
		log.Println("No capacity or no bids, skipping auction")
		return
	}

	log.Printf("[Auction %d] %d bids for %d units", egressPort, len(bids), capacity)

	// allocations, clearingPrice := algo.RunUniformPriceAuction(intervalID, capacity, bids)
	allocations, clearingPrice := algo.RunReservationPriceAuction(intervalID, egressPort, capacity, bids, r.scenario.Parameters["reservation_price"])

	for _, alloc := range allocations {
		resp, err := r.client.SetUp(ctx, &proto.SetUpRequest{
			ASideId:       alloc.IngressPort,
			BSideId:       egressPort,
			BandwidthKbps: alloc.AllocatedUnits, // TODO: map bandwidth (kbps)
		})
		if err != nil {
			log.Printf("Error setting up: %v", err)
			return
		}
		if resp.IsSuccess {
			log.Printf("[Auction %d] %d allocated %d units", egressPort, alloc.IngressPort, alloc.AllocatedUnits)
		} else {
			log.Printf("[Auction %d] %d set up failed", egressPort, alloc.IngressPort)
		}

	}

	err = bidMap.Clear(ctx)
	if err != nil {
		log.Printf("Error clearing bids: %v", err)
	}

	log.Printf("[Auction %d] Interval %s clearing price %d", egressPort, intervalID, clearingPrice)
}

func currentIntervalID(interval time.Duration) string {
	now := time.Now().Unix()
	intervalSec := int64(interval.Seconds())
	start := (now / intervalSec) * intervalSec
	return time.Unix(start, 0).UTC().Format(time.RFC3339)
}
