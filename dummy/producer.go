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

// spikeTriggered returns true once elapsed wall-clock time has passed
// spike_after_intervals full auction cycles, regardless of telemetry interval.
func (p *DummyProducer) spikeTriggered(telemetryIntervalSeconds float64) bool {
	auctionInterval, err := time.ParseDuration(p.scenario.AuctionInterval)
	if err != nil || auctionInterval <= 0 {
		auctionInterval = 30 * time.Second
	}
	elapsedSeconds := float64(p.intervalCount) * telemetryIntervalSeconds
	spikeAfterSeconds := float64(p.traffic.SpikeAfterIntervals) * auctionInterval.Seconds()
	return elapsedSeconds >= spikeAfterSeconds
}

// bytesPerInterval returns rx/tx byte deltas for one telemetry interval.
// tx is capped by the latest allocation for this flow, if available.
func (p *DummyProducer) bytesPerInterval(intervalSeconds float64, ingressPort, egressPort uint64) (rx uint64, tx uint64) {
	switch p.traffic.Pattern {
	case scenario.TrafficPatternSteady, scenario.TrafficPatternSpike:
		targetKbps := p.traffic.RateKbps
		if p.traffic.Pattern == scenario.TrafficPatternSpike && p.spikeTriggered(intervalSeconds) {
			targetKbps = p.traffic.SpikeRateKbps
		}
		rxBytes := uint64(float64(targetKbps) * 1000.0 / 8.0 * intervalSeconds)
		rxBytes = max(rxBytes, 1)

		txBytes := rxBytes
		if allocKbps := p.allocations.Get(ingressPort, egressPort); allocKbps > 0 {
			maxTx := uint64(float64(allocKbps) * 1000.0 / 8.0 * intervalSeconds)
			if txBytes > maxTx {
				txBytes = maxTx
			}
		}
		return rxBytes, txBytes
	default:
		rxR := uint64(RandRange(10_000, 200_000))
		txR := rxR - uint64(RandRange(0, 5_000))
		return rxR, txR
	}
}

func (p *DummyProducer) Run(ctx context.Context) {
	flowsProduced, _ := localotel.Meter.Int64Counter("flows_produced_total", metric.WithDescription("Total number of flows produced"))

	interval, err := time.ParseDuration(p.scenario.TelemetryInterval)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse telemetry interval", "error", err, "interval", p.scenario.TelemetryInterval)
		return
	}
	intervalSeconds := interval.Seconds()

	auctionInterval, _ := time.ParseDuration(p.scenario.AuctionInterval)
	spikeAfterSeconds := float64(p.traffic.SpikeAfterIntervals) * auctionInterval.Seconds()
	slog.InfoContext(ctx, "producer started",
		"pattern", p.traffic.Pattern,
		"rate_kbps", p.traffic.RateKbps,
		"spike_after_intervals", p.traffic.SpikeAfterIntervals,
		"spike_after_seconds", spikeAfterSeconds,
		"spike_rate_kbps", p.traffic.SpikeRateKbps,
		"telemetry_interval_seconds", intervalSeconds,
	)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.intervalCount++
			ctx, span := localotel.Tracer.Start(ctx, "produce-telemetry", trace.WithNewRoot())
			var messages []kafka.Message

			for _, inPort := range p.scenario.Switches[0].IngressPorts {
				for _, ePort := range p.scenario.Switches[0].EgressPorts {

					flowKey := fmt.Sprintf("%d-%d", inPort, ePort)

					// Get previous counters
					state := p.counters[flowKey]

					rxIncrease, txIncrease := p.bytesPerInterval(intervalSeconds, uint64(inPort), uint64(ePort))

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
				span.End()
				continue // Skip metrics/logging on error
			} else {
				flowsProduced.Add(ctx, int64(len(messages)), metric.WithAttributes(attribute.String("switch_id", p.scenario.Switches[0].ID)))
				msg := fmt.Sprintf("Produced %d records (interval=%d pattern=%s)", len(messages), p.intervalCount, p.traffic.Pattern)
				slog.DebugContext(ctx, msg,
					"number_of_messages", len(messages),
				)
				span.End()
			}
		}
	}
}
