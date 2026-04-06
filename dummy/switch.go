package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/chew01/ixp-gcp/shared"
	"github.com/chew01/ixp-gcp/shared/otel"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/metric"
)

type DummySwitch struct {
	reader      *kafka.Reader
	allocations *AllocationTable
}

func NewDummySwitch(reader *kafka.Reader, allocations *AllocationTable) *DummySwitch {
	return &DummySwitch{
		reader:      reader,
		allocations: allocations,
	}
}

func (s *DummySwitch) Run(ctx context.Context) {
	configsAccepted, _ := otel.Meter.Int64Counter("configs_accepted_total", metric.WithDescription("Total number of configurations accepted"))

	for {
		_, span := otel.Tracer.Start(ctx, "accept-config")
		msg, err := s.reader.ReadMessage(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "Error reading message", "error", err)
			span.End()
			continue
		}

		var record shared.AuctionResultRecord
		if err := json.Unmarshal(msg.Value, &record); err != nil {
			slog.ErrorContext(ctx, "Error parsing JSON", "error", err)
			span.End()
			continue
		}

		s.allocations.Set(record.IngressPort, record.EgressPort, record.BandwidthKbps)
		configsAccepted.Add(ctx, 1)
		log_msg := fmt.Sprintf("Auction result: %d kbps (%d->%d)", record.BandwidthKbps, record.IngressPort, record.EgressPort)
		slog.DebugContext(ctx, log_msg,
			"bandwidthKbps", record.BandwidthKbps,
			"ingressPort", record.IngressPort,
			"egressPort", record.EgressPort,
		)
		span.End()
	}
}
