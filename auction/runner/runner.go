package runner

import (
	"context"
	"encoding/json"
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
	"github.com/chew01/ixp-gcp/shared"
	pb "github.com/chew01/ixp-gcp/shared/proto/pb"
	"github.com/chew01/ixp-gcp/shared/scenario"
	"github.com/segmentio/kafka-go"
	"google.golang.org/protobuf/proto"
)

// AuctionRunner owns the auction loop
type AuctionRunner struct {
	interval time.Duration
	scenario *scenario.Scenario
	writer   *kafka.Writer
}

func New(writer *kafka.Writer, interval time.Duration, scenario *scenario.Scenario) *AuctionRunner {
	return &AuctionRunner{
		writer:   writer,
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
	}
}

func (r *AuctionRunner) runOnce(ctx context.Context, capacity uint64, egressPort uint64) {
	intervalID := currentIntervalID(r.interval)

	log.Printf("[Auction %d] Interval %s running", egressPort, intervalID)

	var bids []models.Bid

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
		valueParts := strings.Split(value, "|")
		customerID := ""
		if len(valueParts) >= 3 {
			customerID = valueParts[2]
		}

		ingressPort, err := strconv.ParseUint(key, 10, 64)
		if err != nil {
			log.Printf("Error parsing ingress port: %v", err)
			continue
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

		bids = append(bids, models.Bid{
			IngressPort: ingressPort,
			EgressPort:  egressPort,
			Units:       units,
			UnitPrice:   unitPrice,
			CustomerID:  customerID,
		})
	}

	if capacity <= 0 || len(bids) == 0 {
		log.Println("No capacity or no bids, skipping auction")
		return
	}

	log.Printf("[Auction %d] %d bids for %d units", egressPort, len(bids), capacity)

	allocations, clearingPrice := algo.RunReservationPriceAuction(intervalID, egressPort, capacity, bids, r.scenario.ReservationPrice)

	for _, alloc := range allocations {
		err := r.WriteResults(ctx, "sw-1", alloc.IngressPort, alloc.EgressPort, alloc.AllocatedUnits)
		if err != nil {
			log.Printf("Error setting up: %v", err)
			return
		}
		log.Printf("Allocated %d units (%d->%d)", alloc.AllocatedUnits, alloc.IngressPort, alloc.EgressPort)
	}

	// Bill credits per customer.
	// Bill credits: allocated_units * clearing_price per customer (grouped)
	if err := r.updateCredits(ctx, allocations, clearingPrice); err != nil {
		log.Printf("Error updating credits: %v", err)
	}

	// Accumulate utility: (valuation_per_unit - clearing_price) * allocated_units per customer.
	if err := r.updateUtility(ctx, allocations, clearingPrice); err != nil {
		log.Printf("Error updating utility: %v", err)
	}

	// Store auction history, including clearing price and per-customer allocations.
	if err := r.storeAuctionHistory(ctx, intervalID, egressPort, clearingPrice, allocations); err != nil {
		log.Printf("Error storing auction history: %v", err)
	}

	err = bidMap.Clear(ctx)
	if err != nil {
		log.Printf("Error clearing bids: %v", err)
	}

	log.Printf("[Auction %d] Interval %s clearing price %d", egressPort, intervalID, clearingPrice)
}

func (r *AuctionRunner) WriteResults(ctx context.Context, switchID string, ingressPort, egressPort, bandwidthKbps uint64) error {
	ingressPort32 := uint32(ingressPort)
	egressPort32 := uint32(egressPort)
	result := &pb.AuctionResult{
		FlowId: &pb.Flow{
			IngressPort: &ingressPort32,
			EgressPort:  &egressPort32,
		},
		BandwidthKbps: bandwidthKbps,
	}

	key := fmt.Sprintf("%s-results", switchID)
	value, err := proto.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal auction result: %w", err)
	}

	err = r.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(key),
		Value: value,
	})

	return err
}

func (r *AuctionRunner) updateCredits(ctx context.Context, allocations []models.Allocation, clearingPrice int) error {
	// Group spend by customer: allocated_units * clearing_price
	spendByCustomer := make(map[string]int)
	for _, alloc := range allocations {
		if alloc.CustomerID == "" {
			continue
		}
		amount := int(alloc.AllocatedUnits) * clearingPrice
		spendByCustomer[alloc.CustomerID] += amount
	}
	if len(spendByCustomer) == 0 {
		return nil
	}

	creditsMap, err := atomix.Map[string, string]("credits-map").
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		return fmt.Errorf("get credits map: %w", err)
	}

	for customerID, amount := range spendByCustomer {
		entry, err := creditsMap.Get(ctx, customerID)
		var cred shared.CustomerCredits
		if err == nil && entry.Value != "" {
			if err := json.Unmarshal([]byte(entry.Value), &cred); err != nil {
				log.Printf("invalid credits value for %s: %v", customerID, err)
				continue
			}
		}
		cred.TotalSpent += amount
		b, err := json.Marshal(cred)
		if err != nil {
			return fmt.Errorf("marshal credits: %w", err)
		}
		if _, err := creditsMap.Put(ctx, customerID, string(b)); err != nil {
			return fmt.Errorf("update credits for %s: %w", customerID, err)
		}
		log.Printf("[credits] %s spent %d (total %d)", customerID, amount, cred.TotalSpent)
	}
	return nil
}

// valuationForCustomer looks up the ValuationPerUnit for a customer in the scenario.
// Returns 0 if not found (will result in zero or negative utility, which is a warning sign).
func (r *AuctionRunner) valuationForCustomer(customerID string) int {
	for _, c := range r.scenario.Customers {
		if c.ID == customerID {
			return c.ValuationPerUnit
		}
	}
	return 0
}

// updateUtility accumulates (valuation_per_unit - clearing_price) * allocated_units
// per customer into the utility-map Atomix store. Values can be negative when
// clearing_price > valuation (deliberate low-valuation experiments).
func (r *AuctionRunner) updateUtility(ctx context.Context, allocations []models.Allocation, clearingPrice int) error {
	utilityByCustomer := make(map[string]int)
	for _, alloc := range allocations {
		if alloc.CustomerID == "" {
			continue
		}
		valuation := r.valuationForCustomer(alloc.CustomerID)
		utility := (valuation - clearingPrice) * int(alloc.AllocatedUnits)
		utilityByCustomer[alloc.CustomerID] += utility
	}
	if len(utilityByCustomer) == 0 {
		return nil
	}

	utilityMap, err := atomix.Map[string, string]("utility-map").
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		return fmt.Errorf("get utility map: %w", err)
	}

	for customerID, utility := range utilityByCustomer {
		entry, err := utilityMap.Get(ctx, customerID)
		var current int
		if err == nil && entry.Value != "" {
			current, _ = strconv.Atoi(entry.Value)
		}
		current += utility
		if _, err := utilityMap.Put(ctx, customerID, strconv.Itoa(current)); err != nil {
			return fmt.Errorf("update utility for %s: %w", customerID, err)
		}
		log.Printf("[utility] %s earned %d (total %d)", customerID, utility, current)
	}
	return nil
}

func (r *AuctionRunner) storeAuctionHistory(ctx context.Context, intervalID string, egressPort uint64, clearingPrice int, allocations []models.Allocation) error {
	historyMap, err := atomix.Map[string, string]("auction-history").
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		return fmt.Errorf("get auction history map: %w", err)
	}

	record := shared.AuctionHistoryRecord{
		Interval:      intervalID,
		EgressPort:    egressPort,
		ClearingPrice: clearingPrice,
	}

	for _, alloc := range allocations {
		if alloc.CustomerID == "" || alloc.AllocatedUnits == 0 {
			continue
		}
		record.Allocations = append(record.Allocations, shared.AuctionCustomerAllocation{
			CustomerID:  alloc.CustomerID,
			IngressPort: alloc.IngressPort,
			Units:       alloc.AllocatedUnits,
		})
	}

	b, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal auction history: %w", err)
	}

	key := fmt.Sprintf("%s|%d", intervalID, egressPort)
	if _, err := historyMap.Put(ctx, key, string(b)); err != nil {
		return fmt.Errorf("put auction history for %s: %w", key, err)
	}

	return nil
}

func currentIntervalID(interval time.Duration) string {
	now := time.Now().Unix()
	intervalSec := int64(interval.Seconds())
	start := (now / intervalSec) * intervalSec
	return time.Unix(start, 0).UTC().Format(time.RFC3339)
}
