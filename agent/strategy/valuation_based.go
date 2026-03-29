package strategy

// ValuationBased bids exactly at the agent's configured valuation per unit.
// This is the dominant strategy for a uniform-price (second-price) auction:
// the agent wins whenever clearing_price ≤ valuation, pays the clearing price,
// and earns positive utility. There is no benefit to bidding above or below
// valuation in this mechanism.
type ValuationBased struct{}

func (s ValuationBased) ComputeBid(ctx BidContext) (units uint64, price uint64, skip bool) {
	if ctx.Metrics.ThroughputKbps <= 0 && ctx.Metrics.DropKbps <= 0 {
		return 0, 0, true
	}
	demand := ctx.Metrics.ThroughputKbps + ctx.Metrics.DropKbps
	if demand < 1 {
		demand = 1
	}
	units = uint64(demand * 1.05)
	// Bid exactly at valuation — never above, never below.
	price = uint64(ctx.ValuationPerUnit)
	if price < uint64(ctx.Scene.ReservationPrice) {
		price = uint64(ctx.Scene.ReservationPrice)
	}
	return units, price, false
}
