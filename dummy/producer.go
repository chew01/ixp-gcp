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
}

func NewDummyProducer(writer *kafka.Writer, scenario *scenario.Scenario) *DummyProducer {
	return &DummyProducer{
		switchID: scenario.Switches[0].ID,
		kafka:    writer,
		scenario: scenario,
	}
}

func (p *DummyProducer) Run(ctx context.Context) {
	for {
		windowStartNs := time.Now().UnixNano()
		time.Sleep(ProduceWindowSec * time.Second)
		windowEndNs := time.Now().UnixNano()

		var flows []shared.Flow
		for _, inPort := range p.scenario.Switches[0].IngressPorts {
			for _, ePort := range p.scenario.Switches[0].EgressPorts {
				f := shared.Flow{
					IngressPort: inPort,
					EgressPort:  ePort,
					Bytes:       uint64(RandRange(5e5, 2e6)),
				}
				flows = append(flows, f)
			}
		}

		r := shared.TelemetryRecord{
			SwitchID:      p.scenario.Switches[0].ID,
			WindowStartNS: windowStartNs,
			WindowEndNS:   windowEndNs,
			Flows:         flows,
		}

		key := fmt.Sprintf("%s|%d", p.scenario.Switches[0].ID, windowStartNs)
		value, err := json.Marshal(r)
		if err != nil {
			log.Fatal(err)
		}

		err = p.kafka.WriteMessages(ctx, kafka.Message{
			Key:   []byte(key),
			Value: value,
		})
		if err != nil {
			log.Printf("Failed to write message to Kafka: %v", err)
		}
		log.Printf("Produced %d flows", len(flows))
	}
}
