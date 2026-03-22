package strategy_test

import (
	"testing"

	"github.com/chew01/ixp-gcp/agent/strategy"
	"github.com/chew01/ixp-gcp/shared"
	"github.com/chew01/ixp-gcp/shared/scenario"
)

func TestPriceInsensitive_ComputeBid(t *testing.T) {
	scene := &scenario.Scenario{
		ReservationPrice: 10,
	}

	t.Run("zero traffic — skip", func(t *testing.T) {
		s := strategy.NewPriceInsensitive()
		ctx := strategy.BidContext{
			Scene:   scene,
			Metrics: shared.FlowMetricsValue{ThroughputKbps: 0, DropKbps: 0},
		}
		_, _, skip := s.ComputeBid(ctx)
		if !skip {
			t.Error("expected skip=true for zero traffic")
		}
	})

	t.Run("price is reservation * multiplier regardless of clearing history", func(t *testing.T) {
		s := strategy.NewPriceInsensitive() // default multiplier 10
		wantPrice := uint64(10 * 10)        // reservation(10) * multiplier(10) = 100

		cases := []int{0, 5, 50, 200}
		for _, clearing := range cases {
			ctx := strategy.BidContext{
				Scene:             scene,
				Metrics:           shared.FlowMetricsValue{ThroughputKbps: 8, DropKbps: 0},
				LastClearingPrice: clearing,
			}
			_, price, skip := s.ComputeBid(ctx)
			if skip {
				t.Errorf("clearing=%d: unexpected skip", clearing)
			}
			if price != wantPrice {
				t.Errorf("clearing=%d: price=%d, want %d", clearing, price, wantPrice)
			}
		}
	})

	t.Run("units are demand-aware (throughput + drop) * 1.05", func(t *testing.T) {
		s := strategy.NewPriceInsensitive()
		ctx := strategy.BidContext{
			Scene:   scene,
			Metrics: shared.FlowMetricsValue{ThroughputKbps: 6, DropKbps: 4},
		}
		units, _, skip := s.ComputeBid(ctx)
		if skip {
			t.Fatal("unexpected skip")
		}
		// floor((6+4) * 1.05) = 10
		if units < 10 {
			t.Errorf("units=%d, want >= 10", units)
		}
	})

	t.Run("custom multiplier via struct field", func(t *testing.T) {
		s := strategy.PriceInsensitive{PriceMultiplier: 5}
		ctx := strategy.BidContext{
			Scene:   scene,
			Metrics: shared.FlowMetricsValue{ThroughputKbps: 5, DropKbps: 0},
		}
		_, price, _ := s.ComputeBid(ctx)
		if price != 50 { // 10 * 5
			t.Errorf("price=%d, want 50", price)
		}
	})
}
