package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/atomix/go-sdk/pkg/atomix"
	"github.com/atomix/go-sdk/pkg/generic"
	atomixmap "github.com/atomix/go-sdk/pkg/primitive/map"
	"github.com/chew01/ixp-gcp/shared"
	localotel "github.com/chew01/ixp-gcp/shared/otel"
	pb "github.com/chew01/ixp-gcp/shared/proto/pb"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/protobuf/proto"
)

type processingError struct {
	target string
	err    error
}

func (e *processingError) Error() string {
	return e.err.Error()
}

func (e *processingError) Unwrap() error {
	return e.err
}

func newProcessingError(target string, err error) error {
	if err == nil {
		return nil
	}
	return &processingError{target: target, err: err}
}

type FlowState struct {
	LastSeenTime time.Time `json:"last_seen_time"`
	LastRxBytes  uint64    `json:"last_rx_bytes"`
	LastTxBytes  uint64    `json:"last_tx_bytes"`
}

type FlowMetrics struct {
	FlowKey     string
	IngressKbps float64
	EgressKbps  float64
	DropKbps    float64
	DropRate    float64
}

type Consumer struct {
	reader        *kafka.Reader
	topic         string
	flowStateMap  atomixmap.Map[string, string]
	throughputMap atomixmap.Map[string, string]
	metrics       *MetricsRegistry
}

func NewConsumer(ctx context.Context, kafkaBootstrap, topic string, dialer *kafka.Dialer) (*Consumer, error) {
	metrics, err := InitMetrics(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize metrics: %w", err)
	}

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        []string{kafkaBootstrap},
		Topic:          topic,
		GroupID:        "telemetry-service",
		StartOffset:    kafka.FirstOffset, // Start from beginning on first run; use committed offset on subsequent runs
		Dialer:         dialer,
		CommitInterval: 5 * time.Second, // Commit offsets every 5 seconds for automatic recovery
	})

	flowStateMap, err := atomix.Map[string, string]("flow-state-map").
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get flow state map: %w", err)
	}

	throughputMap, err := atomix.Map[string, string]("throughput-map").
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get throughput map: %w", err)
	}

	return &Consumer{
		reader:        reader,
		topic:         topic,
		flowStateMap:  flowStateMap,
		throughputMap: throughputMap,
		metrics:       metrics,
	}, nil
}

func (c *Consumer) Close() {
	c.reader.Close()
}

func (c *Consumer) Run(ctx context.Context) {
	idleTicker := time.NewTicker(30 * time.Second)
	defer idleTicker.Stop()

	var consumedCount int64

	// Log initial consumer state
	stats := c.reader.Stats()
	slog.Info("Kafka consumer starting",
		"topic", c.topic,
		"initial_offset", c.reader.Offset(),
		"partition_count", stats.Partition,
		"lag", stats.Lag,
		"queue_length", stats.QueueLength,
	)

	for {
		msg, err := c.reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				slog.Info("Consumer shutting down")
				return
			}
			stats := c.reader.Stats()
			slog.Error("kafka read failed",
				"topic", c.topic,
				"error", err,
				"lag", stats.Lag,
				"queue_length", stats.QueueLength,
			)
			continue
		}

		consumedCount++
		if consumedCount == 1 || consumedCount%100 == 0 {
			slog.Debug("kafka message consumed",
				"topic", c.topic,
				"count", consumedCount,
				"partition", msg.Partition,
				"offset", msg.Offset,
				"message_time", msg.Time,
			)
		}

		// Record flow consumed from Kafka
		if c.metrics.FlowsConsumedTotal != nil {
			c.metrics.FlowsConsumedTotal.Add(ctx, 1)
		}

		if err := c.handleMessage(ctx, msg); err != nil {
			var pErr *processingError
			if errors.As(err, &pErr) {
				if c.metrics.KafkaConsumerErrors != nil {
					c.metrics.KafkaConsumerErrors.Add(ctx, 1, metric.WithAttributes(attribute.String("error_target", pErr.target)))
				}
			}
			slog.Error("Error handling message", "error", err)
		}

		select {
		case <-idleTicker.C:
			stats := c.reader.Stats()
			slog.Debug("telemetry consumer heartbeat",
				"topic", c.topic,
				"consumed_total", consumedCount,
				"lag", stats.Lag,
				"queue_length", stats.QueueLength,
			)
		default:
		}
	}
}

func (c *Consumer) handleMessage(ctx context.Context, msg kafka.Message) error {
	var report pb.TelemetryReport
	if err := proto.Unmarshal(msg.Value, &report); err != nil {
		return fmt.Errorf("failed to parse proto value: %w", err)
	}

	flow := report.GetFlowId()
	if flow == nil {
		return fmt.Errorf("telemetry report has no flow_id")
	}

	flowKey := buildFlowKey(flow)

	prev, err := c.getFlowState(ctx, flowKey)
	if err != nil {
		return fmt.Errorf("failed to get flow state: %w", err)
	}

	eventTime := msg.Time
	if eventTime.IsZero() {
		eventTime = time.Now()
		slog.Debug("kafka message timestamp missing; using current time",
			"topic", c.topic,
			"flow_key", flowKey,
		)
	}

	if prev != nil {
		metrics, ok := computeMetrics(flowKey, &report, *prev, eventTime)
		if ok {
			if err := c.publishMetrics(ctx, metrics); err != nil {
				return err
			}
		} else {
			slog.Debug("skipping metrics publish due to non-positive delta time",
				"flow_key", flowKey,
				"message_time", eventTime,
				"previous_time", prev.LastSeenTime,
			)
		}
	} else {
		slog.Debug("flow baseline established", "ingress_port", flow.GetIngressPort(), "egress_port", flow.GetEgressPort())
	}

	return c.setFlowState(ctx, flowKey, FlowState{
		LastSeenTime: eventTime,
		LastRxBytes:  report.GetRxByteCount(),
		LastTxBytes:  report.GetTxByteCount(),
	})
}

func (c *Consumer) getFlowState(ctx context.Context, key string) (*FlowState, error) {
	entry, err := c.flowStateMap.Get(ctx, key)
	if err != nil {
		return nil, nil // key doesn't exist yet
	}

	var state FlowState
	if err := json.Unmarshal([]byte(entry.Value), &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal flow state: %w", err)
	}

	return &state, nil
}

func (c *Consumer) setFlowState(ctx context.Context, key string, state FlowState) error {
	b, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal flow state: %w", err)
	}

	startAtomixTime := time.Now()
	if _, err := c.flowStateMap.Put(ctx, key, string(b)); err != nil {
		localotel.RecordAtomixOperation(ctx, "write_flow_state", startAtomixTime, err)
		return newProcessingError("atomix_write", fmt.Errorf("failed to put flow state: %w", err))
	}
	localotel.RecordAtomixOperation(ctx, "write_flow_state", startAtomixTime, nil)

	return nil
}

func (c *Consumer) publishMetrics(ctx context.Context, m FlowMetrics) error {
	val := shared.FlowMetricsValue{
		ThroughputKbps: m.IngressKbps,
		EgressKbps:     m.EgressKbps,
		DropKbps:       m.DropKbps,
		DropRatePct:    m.DropRate,
	}
	b, err := json.Marshal(val)
	if err != nil {
		return newProcessingError("kafka_decode", fmt.Errorf("failed to marshal flow metrics: %w", err))
	}
	startAtomixTime := time.Now()
	if _, err := c.throughputMap.Put(ctx, m.FlowKey, string(b)); err != nil {
		localotel.RecordAtomixOperation(ctx, "write_flow_metrics", startAtomixTime, err)
		return newProcessingError("atomix_write", fmt.Errorf("failed to put flow metrics: %w", err))
	}
	localotel.RecordAtomixOperation(ctx, "write_flow_metrics", startAtomixTime, nil)

	// Record flow published to Atomix
	if c.metrics.FlowMetricsPublished != nil {
		c.metrics.FlowMetricsPublished.Add(ctx, 1)
	}

	slog.Debug("flow metrics published",
		"flow_key", m.FlowKey,
		"ingress_kbps", m.IngressKbps,
		"egress_kbps", m.EgressKbps,
		"drop_kbps", m.DropKbps,
		"drop_rate_pct", m.DropRate,
	)

	// TODO: forward to TSDB
	return nil
}

func buildFlowKey(flow *pb.Flow) string {
	return fmt.Sprintf("%d|%d",
		flow.GetIngressPort(),
		flow.GetEgressPort(),
	)
}

func computeMetrics(flowKey string, report *pb.TelemetryReport, prev FlowState, msgTime time.Time) (FlowMetrics, bool) {
	dt := msgTime.Sub(prev.LastSeenTime).Seconds()
	if dt <= 0 {
		return FlowMetrics{}, false
	}

	rxDelta := delta(report.GetRxByteCount(), prev.LastRxBytes)
	txDelta := delta(report.GetTxByteCount(), prev.LastTxBytes)

	var dropDelta uint64
	if rxDelta > txDelta {
		dropDelta = rxDelta - txDelta
	}

	rxBits := float64(rxDelta) * 8
	txBits := float64(txDelta) * 8
	dropBits := float64(dropDelta) * 8

	ingressBps := rxBits / dt
	egressBps := txBits / dt
	dropBps := dropBits / dt

	var dropRate float64
	if rxDelta > 0 {
		dropRate = float64(dropDelta) / float64(rxDelta) * 100
	}

	return FlowMetrics{
		FlowKey:     flowKey,
		IngressKbps: ingressBps / 1e3,
		EgressKbps:  egressBps / 1e3,
		DropKbps:    dropBps / 1e3,
		DropRate:    dropRate,
	}, true
}

func delta(curr, prev uint64) uint64 {
	if curr >= prev {
		return curr - prev
	}
	// ASSUMPTION - does not wrap more than once
	return (math.MaxUint64 - prev) + curr + 1
}
