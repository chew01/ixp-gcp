package strategy

import (
	"github.com/chew01/ixp-gcp/shared"
	"github.com/chew01/ixp-gcp/shared/scenario"
)

// BidContext bundles all information a strategy needs for one (ingress, egress) pair.
type BidContext struct {
	Scene              *scenario.Scenario
	CustomerID         string
	SwitchID           string
	IngressPort        uint32
	EgressPort         uint32
	Metrics            shared.FlowMetricsValue
	Credits            shared.CustomerCredits
	LastClearingPrice  int
	ValuationPerUnit   int    // from scenario customer config; used by valuation_based and utility calc
	LastAllocatedUnits uint64 // units allocated in the most recent auction round (used for utility-aligned reward)
}

// Bidder computes the bid quantity and price for a single flow.
// Returning skip=true means no bid should be submitted for this flow this round.
type Bidder interface {
	ComputeBid(ctx BidContext) (units uint64, price uint64, skip bool)
}
