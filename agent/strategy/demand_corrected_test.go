package strategy_test

import (
	"testing"

	"github.com/chew01/ixp-gcp/agent/strategy"
	"github.com/chew01/ixp-gcp/shared"
	"github.com/chew01/ixp-gcp/shared/scenario"
)

func TestDemandCorrected_ComputeBid(t *testing.T) {
	scene := &scenario.Scenario{
		ReservationPrice: 10,
	}
	s := strategy.DemandCorrected{}

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
			name:           "zero throughput and zero drops — skip",
			throughputKbps: 0,
			dropKbps:       0,
			wantSkip:       true,
		},
		{
			name:             "positive throughput no drops — degrades gracefully to conservative-like behaviour",
			throughputKbps:   10,
			dropKbps:         0,
			wantSkip:         false,
			wantUnitsAtLeast: 10, // floor(10 * 1.05) = 10
			wantPrice:        10,
		},
		{
			name:             "moderate drop rate — bids for full demand",
			throughputKbps:   7,
			dropKbps:         3,
			wantSkip:         false,
			wantUnitsAtLeast: 10, // floor((7+3) * 1.05) = 10
			wantPrice:        10,
		},
		{
			name:             "extreme drop rate (>50%) — bids for full demand",
			throughputKbps:   4,
			dropKbps:         6,
			wantSkip:         false,
			wantUnitsAtLeast: 10, // floor((4+6) * 1.05) = 10
			wantPrice:        10,
		},
		{
			name:              "clearing price above reservation — price follows clearing",
			throughputKbps:    8,
			dropKbps:          2,
			lastClearingPrice: 25,
			wantSkip:          false,
			wantUnitsAtLeast:  10,
			wantPrice:         25,
		},
		{
			name:              "clearing price below reservation — use reservation",
			throughputKbps:    5,
			dropKbps:          1,
			lastClearingPrice: 3,
			wantSkip:          false,
			wantUnitsAtLeast:  6,
			wantPrice:         10,
		},
		{
			name:           "zero throughput with drops only — bids for drop demand",
			throughputKbps: 0,
			dropKbps:       8,
			wantSkip:       false,
			wantUnitsAtLeast: 8, // floor(8 * 1.05) = 8
			wantPrice:      10,
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
