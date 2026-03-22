package strategy

import (
	"math"
	"os"
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

// NewExploratory reads AGENT_EMA_ALPHA (default 0.3) and AGENT_EMA_EPSILON
// (default 5) from the environment and returns a configured Exploratory.
func NewExploratory() *Exploratory {
	alpha := 0.3
	if v := os.Getenv("AGENT_EMA_ALPHA"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f < 1 {
			alpha = f
		}
	}
	epsilon := 5
	if v := os.Getenv("AGENT_EMA_EPSILON"); v != "" {
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
