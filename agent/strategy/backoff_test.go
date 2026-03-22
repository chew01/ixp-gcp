package strategy_test

import (
	"testing"

	"github.com/chew01/ixp-gcp/agent/strategy"
	"github.com/chew01/ixp-gcp/shared"
	"github.com/chew01/ixp-gcp/shared/scenario"
)

func TestBackoff_ComputeBid(t *testing.T) {
	scene := &scenario.Scenario{
		ReservationPrice: 10,
	}
	metrics := shared.FlowMetricsValue{ThroughputKbps: 8, DropKbps: 0}

	// Helper to create a Backoff with known parameters (no env deps in tests).
	newBackoff := func(threshold, expensivePrice int) *strategy.Backoff {
		return &strategy.Backoff{
			BackoffThreshold:  threshold,
			ExpensivePrice:    expensivePrice,
			CurrentMultiplier: 1.0,
		}
	}

	t.Run("zero traffic — skip", func(t *testing.T) {
		s := newBackoff(3, 20)
		ctx := strategy.BidContext{
			Scene:   scene,
			Metrics: shared.FlowMetricsValue{ThroughputKbps: 0, DropKbps: 0},
		}
		_, _, skip := s.ComputeBid(ctx)
		if !skip {
			t.Error("expected skip=true for zero traffic")
		}
	})

	t.Run("below threshold — multiplier stays at 1.0", func(t *testing.T) {
		s := newBackoff(3, 20)
		// Two expensive rounds — below threshold of 3, no backoff yet.
		for i := 0; i < 2; i++ {
			ctx := strategy.BidContext{
				Scene:             scene,
				Metrics:           metrics,
				LastClearingPrice: 25, // > expensivePrice(20)
			}
			_, price, _ := s.ComputeBid(ctx)
			if price != 25 { // 25 * 1.0 = 25, which is > reservation
				t.Errorf("round %d: price=%d, want 25 (no backoff yet)", i+1, price)
			}
		}
	})

	t.Run("multiplier halves at and beyond backoff threshold", func(t *testing.T) {
		s := newBackoff(3, 20)
		clearingPrice := 40

		// Rounds 1–2: expensive but below threshold, full price.
		for i := 0; i < 2; i++ {
			ctx := strategy.BidContext{Scene: scene, Metrics: metrics, LastClearingPrice: clearingPrice}
			_, price, _ := s.ComputeBid(ctx)
			want := uint64(clearingPrice)
			if price != want {
				t.Errorf("round %d: price=%d, want %d", i+1, price, want)
			}
		}

		// Round 3: consecutiveExpensive reaches threshold → multiplier halves to 0.5.
		ctx := strategy.BidContext{Scene: scene, Metrics: metrics, LastClearingPrice: clearingPrice}
		_, price, _ := s.ComputeBid(ctx)
		want := uint64(float64(clearingPrice) * 0.5) // 20
		if price != want {
			t.Errorf("round 3: price=%d, want %d (half of clearing)", price, want)
		}

		// Round 4: still expensive, multiplier would halve again but floors at 0.5.
		ctx = strategy.BidContext{Scene: scene, Metrics: metrics, LastClearingPrice: clearingPrice}
		_, price, _ = s.ComputeBid(ctx)
		if price != want { // still 20 (floor)
			t.Errorf("round 4: price=%d, want %d (floor at 0.5)", price, want)
		}
	})

	t.Run("multiplier resets after cheap round", func(t *testing.T) {
		s := newBackoff(3, 20)
		clearingPrice := 40

		// Drive to backoff state.
		for i := 0; i < 3; i++ {
			ctx := strategy.BidContext{Scene: scene, Metrics: metrics, LastClearingPrice: clearingPrice}
			s.ComputeBid(ctx)
		}

		// One cheap round resets multiplier.
		cheapCtx := strategy.BidContext{Scene: scene, Metrics: metrics, LastClearingPrice: 15} // <= expensivePrice
		_, price, _ := s.ComputeBid(cheapCtx)
		// After reset, multiplier=1.0; price = max(reservation, 15*1.0) = 15.
		if price != 15 {
			t.Errorf("after cheap round: price=%d, want 15", price)
		}

		// Next expensive round starts fresh accumulation — no immediate backoff.
		expCtx := strategy.BidContext{Scene: scene, Metrics: metrics, LastClearingPrice: clearingPrice}
		_, price, _ = s.ComputeBid(expCtx)
		if price != uint64(clearingPrice) {
			t.Errorf("first expensive after reset: price=%d, want %d", price, clearingPrice)
		}
	})

	t.Run("price floored at reservation_price", func(t *testing.T) {
		s := newBackoff(1, 5) // threshold=1, expensivePrice=5
		// One expensive round drives consecutive to threshold, multiplier halves.
		// If clearing price * 0.5 < reservation, price should clamp to reservation.
		ctx := strategy.BidContext{Scene: scene, Metrics: metrics, LastClearingPrice: 12}
		_, price, _ := s.ComputeBid(ctx) // 12 * 0.5 = 6 < reservation(10)? No, 6 < 10 → price=10
		if price != 10 {
			t.Errorf("price=%d, want 10 (reservation floor)", price)
		}
	})

	t.Run("auto expensive_price defaults to 2x reservation", func(t *testing.T) {
		// expensivePrice=0 means auto (2 * reservation_price = 20).
		s := &strategy.Backoff{
			BackoffThreshold:  1,
			ExpensivePrice:    0, // auto
			CurrentMultiplier: 1.0,
		}
		// clearing=25 > 2*10=20 → expensive.
		ctx := strategy.BidContext{Scene: scene, Metrics: metrics, LastClearingPrice: 25}
		_, _, skip := s.ComputeBid(ctx)
		if skip {
			t.Fatal("unexpected skip")
		}
		// consecutiveExpensive reached threshold(1), multiplier halved to 0.5.
		ctx2 := strategy.BidContext{Scene: scene, Metrics: metrics, LastClearingPrice: 25}
		_, price, _ := s.ComputeBid(ctx2)
		// 25 * 0.5 = 12 (> reservation 10)
		if price != 12 {
			t.Errorf("price=%d, want 12", price)
		}
	})
}
