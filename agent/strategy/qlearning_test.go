package strategy_test

import (
	"testing"

	"github.com/chew01/ixp-gcp/agent/strategy"
	"github.com/chew01/ixp-gcp/shared"
	"github.com/chew01/ixp-gcp/shared/scenario"
)

func makeQLCtx(throughput, drop float64, valuation, clearing int, allocUnits uint64) strategy.BidContext {
	return strategy.BidContext{
		Scene: &scenario.Scenario{
			ReservationPrice: 50,
		},
		Metrics: shared.FlowMetricsValue{
			ThroughputKbps: throughput,
			DropKbps:       drop,
		},
		ValuationPerUnit:   valuation,
		LastClearingPrice:  clearing,
		LastAllocatedUnits: allocUnits,
	}
}

func TestQLearning_SkipsZeroDemand(t *testing.T) {
	s := strategy.NewQLearning(nil)
	_, _, skip := s.ComputeBid(makeQLCtx(0, 0, 500, 100, 0))
	if !skip {
		t.Error("expected skip for zero demand")
	}
}

func TestQLearning_BidsWithPositiveDemand(t *testing.T) {
	s := strategy.NewQLearning(nil)
	units, price, skip := s.ComputeBid(makeQLCtx(20, 5, 500, 100, 10))
	if skip {
		t.Fatal("expected bid, got skip")
	}
	if units == 0 {
		t.Error("units should be > 0")
	}
	if price < 50 {
		t.Errorf("price=%d, want >= reservation_price (50)", price)
	}
}

func TestQLearning_UtilityRewardDoesNotPanic(t *testing.T) {
	s := strategy.NewQLearning(map[string]string{
		"ql_alpha":   "0.1",
		"ql_gamma":   "0.9",
		"ql_epsilon": "0.0", // deterministic: always exploit
	})
	// First call: no previous state, no reward update
	s.ComputeBid(makeQLCtx(20, 5, 500, 100, 10))
	// Second call: should compute utility reward (500-100)*10 = 4000 without panicking
	_, _, skip := s.ComputeBid(makeQLCtx(20, 5, 500, 80, 12))
	if skip {
		t.Error("expected bid on second call")
	}
}

func TestQLearning_PriceFlooredAtReservation(t *testing.T) {
	// With epsilon=0 and all Q-values at 0, agent picks action 0 (0.8× multiplier).
	// Base = max(last_clearing=0, reservation=50) = 50.
	// Action 0 → 0.8×50 = 40, floored to 50.
	s := strategy.NewQLearning(map[string]string{"ql_epsilon": "0.0"})
	_, price, _ := s.ComputeBid(makeQLCtx(20, 5, 500, 0, 0))
	if price < 50 {
		t.Errorf("price=%d, want >= 50 (reservation_price)", price)
	}
}
