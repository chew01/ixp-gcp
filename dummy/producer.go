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
	flowsProduced, _ := localotel.Meter.Int64Counter("flows_produced_total", metric.WithDescription("Total number of flows produced"))

	interval, err := time.ParseDuration(p.scenario.TelemetryInterval)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse telemetry interval", "error", err, "interval", p.scenario.TelemetryInterval)
		return // or log.Fatal(err) if you want to exit
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ctx, span := localotel.Tracer.Start(ctx, "produce-telemetry", trace.WithNewRoot())
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
						span.SetStatus(codes.Error, "failed to marshal telemetry record")
						span.RecordError(err)
						slog.ErrorContext(ctx, "Failed to marshal telemetry record", "error", err)
						continue // Skip this cycle on error
					}

					messages = append(messages, kafka.Message{
						Key:   []byte(p.switchID),
						Value: value,
					})
				}
			}

			if err := p.kafka.WriteMessages(ctx, messages...); err != nil {
				msg := fmt.Sprintf("Failed to write %d messages to Kafka: %v", len(messages), err)
				span.SetStatus(codes.Error, "failed to write telemetry to Kafka")
				span.RecordError(err)
				slog.ErrorContext(ctx, msg,
					"number_of_messages", len(messages),
					"error", err,
				)
				continue // Skip metrics/logging on error
			} else {
				flowsProduced.Add(ctx, int64(len(messages)), metric.WithAttributes(attribute.String("switch_id", p.scenario.Switches[0].ID)))
				msg := fmt.Sprintf("Produced %d records", len(messages))
				slog.DebugContext(ctx, msg,
					"number_of_messages", len(messages),
				)
				span.End()
			}
		}
	}
}
