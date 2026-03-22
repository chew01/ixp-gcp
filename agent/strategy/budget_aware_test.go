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
	s := strategy.BudgetAware{}

	clearing := 40

	tests := []struct {
		name            string
		startingBalance int
		totalSpent      int
		wantPrice       uint64
	}{
		{
			// fraction = (1000-100)/1000 = 0.90 > 0.75 → full clearing price
			name: "fraction > 0.75 — full clearing price",
			startingBalance: 1000, totalSpent: 100,
			wantPrice: 40,
		},
		{
			// fraction = (1000-400)/1000 = 0.60 > 0.50 → 75% of clearing
			name: "fraction > 0.50 — 75% of clearing price",
			startingBalance: 1000, totalSpent: 400,
			wantPrice: 30, // floor(40 * 0.75) = 30
		},
		{
			// fraction = (1000-700)/1000 = 0.30 > 0.25 → 50% of clearing
			name: "fraction > 0.25 — 50% of clearing price",
			startingBalance: 1000, totalSpent: 700,
			wantPrice: 20, // floor(40 * 0.50) = 20
		},
		{
			// fraction = (1000-900)/1000 = 0.10 <= 0.25 → reservation price
			name: "fraction <= 0.25 — reservation price",
			startingBalance: 1000, totalSpent: 900,
			wantPrice: 10,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
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
		// fraction > 0.50 → 75% of clearing; if clearing is low, floor applies.
		ctx := strategy.BidContext{
			Scene:   scene,
			Metrics: metrics,
			Credits: shared.CustomerCredits{StartingBalance: 1000, TotalSpent: 400},
			// clearing=8; 8*0.75=6 < reservation(10) → floor to 10
			LastClearingPrice: 8,
		}
		_, price, _ := s.ComputeBid(ctx)
		if price != 10 {
			t.Errorf("price=%d, want 10 (reservation floor)", price)
		}
	})

	t.Run("zero starting_balance — fallback to conservative", func(t *testing.T) {
		ctx := strategy.BidContext{
			Scene:   scene,
			Metrics: shared.FlowMetricsValue{ThroughputKbps: 10, DropKbps: 0},
			Credits: shared.CustomerCredits{StartingBalance: 0, TotalSpent: 0},
			// No clearing history; conservative uses reservation price.
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
