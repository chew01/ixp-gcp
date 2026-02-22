package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"

	"github.com/atomix/go-sdk/pkg/atomix"
	"github.com/atomix/go-sdk/pkg/generic"
	localotel "github.com/chew01/ixp-gcp/shared/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type AtomixFlowStore struct{}

func (s *AtomixFlowStore) Get(ctx context.Context, flowKey string) (string, error) {
	throughputMap, err := atomix.Map[string, string]("throughput-map").
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		log.Printf("Error getting throughput map: %v", err)
	}

	entry, err := throughputMap.Get(ctx, flowKey)
	if err != nil {
		log.Printf("Error getting flow %s: %v", flowKey, err)
		return "", err
	}
	return entry.Value, nil
}

type AtomixBidStore struct{}

func (s *AtomixBidStore) Put(ctx context.Context, bid Bid) error {
	ctx, span := localotel.Tracer.Start(ctx, "bid-storing")
	defer span.End()
	mapID := fmt.Sprintf("bids-%d", *bid.EgressPort)
	bidMap, err := atomix.Map[string, string](mapID).
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		span.SetStatus(codes.Error, "error getting bid map")
		span.RecordError(err)
		msg := fmt.Sprintf("error getting bid map: %v", err)
		slog.ErrorContext(ctx, msg, "error", err)
		return err
	}

	// Inject trace context into a carrier
	carrier := make(localotel.StringMapCarrier)
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	traceParent := carrier["traceparent"]

	identifier := fmt.Sprintf("%d", *bid.IngressPort)
	bidValue := fmt.Sprintf("%d|%d|%s", *bid.Units, *bid.UnitPrice, traceParent)
	msg := fmt.Sprintf("Putting %s to %s", bidValue, identifier)
	slog.DebugContext(ctx, msg,
		"bidValue", bidValue,
		"identifer", identifier,
		"traceParent", traceParent,
	)
	_, err = bidMap.Put(ctx, identifier, bidValue)
	if err != nil {
		span.SetStatus(codes.Error, "error putting bid for ingress port")
		span.RecordError(err)
		span.SetAttributes(attribute.Int("ingressPort", int(*bid.IngressPort)))
		msg := fmt.Sprintf("Error putting bid for ingress port %d: %v", bid.IngressPort, err)
		slog.ErrorContext(ctx, msg, "ingressPort", bid.IngressPort, "error", err)
		return err
	}

	length, err := bidMap.Len(ctx)
	if err != nil {
		span.SetStatus(codes.Error, "error getting bid map ength")
		span.RecordError(err)
		msg := fmt.Sprintf("Error getting bid map length: %v", err)
		slog.ErrorContext(ctx, msg, "error", err)
	} else {
		msg := fmt.Sprintf("%d bids in %s", length, mapID)
		slog.DebugContext(ctx, msg,
			"length", length,
			"mapId", mapID,
		)
	}
	return nil
}
