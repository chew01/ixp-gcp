package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/atomix/go-sdk/pkg/atomix"
	"github.com/atomix/go-sdk/pkg/generic"
	"github.com/chew01/ixp-gcp/auction/algo"
	"github.com/chew01/ixp-gcp/auction/models"
	"github.com/chew01/ixp-gcp/shared"
	localotel "github.com/chew01/ixp-gcp/shared/otel"
	"github.com/chew01/ixp-gcp/shared/scenario"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var (
	auctionMetricsOnce             sync.Once
	auctionRunsCounter             metric.Int64Counter
	auctionBidsTotalCounter        metric.Int64Counter
	auctionBidsAllocatedCounter    metric.Int64Counter
	auctionBidsUnallocatedCounter  metric.Int64Counter
	auctionRequestedUnitsCounter   metric.Int64Counter
	auctionAllocatedUnitsCounter   metric.Int64Counter
	auctionClearingPriceHist       metric.Int64Histogram
	auctionBidAllocationRatioHist  metric.Float64Histogram
	auctionUnitAllocationRatioHist metric.Float64Histogram
)

// AuctionRunner owns the auction loop
type AuctionRunner struct {
	interval time.Duration
	scenario *scenario.Scenario
	writer   *kafka.Writer
}

func New(writer *kafka.Writer, interval time.Duration, scenario *scenario.Scenario) *AuctionRunner {
	initAuctionMetrics()
	return &AuctionRunner{
		writer:   writer,
		interval: interval,
		scenario: scenario,
	}
}

func initAuctionMetrics() {
	auctionMetricsOnce.Do(func() {
		var err error

		auctionRunsCounter, err = localotel.Meter.Int64Counter(
			"ixp.auction.runs.total",
			metric.WithDescription("Total number of auction runs"),
			metric.WithUnit("1"),
		)
		if err != nil {
			slog.Error("failed to initialize auctionRunsCounter", "error", err)
		}

		auctionBidsTotalCounter, err = localotel.Meter.Int64Counter(
			"ixp.auction.bids.total",
			metric.WithDescription("Total number of bids observed by auction runs"),
			metric.WithUnit("1"),
		)
		if err != nil {
			slog.Error("failed to initialize auctionBidsTotalCounter", "error", err)
		}

		auctionBidsAllocatedCounter, err = localotel.Meter.Int64Counter(
			"ixp.auction.bids.allocated",
			metric.WithDescription("Total number of bids that received an allocation"),
			metric.WithUnit("1"),
		)
		if err != nil {
			slog.Error("failed to initialize auctionBidsAllocatedCounter", "error", err)
		}

		auctionBidsUnallocatedCounter, err = localotel.Meter.Int64Counter(
			"ixp.auction.bids.unallocated",
			metric.WithDescription("Total number of bids that received no allocation"),
			metric.WithUnit("1"),
		)
		if err != nil {
			slog.Error("failed to initialize auctionBidsUnallocatedCounter", "error", err)
		}

		auctionRequestedUnitsCounter, err = localotel.Meter.Int64Counter(
			"ixp.auction.units.requested",
			metric.WithDescription("Total number of bandwidth units requested by bids"),
			metric.WithUnit("kbps"),
		)
		if err != nil {
			slog.Error("failed to initialize auctionRequestedUnitsCounter", "error", err)
		}

		auctionAllocatedUnitsCounter, err = localotel.Meter.Int64Counter(
			"ixp.auction.units.allocated",
			metric.WithDescription("Total number of bandwidth units allocated by auctions"),
			metric.WithUnit("kbps"),
		)
		if err != nil {
			slog.Error("failed to initialize auctionAllocatedUnitsCounter", "error", err)
		}

		auctionClearingPriceHist, err = localotel.Meter.Int64Histogram(
			"ixp.auction.clearing_price",
			metric.WithDescription("Distribution of auction clearing prices"),
			metric.WithUnit("SGD"),
		)
		if err != nil {
			slog.Error("failed to initialize auctionClearingPriceHist", "error", err)
		}

		auctionBidAllocationRatioHist, err = localotel.Meter.Float64Histogram(
			"ixp.auction.bid_allocation_ratio",
			metric.WithDescription("Per-run ratio of allocated bids to total bids"),
			metric.WithUnit("1"),
		)
		if err != nil {
			slog.Error("failed to initialize auctionBidAllocationRatioHist", "error", err)
		}

		auctionUnitAllocationRatioHist, err = localotel.Meter.Float64Histogram(
			"ixp.auction.unit_allocation_ratio",
			metric.WithDescription("Per-run ratio of allocated units to requested units"),
			metric.WithUnit("1"),
		)
		if err != nil {
			slog.Error("failed to initialize auctionUnitAllocationRatioHist", "error", err)
		}
	})
}

func (r *AuctionRunner) Run(ctx context.Context) {
	// ctx, span := localotel.Tracer.Start(ctx, "auction-runner-running")
	// defer span.End()

	slog.DebugContext(ctx, "checking for scenario existence")
	if r.scenario == nil {
		// span.SetStatus(codes.Error, "scenario is nil or has no switches")
		slog.ErrorContext(ctx, "Auction runner failed to start: scenario is nil")
		return
	}

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			for _, port := range r.scenario.Switches[0].EgressPorts {
				r.runOnce(ctx, r.scenario.Switches[0].MaxCapacity, uint64(port))
			}
		case <-ctx.Done():
			slog.DebugContext(ctx, "Auction runner shutting down")
			return
		}
	}
}

func (r *AuctionRunner) runOnce(ctx context.Context, capacity uint64, egressPort uint64) {
	auctionCtx, auctionSpan := localotel.Tracer.Start(ctx, "auction-run-once",
		trace.WithNewRoot(),
		trace.WithAttributes(attribute.Int("egressPort", int(egressPort))))
	defer auctionSpan.End()
	attrs := metric.WithAttributes(
		attribute.Int64("egress_port", int64(egressPort)),
	)
	auctionRunsCounter.Add(auctionCtx, 1, attrs)
	intervalID := currentIntervalID(r.interval)
	auctionSpan.SetAttributes(attribute.String("intervalID", intervalID))

	msg := fmt.Sprintf("[Auction %d] Interval %s running", egressPort, intervalID)
	slog.DebugContext(ctx, msg,
		"egressPort", egressPort,
		"intervalID", intervalID,
	)

	// Create a map to hold the spans for each ingress port
	bidSpans := make(map[uint64]trace.Span)
	var bids []models.Bid

	mapID := fmt.Sprintf("bids-%d", egressPort)
	bidMap, err := atomix.Map[string, string](mapID).
		Codec(generic.Scalar[string]()).
		Get(auctionCtx)
	if err != nil {
		auctionSpan.SetStatus(codes.Error, "error getting bid map")
		auctionSpan.RecordError(err)
		msg := fmt.Sprintf("Error getting bid map: %v", err)
		slog.ErrorContext(auctionCtx, msg, "error", err)
	}

	list, err := bidMap.List(auctionCtx)
	if err != nil {
		auctionSpan.SetStatus(codes.Error, "error listing bids")
		auctionSpan.RecordError(err)
		msg := fmt.Sprintf("Error listing bids: %v", err)
		slog.ErrorContext(auctionCtx, msg, "error", err)
		return
	}

	for {
		entry, err := list.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				auctionSpan.SetStatus(codes.Error, "error getting next bid")
				auctionSpan.RecordError(err)
				msg := fmt.Sprintf("Error getting next bid: %v", err)
				slog.ErrorContext(auctionCtx, msg, "error", err)
			}
			break
		}

		key := any(entry.Key).(string)
		value := any(entry.Value).(string)
		valueParts := strings.Split(value, "|")

		// Extract the traceparent (part index 2)
		var links []trace.Link
		// var temp_ctx context.Context
		if len(valueParts) > 2 {
			carrier := localotel.StringMapCarrier{"traceparent": valueParts[2]}
			extractedCtx := otel.GetTextMapPropagator().Extract(auctionCtx, carrier)
			// Grab the SpanContext (the ID data) out of that extracted context
			remoteSpanCtx := trace.SpanContextFromContext(extractedCtx)

			if remoteSpanCtx.IsValid() {
				links = append(links, trace.Link{SpanContext: remoteSpanCtx})
			}

			// Create a link to the original producer span
			// link = trace.LinkFromContext(parentCtx)
			// Add the link to the Auction logic span or a sub-span
			// If processing a single bid has logic, start a sub-span with the link:
			bidCtx, bidSpan := localotel.Tracer.Start(auctionCtx, "process-individual-bid", trace.WithLinks(links...))

			ingressPort, err := strconv.ParseUint(key, 10, 64)
			// Save the span to update later
			bidSpans[ingressPort] = bidSpan
			if err != nil {
				bidSpan.SetStatus(codes.Error, "error parsing ingress port")
				bidSpan.RecordError(err)
				msg := fmt.Sprintf("Error parsing ingress port: %v", err)
				slog.DebugContext(bidCtx, msg, "error", err)
			}
			units, err := strconv.ParseUint(valueParts[0], 10, 64)
			if err != nil {
				bidSpan.SetStatus(codes.Error, "error parsing units")
				bidSpan.RecordError(err)
				msg := fmt.Sprintf("Error parsing units: %v", err)
				slog.ErrorContext(bidCtx, msg, "error", err)
				continue
			}
			unitPrice, err := strconv.Atoi(valueParts[1])
			if err != nil {
				bidSpan.SetStatus(codes.Error, "error parsing unit price")
				bidSpan.RecordError(err)
				msg := fmt.Sprintf("Error parsing unit price: %v", err)
				slog.ErrorContext(bidCtx, msg, "error", err)
				continue
			}

			bids = append(bids, models.Bid{
				IngressPort: ingressPort,
				EgressPort:  egressPort,
				Units:       units,
				UnitPrice:   unitPrice,
			})
			slog.DebugContext(bidCtx, "Bid parsed and linked", "ingress", ingressPort)
		}

	}

	if capacity <= 0 || len(bids) == 0 {
		if len(bids) > 0 {
			var requestedUnits uint64
			for _, bid := range bids {
				requestedUnits += bid.Units
			}
			auctionBidsTotalCounter.Add(auctionCtx, int64(len(bids)), attrs)
			auctionBidsUnallocatedCounter.Add(auctionCtx, int64(len(bids)), attrs)
			auctionRequestedUnitsCounter.Add(auctionCtx, int64(requestedUnits), attrs)
			auctionBidAllocationRatioHist.Record(auctionCtx, 0, attrs)
			auctionUnitAllocationRatioHist.Record(auctionCtx, 0, attrs)
		}
		slog.DebugContext(auctionCtx, "No capacity or no bids, skipping auction")
		return
	}

	msg = fmt.Sprintf("[Auction %d] %d bids for %d units", egressPort, len(bids), capacity)
	auctionSpan.SetAttributes(
		attribute.Int64("auction.number_of_bids", int64(len(bids))),
	)
	slog.DebugContext(auctionCtx, msg,
		"egressPort", egressPort,
		"bid_len", len(bids),
		"capacity", capacity,
	)

	// allocations, clearingPrice := algo.RunUniformPriceAuction(intervalID, capacity, bids)
	_, algoSpan := localotel.Tracer.Start(auctionCtx, "algo-execution")
	allocations, clearingPrice := algo.RunReservationPriceAuction(intervalID, egressPort, capacity, bids, r.scenario.ReservationPrice)
	algoSpan.SetAttributes(attribute.Int("clearing_price", clearingPrice))
	algoSpan.End()

	for _, span := range bidSpans {
		span.SetAttributes(attribute.Bool("auction.is_allocated", false))
		span.SetAttributes(attribute.Int("auction.clearing_price", clearingPrice))
	}

	// Update the auction result for each bid span
	for _, alloc := range allocations {
		if span, ok := bidSpans[alloc.IngressPort]; ok {
			span.SetAttributes(
				attribute.Bool("auction.is_allocated", true),
				attribute.Int64("auction.allocated_units", int64(alloc.AllocatedUnits)),
				attribute.Int64("auction.ingress_port", int64(alloc.IngressPort)),
				attribute.Int64("auction.egress_port", int64(alloc.EgressPort)),
				attribute.Int64("auction.clearing_price", int64(alloc.ClearingPrice)),
				attribute.String("auction.interval", alloc.Interval),
			)
		}
	}

	allocatedBidSet := make(map[uint64]struct{}, len(allocations))
	var requestedUnits uint64
	for _, bid := range bids {
		requestedUnits += bid.Units
	}

	var allocatedUnits uint64
	for _, alloc := range allocations {
		allocatedUnits += alloc.AllocatedUnits
		if alloc.AllocatedUnits > 0 {
			allocatedBidSet[alloc.IngressPort] = struct{}{}
		}
	}

	allocatedBids := len(allocatedBidSet)
	unallocatedBids := len(bids) - allocatedBids
	if unallocatedBids < 0 {
		unallocatedBids = 0
	}

	auctionBidsTotalCounter.Add(auctionCtx, int64(len(bids)), attrs)
	auctionBidsAllocatedCounter.Add(auctionCtx, int64(allocatedBids), attrs)
	auctionBidsUnallocatedCounter.Add(auctionCtx, int64(unallocatedBids), attrs)
	auctionRequestedUnitsCounter.Add(auctionCtx, int64(requestedUnits), attrs)
	auctionAllocatedUnitsCounter.Add(auctionCtx, int64(allocatedUnits), attrs)
	auctionClearingPriceHist.Record(auctionCtx, int64(clearingPrice), attrs)

	bidAllocationRatio := float64(allocatedBids) / float64(len(bids))
	auctionBidAllocationRatioHist.Record(auctionCtx, bidAllocationRatio, attrs)

	if requestedUnits > 0 {
		unitAllocationRatio := float64(allocatedUnits) / float64(requestedUnits)
		auctionUnitAllocationRatioHist.Record(auctionCtx, unitAllocationRatio, attrs)
	}

	// End all bid spans
	for _, span := range bidSpans {
		span.End()
	}

	for _, alloc := range allocations {
		err := r.WriteResults(auctionCtx, "sw-1", alloc.IngressPort, alloc.EgressPort, alloc.AllocatedUnits)
		if err != nil {
			msg := fmt.Sprintf("Error setting up: %v", err)
			slog.ErrorContext(auctionCtx, msg, "error", err)
			return
		}
		msg := fmt.Sprintf("Allocated %d units (%d->%d)", alloc.AllocatedUnits, alloc.IngressPort, alloc.EgressPort)
		slog.DebugContext(auctionCtx, msg,
			"allocatedUnits", alloc.AllocatedUnits,
			"ingressPort", alloc.IngressPort,
			"egressPort", alloc.EgressPort,
		)
	}

	err = bidMap.Clear(auctionCtx)
	if err != nil {
		auctionSpan.SetStatus(codes.Error, "error clearing bids")
		msg := fmt.Sprintf("Error clearing bids: %v", err)
		slog.ErrorContext(auctionCtx, msg, "error", err)
	}

	msg = fmt.Sprintf("[Auction %d] Interval %s clearing price %d", egressPort, intervalID, clearingPrice)
	slog.DebugContext(auctionCtx, msg,
		"egressPort", egressPort,
		"intervalID", intervalID,
		"clearingPrice", clearingPrice,
	)

	auctionSpan.End()
}

func (r *AuctionRunner) WriteResults(ctx context.Context, switchID string, ingressPort, egressPort, bandwidthKbps uint64) error {
	ctx, span := localotel.Tracer.Start(ctx, "kafka-produce-result")
	defer span.End()
	results := shared.AuctionResultRecord{
		IngressPort:   ingressPort,
		EgressPort:    egressPort,
		BandwidthKbps: bandwidthKbps,
	}
	span.SetAttributes(
		attribute.Int64("auction.ingress_port", int64(ingressPort)),
		attribute.Int64("auction.egress_port", int64(egressPort)),
		attribute.Int64("auction.bandwidthkpbs", int64(bandwidthKbps)),
	)
	key := fmt.Sprintf("%s-results", switchID)
	value, err := json.Marshal(results)
	if err != nil {
		span.SetStatus(codes.Error, "failed to parse into JSON")
		span.RecordError(err)
		msg := fmt.Sprintf("failed to parse into JSON: %v", err)
		slog.ErrorContext(ctx, msg, "error", err)
	}

	// Prepare Kafka Headers for Propagation
	// This allows the next service to know which trace this belongs to
	headers := []kafka.Header{}
	carrier := localotel.StringMapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)

	for k, v := range carrier {
		headers = append(headers, kafka.Header{
			Key:   k,
			Value: []byte(v),
		})
	}

	err = r.writer.WriteMessages(ctx, kafka.Message{
		Key:     []byte(key),
		Value:   value,
		Headers: headers, // Trace context being passed here
	})

	if err != nil {
		span.SetStatus(codes.Error, "failed to write to kafka")
		span.RecordError(err)
		msg := fmt.Sprintf("Failed to write to kafka: %v", err)
		slog.ErrorContext(ctx, msg, "error", err)
		return err
	}

	return err
}

func currentIntervalID(interval time.Duration) string {
	now := time.Now().Unix()
	intervalSec := int64(interval.Seconds())
	start := (now / intervalSec) * intervalSec
	return time.Unix(start, 0).UTC().Format(time.RFC3339)
}
