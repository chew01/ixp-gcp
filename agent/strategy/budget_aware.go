package strategy

// BudgetAware scales bid price down as remaining credits deplete. When
// starting_balance is zero (not configured), it falls back to conservative
// behaviour (10% above throughput, no budget awareness).
type BudgetAware struct{}

func (s BudgetAware) ComputeBid(ctx BidContext) (units uint64, price uint64, skip bool) {
	// Fallback to conservative when no starting balance is configured.
	if ctx.Credits.StartingBalance == 0 {
		return Conservative{}.ComputeBid(ctx)
	}

	if ctx.Metrics.ThroughputKbps <= 0 && ctx.Metrics.DropKbps <= 0 {
		return 0, 0, true
	}

	effectiveDemand := ctx.Metrics.ThroughputKbps + ctx.Metrics.DropKbps
	unitsF := effectiveDemand * 1.05
	if unitsF < 1 {
		unitsF = 1
	}

	remaining := ctx.Credits.StartingBalance - ctx.Credits.TotalSpent
	fraction := float64(remaining) / float64(ctx.Credits.StartingBalance)

	reservationPrice := uint64(ctx.Scene.ReservationPrice)
	clearingPrice := uint64(ctx.LastClearingPrice)

	var p uint64
	switch {
	case fraction > 0.75:
		p = clearingPrice
	case fraction > 0.50:
		p = uint64(float64(clearingPrice) * 0.75)
	case fraction > 0.25:
		p = uint64(float64(clearingPrice) * 0.50)
	default:
		p = reservationPrice
	}

	if p < reservationPrice {
		p = reservationPrice
	}

	return uint64(unitsF), p, false
}
