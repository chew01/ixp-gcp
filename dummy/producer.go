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
}

func NewDummyProducer(writer *kafka.Writer, sc *scenario.Scenario) *DummyProducer {
	return &DummyProducer{
		switchID: sc.Switches[0].ID,
		kafka:    writer,
		scenario: sc,
		counters: make(map[string]struct {
			rx uint64
			tx uint64
		}),
		traffic: sc.Traffic.WithDefaults(),
	}
}

// bytesPerInterval returns the rx and tx byte increase for one telemetry interval.
// For steady/spike patterns tx == rx — the switch enforces drops from allocation,
// so the producer reports ingress faithfully without simulating drops itself.
func (p *DummyProducer) bytesPerInterval(intervalSeconds float64) (rx uint64, tx uint64) {
	switch p.traffic.Pattern {
	case scenario.TrafficPatternSteady, scenario.TrafficPatternSpike:
		targetKbps := p.traffic.RateKbps
		if p.traffic.Pattern == scenario.TrafficPatternSpike && p.intervalCount >= p.traffic.SpikeAfterIntervals {
			targetKbps = p.traffic.SpikeRateKbps
		}
		// kbps * 1000 = bps; bps / 8 = bytes/s; * intervalSeconds = bytes/interval
		b := uint64(float64(targetKbps) * 1000.0 / 8.0 * intervalSeconds)
		b = max(b, 1)
		return b, b
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

	log.Printf("producer started: pattern=%s rate=%dkbps spike_after=%d spike_rate=%dkbps interval=%.1fs",
		p.traffic.Pattern, p.traffic.RateKbps, p.traffic.SpikeAfterIntervals, p.traffic.SpikeRateKbps, intervalSeconds)

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

					rxIncrease, txIncrease := p.bytesPerInterval(intervalSeconds)
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
