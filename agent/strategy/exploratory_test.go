package strategy_test

import (
	"testing"

	"github.com/chew01/ixp-gcp/agent/strategy"
	"github.com/chew01/ixp-gcp/shared"
	"github.com/chew01/ixp-gcp/shared/scenario"
)

func TestExploratory_ComputeBid(t *testing.T) {
	scene := &scenario.Scenario{
		ReservationPrice: 10,
	}
	metrics := shared.FlowMetricsValue{ThroughputKbps: 8, DropKbps: 2}

	t.Run("zero traffic — skip", func(t *testing.T) {
		s := &strategy.Exploratory{Alpha: 0.3, Epsilon: 5}
		ctx := strategy.BidContext{
			Scene:   scene,
			Metrics: shared.FlowMetricsValue{ThroughputKbps: 0, DropKbps: 0},
		}
		_, _, skip := s.ComputeBid(ctx)
		if !skip {
			t.Error("expected skip=true for zero traffic")
		}
	})

	t.Run("first round seeds EMA from clearing price", func(t *testing.T) {
		s := &strategy.Exploratory{Alpha: 0.3, Epsilon: 5}
		ctx := strategy.BidContext{
			Scene:             scene,
			Metrics:           metrics,
			LastClearingPrice: 20,
		}
		_, price, skip := s.ComputeBid(ctx)
		if skip {
			t.Fatal("unexpected skip")
		}
		// EMA initialized to 20; price = max(10, floor(20)+5) = 25.
		if price != 25 {
			t.Errorf("price=%d, want 25 (20 + epsilon 5)", price)
		}
	})

	t.Run("zero clearing price on first round — price falls back to reservation", func(t *testing.T) {
		s := &strategy.Exploratory{Alpha: 0.3, Epsilon: 5}
		ctx := strategy.BidContext{
			Scene:             scene,
			Metrics:           metrics,
			LastClearingPrice: 0,
		}
		_, price, _ := s.ComputeBid(ctx)
		// EMA = 0; ema+epsilon = 5 < reservation(10) → price = 10.
		if price != 10 {
			t.Errorf("price=%d, want 10 (reservation floor)", price)
		}
	})

	t.Run("EMA stable when repeated identical clearing price", func(t *testing.T) {
		s := &strategy.Exploratory{Alpha: 0.3, Epsilon: 5}
		ctx := strategy.BidContext{
			Scene:             scene,
			Metrics:           metrics,
			LastClearingPrice: 20,
		}
		var lastPrice uint64
		for i := 0; i < 10; i++ {
			_, lastPrice, _ = s.ComputeBid(ctx)
		}
		// With stable clearing=20, EMA stays at 20; price stays at 25.
		if lastPrice != 25 {
			t.Errorf("price=%d, want 25 after stable clearing", lastPrice)
		}
	})

	t.Run("EMA converges toward stable price after several rounds", func(t *testing.T) {
		// Start fresh, then drive clearing price of 30 for many rounds.
		// EMA should converge to 30; final price = 30 + epsilon(5) = 35.
		s := &strategy.Exploratory{Alpha: 0.3, Epsilon: 5}
		ctx := strategy.BidContext{
			Scene:             scene,
			Metrics:           metrics,
			LastClearingPrice: 30,
		}
		var lastPrice uint64
		for i := 0; i < 30; i++ {
			_, lastPrice, _ = s.ComputeBid(ctx)
		}
		if lastPrice != 35 {
			t.Errorf("price=%d, want 35 after convergence to clearing=30", lastPrice)
		}
	})

	t.Run("zero clearing price after initialization does not update EMA", func(t *testing.T) {
		s := &strategy.Exploratory{Alpha: 0.3, Epsilon: 5}
		// Seed with clearing=20.
		seedCtx := strategy.BidContext{Scene: scene, Metrics: metrics, LastClearingPrice: 20}
		s.ComputeBid(seedCtx)

		// Subsequent call with clearing=0 should not change EMA.
		ctx := strategy.BidContext{Scene: scene, Metrics: metrics, LastClearingPrice: 0}
		_, price, _ := s.ComputeBid(ctx)
		// EMA still 20; price = 25.
		if price != 25 {
			t.Errorf("price=%d, want 25 (EMA unchanged when clearing=0)", price)
		}
	})

	t.Run("price floored at reservation_price", func(t *testing.T) {
		// epsilon=1, reservation=10; even with low EMA, price >= reservation.
		s := &strategy.Exploratory{Alpha: 0.3, Epsilon: 1}
		ctx := strategy.BidContext{
			Scene:             scene,
			Metrics:           metrics,
			LastClearingPrice: 5, // EMA=5; price = max(10, 5+1=6) = 10
		}
		_, price, _ := s.ComputeBid(ctx)
		if price != 10 {
			t.Errorf("price=%d, want 10 (reservation floor)", price)
		}
	})
}
