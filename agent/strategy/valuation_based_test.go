package strategy_test

import (
	"testing"

	"github.com/chew01/ixp-gcp/agent/strategy"
	"github.com/chew01/ixp-gcp/shared"
	"github.com/chew01/ixp-gcp/shared/scenario"
)

func TestValuationBased_ComputeBid(t *testing.T) {
	scene := &scenario.Scenario{
		ReservationPrice: 50,
	}
	s := strategy.ValuationBased{}

	tests := []struct {
		name             string
		throughputKbps   float64
		dropKbps         float64
		valuationPerUnit int
		wantSkip         bool
		wantPrice        uint64
		wantUnitsAtLeast uint64
	}{
		{
			name:             "normal flow: bid equals valuation",
			throughputKbps:   40,
			dropKbps:         5,
			valuationPerUnit: 500,
			wantSkip:         false,
			wantPrice:        500,
			wantUnitsAtLeast: 1,
		},
		{
			name:           "zero demand: skip",
			throughputKbps: 0,
			dropKbps:       0,
			valuationPerUnit: 500,
			wantSkip:       true,
		},
		{
			name:             "valuation below reservation price: floor is applied",
			throughputKbps:   10,
			dropKbps:         0,
			valuationPerUnit: 20, // below reservation_price of 50
			wantSkip:         false,
			wantPrice:        50, // floored to reservation_price
			wantUnitsAtLeast: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := strategy.BidContext{
				Scene: scene,
				Metrics: shared.FlowMetricsValue{
					ThroughputKbps: tc.throughputKbps,
					DropKbps:       tc.dropKbps,
				},
				ValuationPerUnit: tc.valuationPerUnit,
			}

			units, price, skip := s.ComputeBid(ctx)

			if skip != tc.wantSkip {
				t.Errorf("skip=%v, want %v", skip, tc.wantSkip)
			}
			if tc.wantSkip {
				return
			}
			if price != tc.wantPrice {
				t.Errorf("price=%d, want %d", price, tc.wantPrice)
			}
			if units < tc.wantUnitsAtLeast {
				t.Errorf("units=%d, want >= %d", units, tc.wantUnitsAtLeast)
			}
		})
	}
}
