package strategy_test

import (
	"testing"

	"github.com/chew01/ixp-gcp/agent/strategy"
	"github.com/chew01/ixp-gcp/shared"
	"github.com/chew01/ixp-gcp/shared/scenario"
)

func TestThroughputOptimizer_ComputeBid(t *testing.T) {
	scene := &scenario.Scenario{
		ReservationPrice: 50,
	}

	// Helper to build a context with a seeded clearing price history.
	makeCtx := func(throughput, drop float64, valuation, clearing int) strategy.BidContext {
		return strategy.BidContext{
			Scene: scene,
			Metrics: shared.FlowMetricsValue{
				ThroughputKbps: throughput,
				DropKbps:       drop,
			},
			ValuationPerUnit:  valuation,
			LastClearingPrice: clearing,
		}
	}

	// priceThreshold=0.8, highDemandKbps=80 (defaults)
	t.Run("high demand AND cheap market: bid valuation", func(t *testing.T) {
		s := strategy.NewThroughputOptimizer(map[string]string{
			"price_threshold":  "0.8",
			"high_demand_kbps": "80",
			"price_window":     "1",
		})
		// Seed history with an expensive price, then present a cheap clearing.
		s.ComputeBid(makeCtx(50, 50, 500, 200)) // history: [200]
		// Now demand=180 (high), lastClearing=100 < 200*0.8=160 (cheap)
		_, price, skip := s.ComputeBid(makeCtx(90, 90, 500, 100))
		if skip {
			t.Fatal("expected bid, got skip")
		}
		// Price should be exactly valuation (500), floored to reservation after budget decay
		// (no budget configured so decay=1.0 → price stays at valuation)
		if price != 500 {
			t.Errorf("price=%d, want 500 (valuation)", price)
		}
	})

	t.Run("high demand AND expensive market: follow market price", func(t *testing.T) {
		s := strategy.NewThroughputOptimizer(map[string]string{
			"price_threshold":  "0.8",
			"high_demand_kbps": "80",
			"price_window":     "1",
		})
		// Seed: expectedPrice=100, lastClearing=150 (not cheap: 150 >= 100*0.8=80)
		s.ComputeBid(makeCtx(50, 50, 500, 100))
		_, price, skip := s.ComputeBid(makeCtx(90, 90, 500, 150))
		if skip {
			t.Fatal("expected bid, got skip")
		}
		if price != 150 {
			t.Errorf("price=%d, want 150 (last clearing)", price)
		}
	})

	t.Run("low demand AND cheap market: opportunistic bid", func(t *testing.T) {
		s := strategy.NewThroughputOptimizer(map[string]string{
			"price_threshold":  "0.8",
			"high_demand_kbps": "80",
			"price_window":     "1",
		})
		// Seed: expectedPrice=200, lastClearing=100 (cheap: 100 < 200*0.8=160)
		s.ComputeBid(makeCtx(20, 20, 500, 200))
		_, price, skip := s.ComputeBid(makeCtx(20, 20, 500, 100))
		if skip {
			t.Fatal("expected bid, got skip")
		}
		// Expect ~90% of lastClearing = 90, floored at reservation (50)
		want := uint64(90)
		if price != want {
			t.Errorf("price=%d, want ~%d (90%% of clearing)", price, want)
		}
	})

	t.Run("low demand AND expensive market: conserve budget", func(t *testing.T) {
		s := strategy.NewThroughputOptimizer(map[string]string{
			"price_threshold":  "0.8",
			"high_demand_kbps": "80",
			"price_window":     "1",
		})
		// Seed: expectedPrice=100, lastClearing=200 (expensive: 200 >= 100*0.8=80)
		s.ComputeBid(makeCtx(20, 20, 500, 100))
		_, price, skip := s.ComputeBid(makeCtx(20, 20, 500, 200))
		if skip {
			t.Fatal("expected bid, got skip")
		}
		// Expect reservation_price = 50
		if price != 50 {
			t.Errorf("price=%d, want 50 (reservation price)", price)
		}
	})

	t.Run("zero demand: skip", func(t *testing.T) {
		s := strategy.NewThroughputOptimizer(nil)
		_, _, skip := s.ComputeBid(makeCtx(0, 0, 500, 100))
		if !skip {
			t.Error("expected skip for zero demand")
		}
	})
}
