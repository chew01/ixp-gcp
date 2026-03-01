package main

import (
	"context"
	"encoding/json"
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
}

func NewDummyProducer(writer *kafka.Writer, scenario *scenario.Scenario) *DummyProducer {
	return &DummyProducer{
		switchID: scenario.Switches[0].ID,
		kafka:    writer,
		scenario: scenario,
	}
}

func (p *DummyProducer) Run(ctx context.Context) {
	interval, err := time.ParseDuration(p.scenario.TelemetryInterval)
	if err != nil {
		log.Fatal(err)
	}

	for {
		time.Sleep(interval)
		var messages []kafka.Message

		for _, inPort := range p.scenario.Switches[0].IngressPorts {
			for _, ePort := range p.scenario.Switches[0].EgressPorts {
				rx := uint64(RandRange(1000, 100000))
				t := shared.TelemetryRecord{
					FlowID: shared.Flow{
						IngressPort:  inPort,
						EgressPort:   ePort,
						SourceVLANID: 10,
						DestVLANID:   20,
					},
					RxByteCount: rx,
					TxByteCount: rx,
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
