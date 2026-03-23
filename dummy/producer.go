package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/chew01/ixp-gcp/shared"
	"github.com/chew01/ixp-gcp/shared/scenario"
	"github.com/segmentio/kafka-go"
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

// bytesPerInterval returns the rx and tx byte increase for one telemetry interval.
// rx reflects ingress demand (what the sender is pushing). tx is capped by the
// current allocation for this flow so that the telemetry reports real drops.
func (p *DummyProducer) bytesPerInterval(intervalSeconds float64, ingressPort, egressPort uint64) (rx uint64, tx uint64) {
	switch p.traffic.Pattern {
	case scenario.TrafficPatternSteady, scenario.TrafficPatternSpike:
		targetKbps := p.traffic.RateKbps
		if p.traffic.Pattern == scenario.TrafficPatternSpike && p.spikeTriggered(intervalSeconds) {
			targetKbps = p.traffic.SpikeRateKbps
		}
		// kbps * 1000 = bps; bps / 8 = bytes/s; * intervalSeconds = bytes/interval
		rxBytes := uint64(float64(targetKbps) * 1000.0 / 8.0 * intervalSeconds)
		rxBytes = max(rxBytes, 1)

		// Cap egress by the latest allocation for this flow. Before the first
		// auction result arrives, allocKbps == 0 and we pass through uncapped
		// (switches forward at line-rate until the first auction result).
		txBytes := rxBytes
		if allocKbps := p.allocations.Get(ingressPort, egressPort); allocKbps > 0 {
			maxTx := uint64(float64(allocKbps) * 1000.0 / 8.0 * intervalSeconds)
			if txBytes > maxTx {
				txBytes = maxTx
			}
		}
		return rxBytes, txBytes
	default: // PatternRandom
		rxR := uint64(RandRange(10_000, 200_000))
		txR := rxR - uint64(RandRange(0, 5_000))
		return rxR, txR
	}
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

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.intervalCount++

			var messages []kafka.Message

			for _, inPort := range p.scenario.Switches[0].IngressPorts {
				for _, ePort := range p.scenario.Switches[0].EgressPorts {
					flowKey := fmt.Sprintf("%d-%d", inPort, ePort)
					state := p.counters[flowKey]

			rxIncrease, txIncrease := p.bytesPerInterval(intervalSeconds, uint64(inPort), uint64(ePort))
				state.rx += rxIncrease
				state.tx += txIncrease
					p.counters[flowKey] = state

					t := shared.TelemetryRecord{
						FlowID: shared.Flow{
							IngressPort:  inPort,
							EgressPort:   ePort,
							SourceVLANID: 10,
							DestVLANID:   20,
						},
						RxByteCount: state.rx,
						TxByteCount: state.tx,
					}

					value, err := json.Marshal(t)
					if err != nil {
						log.Fatal(err)
					}

					messages = append(messages, kafka.Message{
						Key:   []byte(p.switchID),
						Value: value,
					})
				}
			}

			if err := p.kafka.WriteMessages(ctx, messages...); err != nil {
				log.Printf("failed to write %d messages to Kafka: %v", len(messages), err)
			} else {
				log.Printf("produced %d records (interval=%d pattern=%s)", len(messages), p.intervalCount, p.traffic.Pattern)
			}
		}
	}
}
