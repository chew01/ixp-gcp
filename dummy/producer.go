package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/segmentio/kafka-go"
)

type DummyProducer struct {
	topic    string
	switchID string
	kafka    *kafka.Writer
}

type Flow struct {
	IngressPort int    `json:"ingress_port"`
	EgressPort  int    `json:"egress_port"`
	Bytes       uint64 `json:"bytes"`
}

type Record struct {
	SchemaVersion int    `json:"schema_version"`
	SwitchID      string `json:"switch_id"`
	WindowStartNS int64  `json:"window_start_ns"`
	WindowEndNS   int64  `json:"window_end_ns"`
	Flows         []Flow `json:"flows"`
}

func NewDummyProducer(writer *kafka.Writer) *DummyProducer {
	return &DummyProducer{
		topic:    Topic,
		switchID: SwitchId,
		kafka:    writer,
	}
}

func (p *DummyProducer) Run(ctx context.Context) {
	for {
		windowStartNs := time.Now().UnixNano()
		time.Sleep(ProduceWindowSec * time.Second)
		windowEndNs := time.Now().UnixNano()

		flows := make([]Flow, FlowsPerProduceWindow)
		for i := 0; i < FlowsPerProduceWindow; i++ {
			f := Flow{
				IngressPort: RandRange(1, 4),
				EgressPort:  RandRange(5, 8),
				Bytes:       uint64(RandRange(5e5, 2e6)),
			}
			flows[i] = f
		}

		r := Record{
			SchemaVersion: 1,
			SwitchID:      SwitchId,
			WindowStartNS: windowStartNs,
			WindowEndNS:   windowEndNs,
			Flows:         flows,
		}

		key := fmt.Sprintf("%s|%d", SwitchId, windowStartNs)
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
