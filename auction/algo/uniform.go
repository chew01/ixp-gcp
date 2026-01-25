package algo

import (
	"sort"

	"github.com/chew01/ixp-gcp/auction/models"
)

func RunUniformPriceAuction(capacity int64, bids []models.Bid) ([]models.Allocation, float64) {
	// Sort bids by unit price DESC
	sort.Slice(bids, func(i, j int) bool {
		return bids[i].UnitPrice > bids[j].UnitPrice
	})

	var allocations []models.Allocation
	remaining := capacity

	// First pass: find clearing price
	var clearingPrice float64

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
	var marginalDemand int64

	for _, bid := range bids {
		if bid.UnitPrice > clearingPrice {
			// Fully allocate
			allocations = append(allocations, models.Allocation{
				UserID:         bid.UserID,
				AllocatedUnits: bid.Units,
				Interval:       bid.Interval,
				ClearingPrice:  clearingPrice,
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
				UserID:         bid.UserID,
				AllocatedUnits: allocated,
				Interval:       bid.Interval,
				ClearingPrice:  clearingPrice,
			})
		}
	}

	return allocations, clearingPrice
}
