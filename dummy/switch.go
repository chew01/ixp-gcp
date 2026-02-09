package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/chew01/ixp-gcp/shared"
	"github.com/segmentio/kafka-go"
)

type DummySwitch struct {
	reader *kafka.Reader
}

func NewDummySwitch(reader *kafka.Reader) *DummySwitch {
	return &DummySwitch{
		reader: reader,
	}
}

func (s *DummySwitch) Run(ctx context.Context) {
	for {
		msg, err := s.reader.ReadMessage(ctx)
		if err != nil {
			log.Println("Error reading message:", err)
			continue
		}

		var record shared.AuctionResultRecord
		if err := json.Unmarshal(msg.Value, &record); err != nil {
			log.Println("Error parsing JSON:", err)
			continue
		}

		log.Printf("Auction result: %d kbps (%d->%d)", record.BandwidthKbps, record.IngressPort, record.EgressPort)
	}
}
