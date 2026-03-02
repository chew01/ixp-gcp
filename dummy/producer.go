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
}

func NewDummyProducer(writer *kafka.Writer, scenario *scenario.Scenario) *DummyProducer {
	return &DummyProducer{
		switchID: scenario.Switches[0].ID,
		kafka:    writer,
		scenario: scenario,
		counters: make(map[string]struct {
			rx uint64
			tx uint64
		}),
	}
}

func (p *DummyProducer) Run(ctx context.Context) {
	interval, err := time.ParseDuration(p.scenario.TelemetryInterval)
	if err != nil {
		log.Fatal(err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:

			var messages []kafka.Message

			for _, inPort := range p.scenario.Switches[0].IngressPorts {
				for _, ePort := range p.scenario.Switches[0].EgressPorts {

					flowKey := fmt.Sprintf("%d-%d", inPort, ePort)

					// Get previous counters
					state := p.counters[flowKey]

					// Simulate traffic increase per interval
					rxIncrease := uint64(RandRange(10_000, 200_000))
					txIncrease := rxIncrease - uint64(RandRange(0, 5_000)) // simulate drops

					// Monotonic increment (wrap happens automatically at 2^64)
					state.rx += rxIncrease
					state.tx += txIncrease

					// Save back
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
				log.Printf("Failed to write %d messages to Kafka: %v", len(messages), err)
			} else {
				log.Printf("Produced %d records", len(messages))
			}
		}
	}
}
