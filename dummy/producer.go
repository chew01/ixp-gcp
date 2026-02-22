package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/chew01/ixp-gcp/shared"
	localotel "github.com/chew01/ixp-gcp/shared/otel"
	"github.com/chew01/ixp-gcp/shared/scenario"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
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
	flowsProduced, _ := localotel.Meter.Int64Counter("flows_produced_total", metric.WithDescription("Total number of flows produced"))

	interval, err := time.ParseDuration(p.scenario.TelemetryInterval)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse telemetry interval", "error", err, "interval", p.scenario.TelemetryInterval)
		return // or log.Fatal(err) if you want to exit
	}
	for {
		ctx, span := localotel.Tracer.Start(ctx, "produce-telemetry", trace.WithNewRoot())
		windowStartNs := time.Now().UnixNano()
		time.Sleep(interval)
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
			span.SetStatus(codes.Error, "failed to marshal telemetry record")
			span.RecordError(err)
			slog.ErrorContext(ctx, "Failed to marshal telemetry record", "error", err)
			continue // Skip this cycle on error
		}

		err = p.kafka.WriteMessages(ctx, kafka.Message{
			Key:   []byte(key),
			Value: value,
		})
		if err != nil {
			span.SetStatus(codes.Error, "failed to write telemetry to Kafka")
			span.RecordError(err)
			slog.ErrorContext(ctx, "Failed to write telemetry to Kafka", "error", err, "topic", p.kafka.Topic, "key", key)
			continue // Skip metrics/logging on error
		}
		flowsProduced.Add(ctx, int64(len(flows)), metric.WithAttributes(attribute.String("switch_id", p.scenario.Switches[0].ID)))
		slog.DebugContext(ctx, "Produced telemetry flows",
			"flow_count", len(flows),
			"switch_id", p.scenario.Switches[0].ID,
			"window_start_ns", windowStartNs,
			"window_end_ns", windowEndNs,
		)
		span.End()
	}
}
