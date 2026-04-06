package strategy

import "strconv"

// Backoff models a budget-constrained AS that deliberately cools the market
// after repeated expensive rounds. Once the clearing price exceeds ExpensivePrice
// for BackoffThreshold consecutive rounds, it halves its bid price multiplier
// (floored at 0.5). The multiplier resets to 1.0 after any cheap round.
type Backoff struct {
	consecutiveExpensive int // unexported: internal round counter
	BackoffThreshold     int
	// ExpensivePrice is the clearing price above which a round counts as expensive.
	// Zero means auto: 2 × reservation_price from the scenario.
	ExpensivePrice    int
	CurrentMultiplier float64
}

// NewBackoff constructs a Backoff from strategy_params.
// Recognised keys: "backoff_threshold" (default 3), "expensive_price" (default 0 = auto).
func NewBackoff(params map[string]string) *Backoff {
	threshold := 3
	if v := params["backoff_threshold"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			threshold = n
		}
	}
	expensivePrice := 0 // 0 = auto (2 × reservation_price)
	if v := params["expensive_price"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			expensivePrice = n
		}
	}
	return &Backoff{
		BackoffThreshold:  threshold,
		ExpensivePrice:    expensivePrice,
		CurrentMultiplier: 1.0,
	}
}

func (s *Backoff) ComputeBid(ctx BidContext) (units uint64, price uint64, skip bool) {
	effectiveDemand := ctx.Metrics.ThroughputKbps + ctx.Metrics.DropKbps
	if ctx.Metrics.ThroughputKbps <= 0 && ctx.Metrics.DropKbps <= 0 {
		return 0, 0, true
	}

	expensivePrice := s.ExpensivePrice
	if expensivePrice == 0 {
		expensivePrice = 2 * ctx.Scene.ReservationPrice
	}

	if ctx.LastClearingPrice > expensivePrice {
		s.consecutiveExpensive++
	} else {
		s.consecutiveExpensive = 0
		s.CurrentMultiplier = 1.0
	}

	if s.consecutiveExpensive >= s.BackoffThreshold {
		s.CurrentMultiplier = s.CurrentMultiplier * 0.5
		if s.CurrentMultiplier < 0.5 {
			s.CurrentMultiplier = 0.5
		}
	}

	unitsF := effectiveDemand * 1.05
	if unitsF < 1 {
		unitsF = 1
	}

	reservationPrice := uint64(ctx.Scene.ReservationPrice)
	bidPrice := uint64(float64(ctx.LastClearingPrice) * s.CurrentMultiplier)
	if bidPrice < reservationPrice {
		bidPrice = reservationPrice
	}

	return uint64(unitsF), bidPrice, false
}
