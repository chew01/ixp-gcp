package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/atomix/go-sdk/pkg/atomix"
	"github.com/atomix/go-sdk/pkg/generic"
	atomixmap "github.com/atomix/go-sdk/pkg/primitive/map"
	"github.com/chew01/ixp-gcp/shared"
	pb "github.com/chew01/ixp-gcp/shared/proto/pb"
	"github.com/segmentio/kafka-go"
	"google.golang.org/protobuf/proto"
)

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
	flowStateMap  atomixmap.Map[string, string]
	throughputMap atomixmap.Map[string, string]
}

func NewConsumer(ctx context.Context, kafkaBootstrap, topic string, dialer *kafka.Dialer) (*Consumer, error) {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{kafkaBootstrap},
		Topic:   topic,
		GroupID: "telemetry-service",
		Dialer:  dialer,
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
		flowStateMap:  flowStateMap,
		throughputMap: throughputMap,
	}, nil
}

func (c *Consumer) Close() {
	c.reader.Close()
}

func (c *Consumer) Run(ctx context.Context) {
	for {
		msg, err := c.reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				log.Println("Consumer shutting down")
				return
			}
			log.Println("Error reading message:", err)
			continue
		}

		if err := c.handleMessage(ctx, msg); err != nil {
			log.Printf("Error handling message: %v", err)
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

	if prev != nil {
		metrics, ok := computeMetrics(flowKey, &report, *prev, msg.Time)
		if ok {
			c.publishMetrics(ctx, metrics)
		}
	} else {
		log.Printf("flow %d→%d: first record, establishing baseline",
			flow.GetIngressPort(),
			flow.GetEgressPort(),
		)
	}

	return c.setFlowState(ctx, flowKey, FlowState{
		LastSeenTime: msg.Time,
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

	if _, err := c.flowStateMap.Put(ctx, key, string(b)); err != nil {
		return fmt.Errorf("failed to put flow state: %w", err)
	}

	return nil
}

func (c *Consumer) publishMetrics(ctx context.Context, m FlowMetrics) {
	val := shared.FlowMetricsValue{
		ThroughputKbps: m.IngressKbps,
		EgressKbps:     m.EgressKbps,
		DropKbps:       m.DropKbps,
		DropRatePct:    m.DropRate,
	}
	b, err := json.Marshal(val)
	if err != nil {
		log.Printf("failed to marshal flow metrics: %v", err)
		return
	}
	c.throughputMap.Put(ctx, m.FlowKey, string(b))

	log.Printf(
		"flow %s: ingress=%.2f Kbps egress=%.2f Kbps drop=%.2f Kbps (%.2f%%)",
		m.FlowKey,
		m.IngressKbps,
		m.EgressKbps,
		m.DropKbps,
		m.DropRate,
	)

	// TODO: forward to TSDB
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
