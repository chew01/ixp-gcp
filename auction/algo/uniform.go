package algo

import (
	"sort"

	"github.com/chew01/ixp-gcp/auction/models"
)

func RunUniformPriceAuction(capacity uint64, bids []models.Bid) ([]models.Allocation, int) {
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
	var marginalBids []models.Bid
	var marginalDemand uint64

	for _, bid := range bids {
		if bid.UnitPrice > clearingPrice {
			// Fully allocate
			allocations = append(allocations, models.Allocation{
				IngressPort:    bid.IngressPort,
				EgressPort:     bid.EgressPort,
				VlanID:         bid.VlanID,
				AllocatedUnits: bid.Units,
				ClearingPrice:  clearingPrice,
				Interval:       bid.Interval,
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
			allocated := bid.Units * remaining / marginalDemand

			allocations = append(allocations, models.Allocation{
				IngressPort:    bid.IngressPort,
				EgressPort:     bid.EgressPort,
				VlanID:         bid.VlanID,
				AllocatedUnits: allocated,
				ClearingPrice:  clearingPrice,
				Interval:       bid.Interval,
			})
		}
	}

	return allocations, clearingPrice
}
