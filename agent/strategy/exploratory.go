// Package strategy — exploratory (EMA-based)
//
// DEPRECATED: This strategy is retained solely for the negative-result experiment
// (Experiment 9). EMA-based price-following is theoretically misaligned with the
// uniform-price (second-price) auction mechanism used by this system. In a
// second-price auction the dominant strategy is to bid your true valuation, not to
// track the clearing price. See docs/AGENTS.md §EMA for the full argument.
//
// Do not use this strategy in production or new experiments.
package strategy

import (
	"math"
	"strconv"
)

// Exploratory maintains an exponential moving average (EMA) of observed
// clearing prices and bids at EMA + epsilon. This allows the strategy to
// track the market price without blindly following sudden spikes.
type Exploratory struct {
	ema         float64
	initialized bool
	Alpha       float64 // smoothing factor (0 < alpha < 1)
	Epsilon     int     // fixed margin above EMA
}

// NewExploratory constructs an Exploratory from strategy_params.
// Recognised keys: "ema_alpha" (default 0.3), "ema_epsilon" (default 5).
func NewExploratory(params map[string]string) *Exploratory {
	alpha := 0.3
	if v := params["ema_alpha"]; v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f < 1 {
			alpha = f
		}
	}
	epsilon := 5
	if v := params["ema_epsilon"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			epsilon = n
		}
	}
	return &Exploratory{Alpha: alpha, Epsilon: epsilon}
}

func (s *Exploratory) ComputeBid(ctx BidContext) (units uint64, price uint64, skip bool) {
	effectiveDemand := ctx.Metrics.ThroughputKbps + ctx.Metrics.DropKbps
	if ctx.Metrics.ThroughputKbps <= 0 && ctx.Metrics.DropKbps <= 0 {
		return 0, 0, true
	}

	if !s.initialized {
		s.ema = float64(ctx.LastClearingPrice)
		s.initialized = true
	} else if ctx.LastClearingPrice > 0 {
		s.ema = s.Alpha*float64(ctx.LastClearingPrice) + (1-s.Alpha)*s.ema
	}

	unitsF := effectiveDemand * 1.05
	if unitsF < 1 {
		unitsF = 1
	}

	reservationPrice := uint64(ctx.Scene.ReservationPrice)
	emaBid := uint64(math.Floor(s.ema)) + uint64(s.Epsilon)
	p := emaBid
	if p < reservationPrice {
		p = reservationPrice
	}

	return uint64(unitsF), p, false
}
