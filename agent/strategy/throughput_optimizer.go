package strategy

import (
	"math"
	"strconv"
)

// ThroughputOptimizer bids aggressively when the market is cheap AND demand is
// high, and conservatively otherwise. It applies a budget decay factor to
// preserve credits for later rounds.
//
// Recognised strategy_params:
//   - price_threshold:  fraction below expected price that counts as "cheap" (default 0.8)
//   - high_demand_kbps: minimum kbps demand to be considered "high" (default 80)
//   - price_window:     rolling average window size for expected price   (default 3)
type ThroughputOptimizer struct {
	priceThreshold float64
	highDemandKbps float64
	history        []float64 // recent clearing prices (rolling window)
	windowSize     int
}

// NewThroughputOptimizer constructs a ThroughputOptimizer from strategy_params.
func NewThroughputOptimizer(params map[string]string) *ThroughputOptimizer {
	threshold := 0.8
	highDemand := 80.0
	window := 3

	if v := params["price_threshold"]; v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f <= 1 {
			threshold = f
		}
	}
	if v := params["high_demand_kbps"]; v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			highDemand = f
		}
	}
	if v := params["price_window"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			window = n
		}
	}

	return &ThroughputOptimizer{
		priceThreshold: threshold,
		highDemandKbps: highDemand,
		windowSize:     window,
	}
}

func (s *ThroughputOptimizer) averagePrice() float64 {
	if len(s.history) == 0 {
		return 0
	}
	sum := 0.0
	for _, p := range s.history {
		sum += p
	}
	return sum / float64(len(s.history))
}

func (s *ThroughputOptimizer) recordPrice(p float64) {
	s.history = append(s.history, p)
	if len(s.history) > s.windowSize {
		s.history = s.history[len(s.history)-s.windowSize:]
	}
}

func (s *ThroughputOptimizer) ComputeBid(ctx BidContext) (units uint64, price uint64, skip bool) {
	if ctx.Metrics.ThroughputKbps <= 0 && ctx.Metrics.DropKbps <= 0 {
		return 0, 0, true
	}

	demand := ctx.Metrics.ThroughputKbps + ctx.Metrics.DropKbps

	lastClearing := float64(ctx.LastClearingPrice)
	// Compute expected price from prior history before recording the current clearing price,
	// so priceIsCheap compares the current round against past market conditions.
	expectedPrice := s.averagePrice()
	if ctx.LastClearingPrice > 0 {
		s.recordPrice(lastClearing)
	}

	reservationPrice := float64(ctx.Scene.ReservationPrice)
	if expectedPrice < reservationPrice {
		expectedPrice = reservationPrice
	}

	demandIsHigh := demand >= s.highDemandKbps
	priceIsCheap := lastClearing > 0 && lastClearing < expectedPrice*s.priceThreshold

	var bidPrice float64
	var bidUnits float64

	switch {
	case demandIsHigh && priceIsCheap:
		// Best conditions: bid full valuation, buy extra headroom.
		bidPrice = float64(ctx.ValuationPerUnit)
		bidUnits = demand * 1.2
	case demandIsHigh && !priceIsCheap:
		// Must buy bandwidth; follow market price.
		bidPrice = lastClearing
		if bidPrice <= 0 {
			bidPrice = reservationPrice
		}
		bidUnits = demand * 1.05
	case !demandIsHigh && priceIsCheap:
		// Opportunistic: buy cheaply at a slight discount.
		bidPrice = lastClearing * 0.9
		if bidPrice < reservationPrice {
			bidPrice = reservationPrice
		}
		bidUnits = demand * 0.8
	default:
		// Expensive and low demand: conserve budget.
		bidPrice = reservationPrice
		bidUnits = demand * 0.5
	}

	if bidUnits < 1 {
		bidUnits = 1
	}

	// Budget decay factor: scale price down as balance depletes to preserve
	// credits for future rounds.
	if ctx.Credits.StartingBalance > 0 {
		remaining := float64(ctx.Credits.StartingBalance-ctx.Credits.TotalSpent) / float64(ctx.Credits.StartingBalance)
		if remaining < 0 {
			remaining = 0
		}
		bidPrice = bidPrice * remaining
	}

	if bidPrice < reservationPrice {
		bidPrice = reservationPrice
	}

	return uint64(bidUnits), uint64(math.Round(bidPrice)), false
}
