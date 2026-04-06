package strategy

import (
	"math"
	"strconv"
	"sync"
)

// BudgetAware scales bid price down as remaining credits deplete. When balance
// is high (>75%), it bids EMA + epsilon to actively lift clearing above
// reservation_price, giving the lower tiers meaningful headroom. As balance
// falls the bid steps down: 75% of EMA → 50% of EMA → reservation_price, so
// credits are spent more slowly at the cost of reduced allocation.
//
// When starting_balance is zero (not configured), it falls back to conservative.
//
// Recognised strategy_params:
//   - ema_alpha:      EMA smoothing factor   (default 0.3, range (0,1))
//   - budget_epsilon: margin above EMA when balance > 75%  (default 5)
type BudgetAware struct {
	mu          sync.Mutex
	ema         float64
	initialized bool
	Alpha       float64
	Epsilon     int
}

func NewBudgetAware(params map[string]string) *BudgetAware {
	alpha := 0.3
	epsilon := 5
	if v := params["ema_alpha"]; v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f < 1 {
			alpha = f
		}
	}
	if v := params["budget_epsilon"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			epsilon = n
		}
	}
	return &BudgetAware{Alpha: alpha, Epsilon: epsilon}
}

func (s *BudgetAware) ComputeBid(ctx BidContext) (units uint64, price uint64, skip bool) {
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

	s.mu.Lock()
	if !s.initialized {
		s.ema = float64(ctx.LastClearingPrice)
		s.initialized = true
	} else if ctx.LastClearingPrice > 0 {
		s.ema = s.Alpha*float64(ctx.LastClearingPrice) + (1-s.Alpha)*s.ema
	}
	ema := s.ema
	s.mu.Unlock()

	remaining := ctx.Credits.StartingBalance - ctx.Credits.TotalSpent
	fraction := float64(remaining) / float64(ctx.Credits.StartingBalance)

	reservationPrice := uint64(ctx.Scene.ReservationPrice)

	var p uint64
	switch {
	case fraction > 0.75:
		// Exploratory phase: bid above EMA to lift clearing above reservation_price.
		p = uint64(math.Floor(ema)) + uint64(s.Epsilon)
	case fraction > 0.50:
		p = uint64(math.Floor(ema * 0.75))
	case fraction > 0.25:
		p = uint64(math.Floor(ema * 0.50))
	default:
		p = reservationPrice
	}

	if p < reservationPrice {
		p = reservationPrice
	}

	return uint64(unitsF), p, false
}
