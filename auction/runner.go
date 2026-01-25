package main

import (
	"context"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/atomix/go-sdk/pkg/atomix"
	"github.com/atomix/go-sdk/pkg/generic"
	"github.com/chew01/ixp-gcp/auction/algo"
	"github.com/chew01/ixp-gcp/auction/models"
	"github.com/chew01/ixp-gcp/proto"
)

// AuctionRunner owns the auction loop
type AuctionRunner struct {
	client   proto.VirtualCircuitClient // TODO: for multiple switches, use multiple clients
	interval time.Duration
}

func NewAuctionRunner(ctx context.Context, client proto.VirtualCircuitClient, interval time.Duration) *AuctionRunner {
	return &AuctionRunner{
		client:   client,
		interval: interval,
	}
}

func (r *AuctionRunner) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.runOnce(ctx)
		case <-ctx.Done():
			log.Println("Auction runner shutting down")
			return
		}
	}
}

func (r *AuctionRunner) runOnce(ctx context.Context) {
	intervalID := currentIntervalID(r.interval)

	log.Printf("Running auction for interval %s", intervalID)

	capacityUnits := int64(100) // TODO: replace with variable

	var bids []models.Bid
	bidMap, err := atomix.Map[string, string]("bid-map").
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

	log.Println("Listing bids from Atomix")

	for {
		entry, err := list.Next()
		if err != nil {
			log.Printf("Error getting next bid: %v", err)
			break
		}

		key := any(entry.Key()).(string)
		value := any(entry.Value()).(string)
		parts := strings.Split(value, "|")

		units, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			log.Printf("Error parsing units for bid %s: %v", key, err)
			continue
		}
		unitPrice, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			log.Printf("Error parsing unit price for bid %s: %v", key, err)
			continue
		}
		bids = append(bids, models.Bid{
			UserID:    key,
			Units:     units,
			UnitPrice: unitPrice,
			Interval:  intervalID,
		})
	}

	if capacityUnits <= 0 || len(bids) == 0 {
		log.Println("No capacity or no bids, skipping auction")
		return
	}

	log.Println("Running auction with", len(bids), "bids for", capacityUnits, "units")

	allocations, clearingPrice := algo.RunUniformPriceAuction(capacityUnits, bids)

	for _, alloc := range allocations {
		r.client.SetUp(ctx, &proto.SetUpRequest{
			ASideId:       1, // TODO: map user to Side ID
			BSideId:       2,
			BandwidthKbps: uint64(alloc.AllocatedUnits), // TODO: map bandwidth (kbps)
		})
	}

	log.Println("Clearing price:", clearingPrice)

	err = bidMap.Clear(ctx)
	if err != nil {
		log.Printf("Error clearing bids: %v", err)
	}

	log.Printf("Auction completed for interval %s", intervalID)
}

func currentIntervalID(interval time.Duration) string {
	now := time.Now().Unix()
	intervalSec := int64(interval.Seconds())
	start := (now / intervalSec) * intervalSec
	return time.Unix(start, 0).UTC().Format(time.RFC3339)
}
