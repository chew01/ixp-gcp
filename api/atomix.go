package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"

	"github.com/atomix/go-sdk/pkg/atomix"
	"github.com/atomix/go-sdk/pkg/generic"
	atomixmap "github.com/atomix/go-sdk/pkg/primitive/map"
	"github.com/chew01/ixp-gcp/shared"
	localotel "github.com/chew01/ixp-gcp/shared/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// ============================================================
// FlowStore
// ============================================================

type FlowStore interface {
	Get(ctx context.Context, flowKey string) (string, error)
	List(ctx context.Context) ([]string, error)
}

type AtomixFlowStore struct {
	throughputMap atomixmap.Map[string, string]
}

func NewAtomixFlowStore(ctx context.Context) (*AtomixFlowStore, error) {
	m, err := atomix.Map[string, string]("throughput-map").
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to init throughput map: %w", err)
	}
	return &AtomixFlowStore{throughputMap: m}, nil
}

func (s *AtomixFlowStore) Get(ctx context.Context, flowKey string) (string, error) {
	ctx, span := localotel.Tracer.Start(ctx, "fetch-flow")
	defer span.End()
	entry, err := s.throughputMap.Get(ctx, flowKey)
	if err != nil {
		msg := fmt.Sprintf("flow %s not found: %v", flowKey, err)
		span.SetStatus(codes.Error, "flow-not-found")
		span.RecordError(err)
		slog.ErrorContext(ctx, msg, "flowKey", flowKey, "error", err)
		return "", fmt.Errorf("flow %s not found: %w", flowKey, err)
	}
	return entry.Value, nil
}

func (s *AtomixFlowStore) List(ctx context.Context) ([]string, error) {
	var keys []string
	entries, err := s.throughputMap.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list flows: %w", err)
	}
	for {
		entry, err := entries.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("failed to iterate flows: %w", err)
		}
		keys = append(keys, entry.Key)
	}
	return keys, nil
}

// ============================================================
// BidStore
// ============================================================

type BidStore interface {
	Put(ctx context.Context, bid shared.BidRequest) error
}

type AtomixBidStore struct {
	// keyed by egress port, initialized lazily since ports are dynamic
	maps map[uint32]atomixmap.Map[string, string]
}

func NewAtomixBidStore() *AtomixBidStore {
	return &AtomixBidStore{
		maps: make(map[uint32]atomixmap.Map[string, string]),
	}
}

func (s *AtomixBidStore) getOrCreateMap(ctx context.Context, egressPort uint32) (atomixmap.Map[string, string], error) {
	ctx, span := localotel.Tracer.Start(ctx, "get-or-create-bid-map")
	defer span.End()
	if m, ok := s.maps[egressPort]; ok {
		return m, nil
	}
	mapID := fmt.Sprintf("bids-%d", egressPort)
	m, err := atomix.Map[string, string](mapID).
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		span.SetStatus(codes.Error, "error getting bid map")
		span.RecordError(err)
		msg := fmt.Sprintf("error getting bid map: %v", err)
		slog.ErrorContext(ctx, msg, "error", err)
		return nil, fmt.Errorf("failed to init bid map %s: %w", mapID, err)
	}

	s.maps[egressPort] = m
	return m, nil
}

func (s *AtomixBidStore) Put(ctx context.Context, bid shared.BidRequest) error {
	ctx, span := localotel.Tracer.Start(ctx, "bid-storing")
	defer span.End()

	span.SetAttributes(
		attribute.Int64("bid.ingress_port", int64(*bid.IngressPort)),
		attribute.Int64("bid.egress_port", int64(*bid.EgressPort)),
		attribute.Int64("bid.units", int64(*bid.Units)),
		attribute.Int64("bid.unit_price", int64(*bid.UnitPrice)),
	)
	bidMap, err := s.getOrCreateMap(ctx, uint32(*bid.EgressPort))
	// Inject trace context into a carrier
	carrier := make(localotel.StringMapCarrier)
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	traceParent := carrier["traceparent"]

	// identifier := fmt.Sprintf("%d", *bid.IngressPort)
	// bidValue := fmt.Sprintf("%d|%d|%s", *bid.Units, *bid.UnitPrice, traceParent)
	// msg := fmt.Sprintf("Putting %s to %s", bidValue, identifier)
	// slog.DebugContext(ctx, msg,
	// 	"bidValue", bidValue,
	// 	"identifer", identifier,
	// 	"traceParent", traceParent,
	// )

	key := fmt.Sprintf("%d", *bid.IngressPort)
	value := fmt.Sprintf("%d|%d|%s", *bid.Units, *bid.UnitPrice, traceParent)

	msg := fmt.Sprintf("Putting %s to %s", value, key)
	slog.DebugContext(ctx, msg,
		"bidValue", value,
		"map_key", key,
		"traceParent", traceParent,
	)

	if _, err := bidMap.Put(ctx, key, value); err != nil {
		span.SetStatus(codes.Error, "Error putting bid for ingress port")
		span.RecordError(err)
		span.SetAttributes(attribute.Int("ingressPort", int(*bid.IngressPort)))
		msg := fmt.Sprintf("Error putting bid for ingress port %d: %v", bid.IngressPort, err)
		slog.ErrorContext(ctx, msg, "ingressPort", bid.IngressPort, "error", err)
		return fmt.Errorf("failed to store bid for ingress port %d: %w", *bid.IngressPort, err)
	}

	length, err := bidMap.Len(ctx)
	if err != nil {
		span.SetStatus(codes.Error, "error getting bid map ength")
		span.RecordError(err)
		msg := fmt.Sprintf("failed to get bid map length: %v", err)
		slog.ErrorContext(ctx, msg, "error", err)
	} else {
		msg := fmt.Sprintf("[bids-%d] %d bids stored", *bid.EgressPort, length)
		slog.DebugContext(ctx, msg,
			"bid.egress_port", *bid.EgressPort,
			"bids.count", length,
		)
		log.Printf("[bids-%d] %d bids stored", *bid.EgressPort, length)
	}

	return nil
}
