package main

import (
	"context"
	"fmt"
	"log"
	"time"

	pb "github.com/chew01/ixp-gcp/shared/proto/pb"
	"github.com/chew01/ixp-gcp/shared/scenario"
	"github.com/segmentio/kafka-go"
	"google.golang.org/protobuf/proto"
)

type DummyProducer struct {
	switchID string
	kafka    *kafka.Writer
	scenario *scenario.Scenario
	counters map[string]struct {
		rx uint64
		tx uint64
	}
	traffic       scenario.TrafficConfig
	intervalCount int
	allocations   *AllocationTable
}

func NewDummyProducer(writer *kafka.Writer, sc *scenario.Scenario, allocations *AllocationTable) *DummyProducer {
	return &DummyProducer{
		switchID: sc.Switches[0].ID,
		kafka:    writer,
		scenario: sc,
		counters: make(map[string]struct {
			rx uint64
			tx uint64
		}),
		traffic:     sc.Traffic.WithDefaults(),
		allocations: allocations,
	}
}

// spikeTriggered returns true once the elapsed wall-clock time has passed
// spike_after_intervals full auction cycles. This makes spike_after_intervals
// independent of telemetry_interval — e.g. spike_after_intervals=5 with a 30s
// auction interval fires after 150s regardless of how fast telemetry ticks.
func (p *DummyProducer) spikeTriggered(telemetryIntervalSeconds float64) bool {
	auctionInterval, err := time.ParseDuration(p.scenario.AuctionInterval)
	if err != nil || auctionInterval <= 0 {
		auctionInterval = 30 * time.Second
	}
	elapsedSeconds := float64(p.intervalCount) * telemetryIntervalSeconds
	spikeAfterSeconds := float64(p.traffic.SpikeAfterIntervals) * auctionInterval.Seconds()
	return elapsedSeconds >= spikeAfterSeconds
}

// rxForInterval returns the ingress demand in bytes for one telemetry interval.
func (p *DummyProducer) rxForInterval(intervalSeconds float64) uint64 {
	targetKbps := p.traffic.RateKbps
	if p.traffic.Pattern == scenario.TrafficPatternSpike && p.spikeTriggered(intervalSeconds) {
		targetKbps = p.traffic.SpikeRateKbps
	}
	// kbps * 1000 = bps; bps / 8 = bytes/s; * intervalSeconds = bytes/interval
	rx := uint64(float64(targetKbps) * 1000.0 / 8.0 * intervalSeconds)
	return max(rx, 1)
}

func (p *DummyProducer) Run(ctx context.Context) {
	interval, err := time.ParseDuration(p.scenario.TelemetryInterval)
	if err != nil {
		log.Fatal(err)
	}
	intervalSeconds := interval.Seconds()

	auctionInterval, _ := time.ParseDuration(p.scenario.AuctionInterval)
	spikeAfterSeconds := float64(p.traffic.SpikeAfterIntervals) * auctionInterval.Seconds()
	log.Printf("producer started: pattern=%s rate=%dkbps spike_after=%d auction-intervals (%.0fs) spike_rate=%dkbps telemetry_interval=%.1fs",
		p.traffic.Pattern, p.traffic.RateKbps, p.traffic.SpikeAfterIntervals, spikeAfterSeconds, p.traffic.SpikeRateKbps, intervalSeconds)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	sw := p.scenario.Switches[0]
	maxCapacityBytesPerSecond := float64(sw.MaxCapacity) * 1000.0 / 8.0

	type flowSlot struct {
		inPort, ePort uint32
		rx            uint64
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.intervalCount++

			maxCapacityBytes := uint64(maxCapacityBytesPerSecond * intervalSeconds)

			// Phase 1: compute ingress demand for every flow.
			slots := make([]flowSlot, 0, len(sw.IngressPorts)*len(sw.EgressPorts))
			for _, inPort := range sw.IngressPorts {
				for _, ePort := range sw.EgressPorts {
					var rx uint64
					if p.traffic.Pattern == scenario.TrafficPatternRandom {
						rx = uint64(RandRange(10_000, 200_000))
					} else {
						rx = p.rxForInterval(intervalSeconds)
					}
					slots = append(slots, flowSlot{inPort, ePort, rx})
				}
			}

			// Phase 2: compute guaranteed egress (min(rx, allocation)) per flow.
			// Before the first auction result (allocKbps == 0), pass through at
			// line-rate — the switch forwards everything until it has guidance.
			guaranteed := make([]uint64, len(slots))
			excess := make([]uint64, len(slots))
			var totalGuaranteed, totalExcess uint64

			for i, s := range slots {
				allocKbps := p.allocations.Get(uint64(s.inPort), uint64(s.ePort))
				if allocKbps == 0 {
					guaranteed[i] = s.rx
				} else {
					cap := uint64(float64(allocKbps) * 1000.0 / 8.0 * intervalSeconds)
					if s.rx <= cap {
						guaranteed[i] = s.rx
					} else {
						guaranteed[i] = cap
						excess[i] = s.rx - cap
						totalExcess += excess[i]
					}
				}
				totalGuaranteed += guaranteed[i]
			}

			// Phase 3: distribute spare egress capacity as best-effort among
			// flows whose demand exceeds their allocation.
			var spareCapacity uint64
			if totalGuaranteed < maxCapacityBytes {
				spareCapacity = maxCapacityBytes - totalGuaranteed
			}

			txIncrease := make([]uint64, len(slots))
			for i := range slots {
				be := uint64(0)
				if totalExcess > 0 && spareCapacity > 0 && excess[i] > 0 {
					be = uint64(float64(excess[i]) / float64(totalExcess) * float64(spareCapacity))
					if be > excess[i] {
						be = excess[i]
					}
				}
				txIncrease[i] = guaranteed[i] + be
			}

			// Phase 4: update counters and emit telemetry.
			var messages []kafka.Message
			for i, s := range slots {
				key := fmt.Sprintf("%d-%d", s.inPort, s.ePort)
				state := p.counters[key]

				var txInc uint64
				if p.traffic.Pattern == scenario.TrafficPatternRandom {
					// Random pattern: use rx minus a small random drop (independent of allocation).
					txInc = s.rx - uint64(RandRange(0, 5_000))
				} else {
					txInc = txIncrease[i]
				}

				state.rx += s.rx
				state.tx += txInc
				p.counters[key] = state

				flow := &pb.Flow{
					IngressPort:  proto.Uint32(s.inPort),
					SourceVlanid: proto.Uint32(10),
					EgressPort:   proto.Uint32(s.ePort),
					DestVlanid:   proto.Uint32(20),
				}

				keyBytes, err := proto.Marshal(flow)
				if err != nil {
					log.Fatal(err)
				}

				report := &pb.TelemetryReport{
					FlowId:      flow,
					RxByteCount: state.rx,
					TxByteCount: state.tx,
				}
				valueBytes, err := proto.Marshal(report)
				if err != nil {
					log.Fatal(err)
				}

				messages = append(messages, kafka.Message{
					Key:   keyBytes,
					Value: valueBytes,
				})
			}

			if err := p.kafka.WriteMessages(ctx, messages...); err != nil {
				log.Printf("failed to write %d messages to Kafka: %v", len(messages), err)
			} else {
				log.Printf("produced %d records (interval=%d pattern=%s)", len(messages), p.intervalCount, p.traffic.Pattern)
			}
		}
	}
}
