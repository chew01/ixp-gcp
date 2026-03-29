package strategy

import (
	"math"
	"math/rand"
	"strconv"
	"sync"
	"time"
)

const (
	numQLStates  = 16 // 4 drop buckets × 4 budget buckets
	numQLActions = 6
)

// actionMultipliers are the price multipliers over the last clearing price
// that the agent can choose between.
var actionMultipliers = [numQLActions]float64{0.8, 1.0, 1.25, 1.5, 2.0, 3.0}

type flowKey struct {
	ingress, egress uint32
}

type flowMemory struct {
	prevState      int
	prevAction     int
	prevDrop       float64
	hasPrev        bool
	prevValuation  int
	prevClearing   int
	prevAllocUnits uint64
}

// QLearning uses tabular Q-learning to bid. It maintains a Q-table over
// (drop_bucket, budget_bucket) × price_multiplier and updates it each round
// using the change in drop rate as the reward signal.
//
// Recognised strategy_params:
//   - ql_alpha:   learning rate          (default 0.1,  range (0,1))
//   - ql_gamma:   discount factor        (default 0.9,  range (0,1))
//   - ql_epsilon: exploration probability (default 0.15, range (0,1))
type QLearning struct {
	mu     sync.Mutex
	Q      [numQLStates][numQLActions]float64
	flows  map[flowKey]*flowMemory
	Alpha   float64
	Gamma   float64
	Epsilon float64
	rng     *rand.Rand
}

func NewQLearning(params map[string]string) *QLearning {
	alpha, gamma, epsilon := 0.1, 0.9, 0.15

	if v := params["ql_alpha"]; v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f < 1 {
			alpha = f
		}
	}
	if v := params["ql_gamma"]; v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f < 1 {
			gamma = f
		}
	}
	if v := params["ql_epsilon"]; v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f < 1 {
			epsilon = f
		}
	}

	return &QLearning{
		flows:   make(map[flowKey]*flowMemory),
		Alpha:   alpha,
		Gamma:   gamma,
		Epsilon: epsilon,
		rng:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// dropBucket discretises drop rate into 4 levels.
func dropBucket(dropRate float64) int {
	switch {
	case dropRate < 0.01:
		return 0 // negligible
	case dropRate < 0.10:
		return 1 // light
	case dropRate < 0.30:
		return 2 // moderate
	default:
		return 3 // heavy
	}
}

// budgetBucket discretises remaining credit fraction into 4 levels.
// Returns 0 (full) when no starting balance is configured.
func budgetBucket(startingBalance, totalSpent int) int {
	if startingBalance == 0 {
		return 0
	}
	remaining := startingBalance - totalSpent
	fraction := float64(remaining) / float64(startingBalance)
	switch {
	case fraction > 0.75:
		return 0
	case fraction > 0.50:
		return 1
	case fraction > 0.25:
		return 2
	default:
		return 3
	}
}

func encodeState(dropB, budgetB int) int { return dropB*4 + budgetB }

func (s *QLearning) maxQ(state int) float64 {
	best := s.Q[state][0]
	for _, v := range s.Q[state][1:] {
		if v > best {
			best = v
		}
	}
	return best
}

func (s *QLearning) bestAction(state int) int {
	best := 0
	for a := 1; a < numQLActions; a++ {
		if s.Q[state][a] > s.Q[state][best] {
			best = a
		}
	}
	return best
}

func (s *QLearning) ComputeBid(ctx BidContext) (units uint64, price uint64, skip bool) {
	effectiveDemand := ctx.Metrics.ThroughputKbps + ctx.Metrics.DropKbps
	if ctx.Metrics.ThroughputKbps <= 0 && ctx.Metrics.DropKbps <= 0 {
		return 0, 0, true
	}

	unitsF := effectiveDemand * 1.05
	if unitsF < 1 {
		unitsF = 1
	}

	var dropRate float64
	if total := ctx.Metrics.ThroughputKbps + ctx.Metrics.DropKbps; total > 0 {
		dropRate = ctx.Metrics.DropKbps / total
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := flowKey{ctx.IngressPort, ctx.EgressPort}
	fm, ok := s.flows[key]
	if !ok {
		fm = &flowMemory{}
		s.flows[key] = fm
	}

	currState := encodeState(
		dropBucket(dropRate),
		budgetBucket(ctx.Credits.StartingBalance, ctx.Credits.TotalSpent),
	)

	// Update Q table: reward is utility earned in the previous round.
	// utility = (valuation_per_unit - clearing_price) * allocated_units
	// This aligns the agent's objective with economic efficiency rather than
	// drop-rate reduction, and rewards winning rounds at low clearing prices.
	if fm.hasPrev {
		reward := float64(fm.prevValuation-fm.prevClearing) * float64(fm.prevAllocUnits)
		old := s.Q[fm.prevState][fm.prevAction]
		s.Q[fm.prevState][fm.prevAction] = old + s.Alpha*(reward+s.Gamma*s.maxQ(currState)-old)
	}

	// Epsilon-greedy action selection.
	var action int
	if s.rng.Float64() < s.Epsilon {
		action = s.rng.Intn(numQLActions)
	} else {
		action = s.bestAction(currState)
	}

	fm.prevState = currState
	fm.prevAction = action
	fm.prevDrop = dropRate
	fm.prevValuation = ctx.ValuationPerUnit
	fm.prevClearing = ctx.LastClearingPrice
	fm.prevAllocUnits = ctx.LastAllocatedUnits
	fm.hasPrev = true

	// Translate action index to a concrete price.
	reservationPrice := uint64(ctx.Scene.ReservationPrice)
	base := uint64(ctx.LastClearingPrice)
	if base < reservationPrice {
		base = reservationPrice
	}
	bidPrice := uint64(math.Round(float64(base) * actionMultipliers[action]))
	if bidPrice < reservationPrice {
		bidPrice = reservationPrice
	}

	return uint64(unitsF), bidPrice, false
}
