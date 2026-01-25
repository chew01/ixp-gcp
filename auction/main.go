package main

import (
	"context"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	pb "github.com/chew01/ixp-gcp/proto"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/atomix/go-sdk/pkg/atomix"
	"github.com/atomix/go-sdk/pkg/generic"
	"google.golang.org/grpc"
)

// Bid represents a user bid (from Atomix)
type Bid struct {
	UserID    string
	Units     int64
	UnitPrice float64
	Interval  string
}

// Allocation represents auction output
type Allocation struct {
	UserID         string
	AllocatedUnits int64
	ClearingPrice  float64
	Interval       string
}

// AuctionRunner owns the auction loop
type AuctionRunner struct {
	client   pb.VirtualCircuitClient // TODO: for multiple switches, use multiple clients
	interval time.Duration
}

func NewAuctionRunner(ctx context.Context, client pb.VirtualCircuitClient, interval time.Duration) *AuctionRunner {
	return &AuctionRunner{
		client:   client,
		interval: interval,
	}
}

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

	conn, err := grpc.NewClient(grpcServerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to create gRPC connection: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	runner := NewAuctionRunner(ctx, pb.NewVirtualCircuitClient(conn), time.Duration(intervalSeconds)*time.Second)

	log.Println("Auction runner started")
	runner.Run(ctx)
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

	var bids []Bid
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

		key := entry.Key()
		value := entry.Value()
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
		bids = append(bids, Bid{
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

	allocations, clearingPrice := runUniformPriceAuction(capacityUnits, bids)

	for _, alloc := range allocations {
		r.client.SetUp(ctx, &pb.SetUpRequest{
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

func runUniformPriceAuction(capacity int64, bids []Bid) ([]Allocation, float64) {
	// Sort bids by unit price DESC
	sort.Slice(bids, func(i, j int) bool {
		return bids[i].UnitPrice > bids[j].UnitPrice
	})

	var allocations []Allocation
	var remaining = capacity
	var clearingPrice float64

	for _, bid := range bids {
		if remaining <= 0 {
			break
		}

		allocated := bid.Units
		if allocated > remaining {
			allocated = remaining
		}

		allocations = append(allocations, Allocation{
			UserID:         bid.UserID,
			AllocatedUnits: allocated,
			Interval:       bid.Interval,
		})

		remaining -= allocated
		clearingPrice = bid.UnitPrice
	}

	// Apply uniform price
	for i := range allocations {
		allocations[i].ClearingPrice = clearingPrice
	}

	return allocations, clearingPrice
}

func currentIntervalID(interval time.Duration) string {
	now := time.Now().Unix()
	intervalSec := int64(interval.Seconds())
	start := (now / intervalSec) * intervalSec
	return time.Unix(start, 0).UTC().Format(time.RFC3339)
}
