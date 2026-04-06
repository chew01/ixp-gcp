package strategy

// Conservative bids 10% above current throughput, at least 1 kbps.
// It skips flows with no throughput and no drops.
type Conservative struct{}

func (s Conservative) ComputeBid(ctx BidContext) (units uint64, price uint64, skip bool) {
	if ctx.Metrics.ThroughputKbps <= 0 && ctx.Metrics.DropKbps <= 0 {
		return 0, 0, true
	}

	unitsF := ctx.Metrics.ThroughputKbps * 1.1
	if unitsF < 1 {
		unitsF = 1
	}

	p := uint64(ctx.Scene.ReservationPrice)
	if ctx.LastClearingPrice > 0 && uint64(ctx.LastClearingPrice) > p {
		p = uint64(ctx.LastClearingPrice)
	}

	return uint64(unitsF), p, false
}
