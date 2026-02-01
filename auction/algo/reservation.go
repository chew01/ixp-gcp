package algo

import (
	"sort"

	"github.com/chew01/ixp-gcp/auction/models"
)

func RunReservationPriceAuction(intervalID string, capacity uint64, bids []models.AuctionBid, rPrice int) ([]models.Allocation, int) {
	bids = append(bids, models.AuctionBid{
		IngressPort: 0,
		EgressPort:  0,
		Units:       capacity,
		UnitPrice:   rPrice,
		IsVirtual:   true,
	})

	// Sort bids by unit price DESC
	sort.Slice(bids, func(i, j int) bool {
		return bids[i].UnitPrice > bids[j].UnitPrice
	})

	var allocations []models.Allocation
	remaining := capacity

	// First pass: find clearing price
	var clearingPrice int

	for _, bid := range bids {
		if remaining <= 0 {
			break
		}
		remaining -= bid.Units
		clearingPrice = bid.UnitPrice
	}

	// Reset remaining capacity
	remaining = capacity

	// Second pass: allocate
	var marginalBids []models.AuctionBid
	var marginalDemand uint64

	for _, bid := range bids {
		if bid.UnitPrice > clearingPrice {
			// Fully allocate
			allocations = append(allocations, models.Allocation{
				IngressPort:    bid.IngressPort,
				EgressPort:     bid.EgressPort,
				AllocatedUnits: bid.Units,
				ClearingPrice:  clearingPrice,
				Interval:       intervalID,
			})
			remaining -= bid.Units
		} else if bid.UnitPrice == clearingPrice {
			marginalBids = append(marginalBids, bid)
			marginalDemand += bid.Units
		}
	}

	// Proportional allocation for marginal bids
	if marginalDemand > 0 && remaining > 0 {
		for _, bid := range marginalBids {
			if bid.IsVirtual {
				continue
			}

			allocated := bid.Units * remaining / marginalDemand
			if allocated > bid.Units {
				allocated = bid.Units // give at most what was asked for
			}

			allocations = append(allocations, models.Allocation{
				IngressPort:    bid.IngressPort,
				EgressPort:     bid.EgressPort,
				AllocatedUnits: allocated,
				ClearingPrice:  clearingPrice,
				Interval:       intervalID,
			})
		}
	}

	return allocations, clearingPrice
}
