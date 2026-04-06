package strategy_test

import (
	"testing"

	"github.com/chew01/ixp-gcp/agent/strategy"
	"github.com/chew01/ixp-gcp/shared"
	"github.com/chew01/ixp-gcp/shared/scenario"
)

func TestBudgetAware_ComputeBid(t *testing.T) {
	scene := &scenario.Scenario{
		ReservationPrice: 10,
	}
	metrics := shared.FlowMetricsValue{ThroughputKbps: 8, DropKbps: 2}

	clearing := 40

	tests := []struct {
		name            string
		startingBalance int
		totalSpent      int
		wantPrice       uint64
	}{
		{
			// fraction = (1000-100)/1000 = 0.90 > 0.75 → EMA(40) + epsilon(5) = 45
			name:            "fraction > 0.75 — EMA + epsilon",
			startingBalance: 1000, totalSpent: 100,
			wantPrice: 45,
		},
		{
			// fraction = (1000-400)/1000 = 0.60 > 0.50 → floor(40 * 0.75) = 30
			name:            "fraction > 0.50 — 75% of EMA",
			startingBalance: 1000, totalSpent: 400,
			wantPrice: 30,
		},
		{
			// fraction = (1000-700)/1000 = 0.30 > 0.25 → floor(40 * 0.50) = 20
			name:            "fraction > 0.25 — 50% of EMA",
			startingBalance: 1000, totalSpent: 700,
			wantPrice: 20,
		},
		{
			// fraction = (1000-900)/1000 = 0.10 <= 0.25 → reservation price
			name:            "fraction <= 0.25 — reservation price",
			startingBalance: 1000, totalSpent: 900,
			wantPrice: 10,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Each sub-test gets a fresh strategy so EMA always initialises to clearing.
			s := strategy.NewBudgetAware(nil)
			ctx := strategy.BidContext{
				Scene:   scene,
				Metrics: metrics,
				Credits: shared.CustomerCredits{
					StartingBalance: tc.startingBalance,
					TotalSpent:      tc.totalSpent,
				},
				LastClearingPrice: clearing,
			}
			_, price, skip := s.ComputeBid(ctx)
			if skip {
				t.Fatal("unexpected skip")
			}
			if price != tc.wantPrice {
				t.Errorf("price=%d, want %d", price, tc.wantPrice)
			}
		})
	}

	t.Run("price floored at reservation_price", func(t *testing.T) {
		// fraction > 0.50 → floor(0.75 * EMA); if EMA is low, floor applies.
		// clearing=8 → EMA=8; floor(8*0.75)=6 < reservation(10) → floor to 10.
		s := strategy.NewBudgetAware(nil)
		ctx := strategy.BidContext{
			Scene:   scene,
			Metrics: metrics,
			Credits: shared.CustomerCredits{StartingBalance: 1000, TotalSpent: 400},
			LastClearingPrice: 8,
		}
		_, price, _ := s.ComputeBid(ctx)
		if price != 10 {
			t.Errorf("price=%d, want 10 (reservation floor)", price)
		}
	})

	t.Run("EMA rises above reservation after warm-up", func(t *testing.T) {
		// In the >75% tier with clearing=reservation(10), EMA converges toward 10.
		// Once EMA + epsilon(5) > 10, the bid escapes the fixed point.
		s := strategy.NewBudgetAware(nil)
		ctx := strategy.BidContext{
			Scene:   scene,
			Metrics: metrics,
			Credits: shared.CustomerCredits{StartingBalance: 1000, TotalSpent: 0},
			LastClearingPrice: 10,
		}
		var lastPrice uint64
		for i := 0; i < 20; i++ {
			_, lastPrice, _ = s.ComputeBid(ctx)
		}
		if lastPrice <= uint64(scene.ReservationPrice) {
			t.Errorf("after warm-up, price=%d still at reservation; expected above %d", lastPrice, scene.ReservationPrice)
		}
	})

	t.Run("zero starting_balance — fallback to conservative", func(t *testing.T) {
		s := strategy.NewBudgetAware(nil)
		ctx := strategy.BidContext{
			Scene:   scene,
			Metrics: shared.FlowMetricsValue{ThroughputKbps: 10, DropKbps: 0},
			Credits: shared.CustomerCredits{StartingBalance: 0, TotalSpent: 0},
			LastClearingPrice: 0,
		}
		units, price, skip := s.ComputeBid(ctx)
		if skip {
			t.Fatal("unexpected skip")
		}
		// Conservative: units = floor(10*1.1)=11, price = reservation=10.
		if units < 11 {
			t.Errorf("units=%d, want >= 11 (conservative fallback)", units)
		}
		if price != 10 {
			t.Errorf("price=%d, want 10 (reservation, conservative fallback)", price)
		}
	})

	t.Run("zero traffic — skip", func(t *testing.T) {
		s := strategy.NewBudgetAware(nil)
		ctx := strategy.BidContext{
			Scene:   scene,
			Metrics: shared.FlowMetricsValue{ThroughputKbps: 0, DropKbps: 0},
			Credits: shared.CustomerCredits{StartingBalance: 1000, TotalSpent: 100},
		}
		_, _, skip := s.ComputeBid(ctx)
		if !skip {
			t.Error("expected skip=true for zero traffic")
		}
	})
}
