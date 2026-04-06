package strategy

import "strconv"

// PriceInsensitive models an AS that values guaranteed bandwidth above cost
// (e.g. latency-critical traffic). It always bids at a fixed multiple of the
// reservation price, ignoring clearing history entirely.
type PriceInsensitive struct {
	PriceMultiplier int
}

// NewPriceInsensitive constructs a PriceInsensitive from strategy_params.
// Recognised key: "price_multiplier" (default 10).
func NewPriceInsensitive(params map[string]string) PriceInsensitive {
	multiplier := 10
	if v := params["price_multiplier"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			multiplier = n
		}
	}
	return PriceInsensitive{PriceMultiplier: multiplier}
}

func (s PriceInsensitive) ComputeBid(ctx BidContext) (units uint64, price uint64, skip bool) {
	effectiveDemand := ctx.Metrics.ThroughputKbps + ctx.Metrics.DropKbps
	if ctx.Metrics.ThroughputKbps <= 0 && ctx.Metrics.DropKbps <= 0 {
		return 0, 0, true
	}

	unitsF := effectiveDemand * 1.05
	if unitsF < 1 {
		unitsF = 1
	}

	p := uint64(ctx.Scene.ReservationPrice) * uint64(s.PriceMultiplier)

	return uint64(unitsF), p, false
}
