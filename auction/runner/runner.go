package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
	auctionMetricsOnce            sync.Once
	auctionRunsCounter            metric.Int64Counter
	auctionRequestedUnitsCounter  metric.Int64Counter
	auctionClearingPriceLatest    metric.Int64Gauge
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

		auctionRequestedUnitsCounter, err = localotel.Meter.Int64Counter(
			"ixp.auction.units.requested",
			metric.WithDescription("Total number of bandwidth units requested by submitted bids"),
			metric.WithUnit("kbps"),
		)
		if err != nil {
			slog.Error("failed to initialize auctionRequestedUnitsCounter", "error", err)
		}

		auctionClearingPriceLatest, err = localotel.Meter.Int64Gauge(
			"ixp.auction.clearing_price.latest",
			metric.WithDescription("Latest auction clearing price per egress port"),
			metric.WithUnit("SGD"),
		)
		if err != nil {
			slog.Error("failed to initialize auctionClearingPriceLatest", "error", err)
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
		customerID := ""
		if len(valueParts) >= 4 {
			customerID = valueParts[3]
		}

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
			if err != nil {
				bidSpan.SetStatus(codes.Error, "error parsing ingress port")
				bidSpan.RecordError(err)
				msg := fmt.Sprintf("Error parsing ingress port: %v", err)
				slog.DebugContext(bidCtx, msg, "error", err)
			}
			// Save the span to update later
			bidSpans[ingressPort] = bidSpan
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
				CustomerID:  customerID,
			})
			slog.DebugContext(bidCtx, "Bid parsed and linked",
				"ingress_port", ingressPort,
				"egress_port", egressPort,
				"units", units,
				"unit_price", unitPrice,
				"customer_id", customerID,
			)
		}

	}

	if capacity <= 0 || len(bids) == 0 {
		if len(bids) > 0 {
			requestedUnitsByIngress := make(map[uint64]int64)
			for _, bid := range bids {
				requestedUnitsByIngress[bid.IngressPort] += int64(bid.Units)
			}
			for ingressPort, requested := range requestedUnitsByIngress {
				bidAttrs := metric.WithAttributes(
					attribute.Int64("egress_port", int64(egressPort)),
					attribute.Int64("ingress_port", int64(ingressPort)),
				)
				auctionRequestedUnitsCounter.Add(auctionCtx, requested, bidAttrs)
			}
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

	reservationPrice := r.scenario.ReservationPrice
	auctionSpan.SetAttributes(attribute.Int("auction.reservation_price", reservationPrice))

	_, algoSpan := localotel.Tracer.Start(auctionCtx, "algo-execution")
	allocations := make([]models.Allocation, 0)
	clearingPrice := reservationPrice
	if capacity > 0 && len(bids) > 0 {
		allocations, clearingPrice = algo.RunReservationPriceAuction(intervalID, egressPort, capacity, bids, reservationPrice)
	}
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
	requestedUnitsByIngress := make(map[uint64]int64)
	for _, bid := range bids {
		requestedUnitsByIngress[bid.IngressPort] += int64(bid.Units)
	}
	for ingressPort, requested := range requestedUnitsByIngress {
		bidAttrs := metric.WithAttributes(
			attribute.Int64("egress_port", int64(egressPort)),
			attribute.Int64("ingress_port", int64(ingressPort)),
		)
		if requested > 0 {
			auctionRequestedUnitsCounter.Add(auctionCtx, requested, bidAttrs)
		}
	}
	auctionClearingPriceLatest.Record(auctionCtx, int64(clearingPrice), attrs)

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
		slog.DebugContext(auctionCtx, fmt.Sprintf("Allocated %d units (%d->%d)", alloc.AllocatedUnits, alloc.IngressPort, alloc.EgressPort),
			"allocated_units", alloc.AllocatedUnits,
			"ingress_port", alloc.IngressPort,
			"egress_port", alloc.EgressPort,
		)
	}

	// Bill credits per customer.
	// Bill credits: allocated_units * clearing_price per customer (grouped)
	if err := r.updateCredits(auctionCtx, allocations, clearingPrice); err != nil {
		slog.ErrorContext(auctionCtx, fmt.Sprintf("Error updating credits: %v", err), "error", err)
	}

	// Store auction history, including clearing price and per-customer allocations.
	if err := r.storeAuctionHistory(auctionCtx, intervalID, egressPort, clearingPrice, allocations); err != nil {
		auctionSpan.SetStatus(codes.Error, "auction-history-store-failed")
		auctionSpan.RecordError(err)
		slog.ErrorContext(auctionCtx, fmt.Sprintf("Error storing auction history: %v", err), "error", err)
	}

	err = bidMap.Clear(auctionCtx)
	if err != nil {
		auctionSpan.SetStatus(codes.Error, "error clearing bids")
		auctionSpan.RecordError(err)
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

func (r *AuctionRunner) updateCredits(ctx context.Context, allocations []models.Allocation, clearingPrice int) error {
	// Group spend by customer: allocated_units * clearing_price
	spendByCustomer := make(map[string]int)
	for _, alloc := range allocations {
		if alloc.CustomerID == "" {
			continue
		}
		amount := int(alloc.AllocatedUnits) * clearingPrice
		spendByCustomer[alloc.CustomerID] += amount
	}
	if len(spendByCustomer) == 0 {
		return nil
	}

	creditsMap, err := atomix.Map[string, string]("credits-map").
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		return fmt.Errorf("get credits map: %w", err)
	}

	for customerID, amount := range spendByCustomer {
		entry, err := creditsMap.Get(ctx, customerID)
		var cred shared.CustomerCredits
		if err == nil && entry.Value != "" {
			if err := json.Unmarshal([]byte(entry.Value), &cred); err != nil {
				log.Printf("invalid credits value for %s: %v", customerID, err)
				continue
			}
		}
		cred.TotalSpent += amount
		b, err := json.Marshal(cred)
		if err != nil {
			return fmt.Errorf("marshal credits: %w", err)
		}
		if _, err := creditsMap.Put(ctx, customerID, string(b)); err != nil {
			return fmt.Errorf("update credits for %s: %w", customerID, err)
		}
		log.Printf("[credits] %s spent %d (total %d)", customerID, amount, cred.TotalSpent)
	}
	return nil
}

func (r *AuctionRunner) storeAuctionHistory(ctx context.Context, intervalID string, egressPort uint64, clearingPrice int, allocations []models.Allocation) error {
	historyMap, err := atomix.Map[string, string]("auction-history").
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		return fmt.Errorf("get auction history map: %w", err)
	}

	record := shared.AuctionHistoryRecord{
		Interval:      intervalID,
		EgressPort:    egressPort,
		ClearingPrice: clearingPrice,
	}

	for _, alloc := range allocations {
		if alloc.CustomerID == "" || alloc.AllocatedUnits == 0 {
			continue
		}
		record.Allocations = append(record.Allocations, shared.AuctionCustomerAllocation{
			CustomerID:  alloc.CustomerID,
			IngressPort: alloc.IngressPort,
			Units:       alloc.AllocatedUnits,
		})
	}

	b, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal auction history: %w", err)
	}

	key := fmt.Sprintf("%s|%d", intervalID, egressPort)
	if _, err := historyMap.Put(ctx, key, string(b)); err != nil {
		return fmt.Errorf("put auction history for %s: %w", key, err)
	}

	return nil
}

func currentIntervalID(interval time.Duration) string {
	now := time.Now().Unix()
	intervalSec := int64(interval.Seconds())
	start := (now / intervalSec) * intervalSec
	return time.Unix(start, 0).UTC().Format(time.RFC3339)
}
