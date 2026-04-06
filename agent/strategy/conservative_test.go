package strategy_test

import (
	"testing"

	"github.com/chew01/ixp-gcp/agent/strategy"
	"github.com/chew01/ixp-gcp/shared"
	"github.com/chew01/ixp-gcp/shared/scenario"
)

func TestConservative_ComputeBid(t *testing.T) {
	scene := &scenario.Scenario{
		ReservationPrice: 10,
	}
	s := strategy.Conservative{}

	tests := []struct {
		name              string
		throughputKbps    float64
		dropKbps          float64
		lastClearingPrice int
		wantSkip          bool
		wantUnitsAtLeast  uint64
		wantPrice         uint64
	}{
		{
			name:           "zero traffic and zero drops — skip",
			throughputKbps: 0,
			dropKbps:       0,
			wantSkip:       true,
		},
		{
			name:             "positive throughput — bid at 110%",
			throughputKbps:   10,
			dropKbps:         0,
			wantSkip:         false,
			wantUnitsAtLeast: 11, // floor(10 * 1.1) = 11
			wantPrice:        10, // reservation price (no clearing history)
		},
		{
			name:              "positive throughput with clearing history — price follows clearing",
			throughputKbps:    8,
			dropKbps:          0,
			lastClearingPrice: 20,
			wantSkip:          false,
			wantUnitsAtLeast:  8, // floor(8 * 1.1) = 8
			wantPrice:         20,
		},
		{
			name:             "traffic with drops — still bids on throughput only",
			throughputKbps:   6,
			dropKbps:         4,
			wantSkip:         false,
			wantUnitsAtLeast: 6, // floor(6 * 1.1) = 6
			wantPrice:        10,
		},
		{
			name:             "zero throughput but non-zero drops — bid minimum unit",
			throughputKbps:   0,
			dropKbps:         5,
			wantSkip:         false,
			wantUnitsAtLeast: 1,
			wantPrice:        10,
		},
		{
			name:              "clearing price lower than reservation — use reservation",
			throughputKbps:    5,
			dropKbps:          0,
			lastClearingPrice: 3,
			wantSkip:          false,
			wantUnitsAtLeast:  5,
			wantPrice:         10,
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
				LastClearingPrice: tc.lastClearingPrice,
			}

			units, price, skip := s.ComputeBid(ctx)

			if skip != tc.wantSkip {
				t.Errorf("skip=%v, want %v", skip, tc.wantSkip)
			}
			if tc.wantSkip {
				return
			}
			if units < tc.wantUnitsAtLeast {
				t.Errorf("units=%d, want >= %d", units, tc.wantUnitsAtLeast)
			}
			if price != tc.wantPrice {
				t.Errorf("price=%d, want %d", price, tc.wantPrice)
			}
		})
	}
}
