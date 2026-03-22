package strategy

// DemandCorrected bids for full observed demand (throughput + drops) rather
// than just 10% above current egress throughput. This allows the agent to
// recover faster from congestion because it accounts for packets being dropped.
type DemandCorrected struct{}

func (s DemandCorrected) ComputeBid(ctx BidContext) (units uint64, price uint64, skip bool) {
	effectiveDemand := ctx.Metrics.ThroughputKbps + ctx.Metrics.DropKbps
	if effectiveDemand <= 0 {
		return 0, 0, true
	}

	unitsF := effectiveDemand * 1.05
	if unitsF < 1 {
		unitsF = 1
	}

	p := uint64(ctx.Scene.ReservationPrice)
	if ctx.LastClearingPrice > 0 && uint64(ctx.LastClearingPrice) > p {
		p = uint64(ctx.LastClearingPrice)
	}

	return uint64(unitsF), p, false
}
