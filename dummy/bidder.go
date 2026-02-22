package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"time"

	localotel "github.com/chew01/ixp-gcp/shared/otel"
	"github.com/chew01/ixp-gcp/shared/scenario"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type DummyBidder struct {
	url      string
	http     *http.Client
	scenario *scenario.Scenario
}

type Bid struct {
	IngressPort *uint64 `json:"ingress_port"`
	EgressPort  *uint64 `json:"egress_port"` // maps to auction
	Units       *uint64 `json:"units"`       // bandwidth units (kbps)
	UnitPrice   *int    `json:"unit_price"`  // price per unit
}

func NewDummyBidder(url string, scenario *scenario.Scenario) *DummyBidder {
	return &DummyBidder{
		url:      url,
		http:     &http.Client{},
		scenario: scenario,
	}
}

func (b *DummyBidder) Run(ctx context.Context) {
	bidsSubmitted, _ := localotel.Meter.Int64Counter("bids_submitted_total", metric.WithDescription("Total number of bids submitted"))

	interval, err := time.ParseDuration(b.scenario.AuctionInterval)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to parse auction interval",
			"error", err,
		)
		return
	}
	for {
		local_ctx, span := localotel.Tracer.Start(ctx, "submit-bids-cycle", trace.WithNewRoot())
		count := 0
		for _, inPort := range b.scenario.Switches[0].IngressPorts {
			for _, ePort := range b.scenario.Switches[0].EgressPorts {
				// 10% chance to introduce a failure
				shouldFail := rand.IntN(10) == 0
				var bid *Bid
				var method string = "POST"

				ingressPort := uint64(inPort)
				egressPort := uint64(ePort)
				units := uint64(RandRange(0, 100))
				unitPrice := RandRange(1, 100)

				if shouldFail {
					// Randomly choose failure type
					failType := rand.IntN(5)
					switch failType {
					case 0: // Method error - use GET instead of POST
						method = "GET"
						bid = &Bid{
							IngressPort: &ingressPort,
							EgressPort:  &egressPort,
							Units:       &units,
							UnitPrice:   &unitPrice,
						}
					case 1: // Missing fields - omit one field randomly (simple)
						switch rand.IntN(4) {
						case 0:
							bid = &Bid{
								Units:     &units,
								UnitPrice: &unitPrice,
							}
						case 1:
							bid = &Bid{
								EgressPort: &egressPort,
								Units:      &units,
								UnitPrice:  &unitPrice,
							}
						case 2:
							bid = &Bid{
								IngressPort: &ingressPort,
								Units:       &units,
								UnitPrice:   &unitPrice,
							}
						case 3:
							bid = &Bid{
								IngressPort: &ingressPort,
								EgressPort:  &egressPort,
								UnitPrice:   &unitPrice,
							}
						}
					case 2: // Out of range port number (use negative value)
						negPort := -ingressPort // -1 as uint64 (all bits set)
						bid = &Bid{
							IngressPort: &negPort,
							EgressPort:  &egressPort,
							Units:       &units,
							UnitPrice:   &unitPrice,
						}
					case 3: // Negative unit price
						negUnitPrice := -unitPrice
						bid = &Bid{
							IngressPort: &ingressPort,
							EgressPort:  &egressPort,
							Units:       &units,
							UnitPrice:   &negUnitPrice,
						}
					case 4: // Zero or invalid units (zero or negative, 50/50)
						var badUnits *uint64
						if rand.IntN(2) == 0 {
							zero := uint64(0)
							badUnits = &zero
						} else {
							// Negative units as int, then cast to uint64 (will be large value)
							neg := -units
							badUnits = &neg
						}
						bid = &Bid{
							IngressPort: &ingressPort,
							EgressPort:  &egressPort,
							Units:       badUnits,
							UnitPrice:   &unitPrice,
						}
					}
				} else {
					// Normal bid
					ingressPort := uint64(inPort)
					egressPort := uint64(ePort)
					units := uint64(RandRange(0, 100))
					unitPrice := RandRange(1, 100)

					bid = &Bid{
						IngressPort: &ingressPort,
						EgressPort:  &egressPort,
						Units:       &units,
						UnitPrice:   &unitPrice,
					}
				}

				if err := b.SubmitBid(local_ctx, bid, method); err != nil {
					slog.ErrorContext(local_ctx, "Failed to submit bid.", "error", err)
				} else {
					count++
				}
			}
		}
		span.End()

		bidsSubmitted.Add(ctx, int64(count), metric.WithAttributes(attribute.String("switch_id", b.scenario.Switches[0].ID)))
		slog.DebugContext(ctx, "Submitted bids", "bid_count", count)
		// span.End()
		time.Sleep(interval)
	}
}

func (b *DummyBidder) SubmitBid(ctx context.Context, bid *Bid, method string) error {
	reqCtx, span := localotel.Tracer.Start(ctx, "submit-bid")
	defer span.End()

	body, err := json.Marshal(bid)
	if err != nil {
		span.SetStatus(codes.Error, "marshal error")
		span.RecordError(err)
		return fmt.Errorf("failed to marshal bid: %v", err)
	}

	req, err := http.NewRequestWithContext(reqCtx, method, b.url, bytes.NewBuffer(body))
	if err != nil {
		span.SetStatus(codes.Error, "request creation error")
		span.RecordError(err)
		return fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	otel.GetTextMapPropagator().Inject(reqCtx, propagation.HeaderCarrier(req.Header))

	// 10% chance to randomly fail to submit
	if rand.IntN(10) == 0 {
		span.SetStatus(codes.Error, "simulated random submit failure")
		span.RecordError(fmt.Errorf("simulated random submit failure"))
		return fmt.Errorf("simulated random submit failure")
	} else { // Submit smoothly
		resp, err := b.http.Do(req)
		if err != nil {
			span.SetStatus(codes.Error, "http error")
			span.RecordError(err)
			return fmt.Errorf("failed to submit bid: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			span.SetStatus(codes.Error, "failed to submit bid")
			span.RecordError(fmt.Errorf("failed to submit bid: %v, response status: %v, body: %v", bid, resp.StatusCode, string(body)))
			return fmt.Errorf("failed to submit bid: %v, response status: %v, body: %v", bid, resp.StatusCode, string(body))
		}
	}
	return nil
}
