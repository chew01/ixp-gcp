// server.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chew01/ixp-gcp/shared"
	localotel "github.com/chew01/ixp-gcp/shared/otel"
	"github.com/chew01/ixp-gcp/shared/scenario"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
)

var (
	serverMetricsOnce    sync.Once
	flowThroughput       metric.Float64ObservableGauge
	flowDropRate         metric.Float64ObservableGauge
	flowDropKbps         metric.Float64ObservableGauge
	flowEgressKbps       metric.Float64ObservableGauge
	customerCreditsSpent metric.Float64ObservableGauge
	auctionClearingPrice metric.Float64ObservableGauge
	customerAllocation   metric.Float64ObservableGauge
	bidPriceHistogram    metric.Int64Histogram
	bidUnitHistogram     metric.Int64Histogram
	apiPolicyViolations  metric.Int64Counter
)

// InitServerMetrics initializes the instruments using the Meter
// created by your boilerplate code.
func (s *Server) InitServerMetrics() {
	serverMetricsOnce.Do(func() {
		var err error

		// Callback for flow throughput metric
		callback_refreshThroughput := func(ctx context.Context, observer metric.Float64Observer) error {
			// Fail-fast: 50ms timeout prevents OTel exporter thread starvation
			timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			defer cancel()
			keys, err := s.fs.List(timeoutCtx)
			if err != nil {
				// Return nil, not err - allows OTel to push counters even if gauge fails
				return nil
			}

			for _, flowKey := range keys {
				swID, ingress, egress, err := parseFlowKey(flowKey)
				if err != nil {
					continue
				}

				raw, err := s.fs.Get(ctx, flowKey)
				if err != nil || raw == "" {
					continue
				}

				metrics, err := parseFlowMetricsValue(raw)
				if err != nil {
					slog.ErrorContext(ctx, fmt.Sprintf("failed to parse flow metrics for %s: %v", flowKey, err))
					continue
				}

				attrs := attribute.NewSet(
					attribute.String("switch_id", swID),
					attribute.Int("ingress_port", ingress),
					attribute.Int("egress_port", egress),
				)

				observer.Observe(metrics.ThroughputKbps, metric.WithAttributeSet(attrs))
			}
			return nil
		}

		// Callback for flow drop rate metric
		callback_refreshDropRate := func(ctx context.Context, observer metric.Float64Observer) error {
			// Fail-fast: 50ms timeout prevents OTel exporter thread starvation
			timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			defer cancel()
			keys, err := s.fs.List(timeoutCtx)
			if err != nil {
				// Return nil, not err - allows OTel to push counters even if gauge fails
				return nil
			}

			for _, flowKey := range keys {
				swID, ingress, egress, err := parseFlowKey(flowKey)
				if err != nil {
					continue
				}

				raw, err := s.fs.Get(ctx, flowKey)
				if err != nil || raw == "" {
					continue
				}

				metrics, err := parseFlowMetricsValue(raw)
				if err != nil {
					continue
				}

				attrs := attribute.NewSet(
					attribute.String("switch_id", swID),
					attribute.Int("ingress_port", ingress),
					attribute.Int("egress_port", egress),
				)

				observer.Observe(metrics.DropRatePct, metric.WithAttributeSet(attrs))
			}
			return nil
		}

		// Callback for flow drop kbps metric
		callback_refreshDropKbps := func(ctx context.Context, observer metric.Float64Observer) error {
			// Fail-fast: 50ms timeout prevents OTel exporter thread starvation
			timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			defer cancel()
			keys, err := s.fs.List(timeoutCtx)
			if err != nil {
				// Return nil, not err - allows OTel to push counters even if gauge fails
				return nil
			}

			for _, flowKey := range keys {
				swID, ingress, egress, err := parseFlowKey(flowKey)
				if err != nil {
					continue
				}

				raw, err := s.fs.Get(ctx, flowKey)
				if err != nil || raw == "" {
					continue
				}

				metrics, err := parseFlowMetricsValue(raw)
				if err != nil {
					continue
				}

				attrs := attribute.NewSet(
					attribute.String("switch_id", swID),
					attribute.Int("ingress_port", ingress),
					attribute.Int("egress_port", egress),
				)

				observer.Observe(metrics.DropKbps, metric.WithAttributeSet(attrs))
			}
			return nil
		}

		// Callback for flow egress kbps metric
		callback_refreshEgressKbps := func(ctx context.Context, observer metric.Float64Observer) error {
			// Fail-fast: 50ms timeout prevents OTel exporter thread starvation
			timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			defer cancel()
			keys, err := s.fs.List(timeoutCtx)
			if err != nil {
				// Return nil, not err - allows OTel to push counters even if gauge fails
				return nil
			}

			for _, flowKey := range keys {
				swID, ingress, egress, err := parseFlowKey(flowKey)
				if err != nil {
					continue
				}

				raw, err := s.fs.Get(ctx, flowKey)
				if err != nil || raw == "" {
					continue
				}

				metrics, err := parseFlowMetricsValue(raw)
				if err != nil {
					continue
				}

				attrs := attribute.NewSet(
					attribute.String("switch_id", swID),
					attribute.Int("ingress_port", ingress),
					attribute.Int("egress_port", egress),
				)

				observer.Observe(metrics.EgressKbps, metric.WithAttributeSet(attrs))
			}
			return nil
		}

		// Callback for customer credits metric
		callback_refreshCreditMetrics := func(ctx context.Context, observer metric.Float64Observer) error {
			// Fail-fast: 50ms timeout prevents OTel exporter thread starvation
			timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			defer cancel()
			keys, err := s.cs.List(timeoutCtx)
			if err != nil {
				slog.ErrorContext(ctx, fmt.Sprintf("failed to list credits keys: %v", err))
				// Return nil, not err - allows OTel to push counters even if gauge fails
				return nil
			}

			for _, customerID := range keys {
				cred, err := s.cs.Get(ctx, customerID)
				if err != nil {
					continue
				}

				attrs := attribute.NewSet(
					attribute.String("customer_id", customerID),
				)

				observer.Observe(float64(cred.TotalSpent), metric.WithAttributeSet(attrs))
			}
			return nil
		}

		// Callback for latest auction metrics per egress port.
		callback_refreshAuctionMetrics := func(ctx context.Context, observer metric.Float64Observer) error {
			// Fail-fast: 50ms timeout prevents OTel exporter thread starvation
			timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			defer cancel()
			keys, err := s.hs.List(timeoutCtx)
			if err != nil {
				slog.ErrorContext(ctx, fmt.Sprintf("failed to list auction history keys: %v", err))
				// Return nil, not err - allows OTel to push counters even if gauge fails
				return nil
			}

			latest := make(map[uint64]*shared.AuctionHistoryRecord)
			for _, key := range keys {
				raw, err := s.hs.Get(ctx, key)
				if err != nil || raw == "" {
					continue
				}

				var rec shared.AuctionHistoryRecord
				if err := json.Unmarshal([]byte(raw), &rec); err != nil {
					slog.ErrorContext(ctx, fmt.Sprintf("failed to unmarshal auction history %s: %v", key, err))
					continue
				}

				prev, ok := latest[rec.EgressPort]
				if !ok || rec.Interval > prev.Interval {
					r := rec
					latest[rec.EgressPort] = &r
				}
			}

			for egressPort, rec := range latest {
				egressAttrs := attribute.NewSet(attribute.Int64("egress_port", int64(egressPort)))
				observer.Observe(float64(rec.ClearingPrice), metric.WithAttributeSet(egressAttrs))

				totalsByCustomer := make(map[string]float64)
				for _, alloc := range rec.Allocations {
					totalsByCustomer[alloc.CustomerID] += float64(alloc.Units)
				}
				for customerID, total := range totalsByCustomer {
					attrs := attribute.NewSet(
						attribute.String("customer_id", customerID),
						attribute.Int64("egress_port", int64(egressPort)),
					)
					observer.Observe(total, metric.WithAttributeSet(attrs))
				}
			}

			return nil
		}

		// Create flow throughput gauge
		flowThroughput, err = localotel.Meter.Float64ObservableGauge(
			"ixp.flow.throughput",
			metric.WithDescription("Flow throughput in Kbps"),
			metric.WithUnit("kbps"),
			metric.WithFloat64Callback(callback_refreshThroughput),
		)
		if err != nil {
			slog.Error("Failed to initialise flowThroughput metric", "error", err)
		}

		// Create flow drop rate gauge
		flowDropRate, err = localotel.Meter.Float64ObservableGauge(
			"ixp.flow.drop_rate",
			metric.WithDescription("Flow packet drop rate as a percentage"),
			metric.WithUnit("%"),
			metric.WithFloat64Callback(callback_refreshDropRate),
		)
		if err != nil {
			slog.Error("Failed to initialise flowDropRate metric", "error", err)
		}

		// Create flow drop kbps gauge
		flowDropKbps, err = localotel.Meter.Float64ObservableGauge(
			"ixp.flow.drop_kbps",
			metric.WithDescription("Flow packet drop rate in Kbps"),
			metric.WithUnit("kbps"),
			metric.WithFloat64Callback(callback_refreshDropKbps),
		)
		if err != nil {
			slog.Error("Failed to initialise flowDropKbps metric", "error", err)
		}

		// Create flow egress kbps gauge
		flowEgressKbps, err = localotel.Meter.Float64ObservableGauge(
			"ixp.flow.egress_kbps",
			metric.WithDescription("Flow egress bandwidth in Kbps"),
			metric.WithUnit("kbps"),
			metric.WithFloat64Callback(callback_refreshEgressKbps),
		)
		if err != nil {
			slog.Error("Failed to initialise flowEgressKbps metric", "error", err)
		}

		// Create customer credits spent gauge
		customerCreditsSpent, err = localotel.Meter.Float64ObservableGauge(
			"ixp.customer.credits_spent",
			metric.WithDescription("Total credits spent by customer (accounting only)"),
			metric.WithUnit("credits"),
			metric.WithFloat64Callback(callback_refreshCreditMetrics),
		)
		if err != nil {
			slog.Error("Failed to initialise customerCreditsSpent metric", "error", err)
		}

		auctionClearingPrice, err = localotel.Meter.Float64ObservableGauge(
			"ixp.auction.clearing_price",
			metric.WithDescription("Latest auction clearing price per egress port"),
			metric.WithUnit("price"),
			metric.WithFloat64Callback(callback_refreshAuctionMetrics),
		)
		if err != nil {
			slog.Error("Failed to initialise auctionClearingPrice metric", "error", err)
		}

		customerAllocation, err = localotel.Meter.Float64ObservableGauge(
			"ixp.customer.allocation_kbps",
			metric.WithDescription("Allocated bandwidth in Kbps per customer per egress port (latest auction round)"),
			metric.WithUnit("kbps"),
			metric.WithFloat64Callback(callback_refreshAuctionMetrics),
		)
		if err != nil {
			slog.Error("Failed to initialise customerAllocation metric", "error", err)
		}

		// Initialize Bid Price Histogram
		bidPriceHistogram, err = localotel.Meter.Int64Histogram(
			"ixp.bid.price",
			metric.WithDescription("Distribution of bid unit prices"),
			metric.WithUnit("SGD"), // or whatever your currency is
		)
		if err != nil {
			slog.Error("Failed to initialize bidPriceHistogram", "error", err)
		}

		// Initialize Bid Units Histogram (Bandwidth demand)
		bidUnitHistogram, err = localotel.Meter.Int64Histogram(
			"ixp.bid.units",
			metric.WithDescription("Distribution of bandwidth units requested"),
			metric.WithUnit("kbps"),
		)
		if err != nil {
			slog.Error("Failed to initialize bid histograms", "error", err)
		}

		apiPolicyViolations, err = localotel.Meter.Int64Counter(
			"ixp.api.policy.violations",
			metric.WithDescription("HTTP 400/403 errors from invalid agent bids"),
			metric.WithUnit("1"),
		)
		if err != nil {
			slog.Error("Failed to initialize apiPolicyViolations", "error", err)
		} else {
			apiPolicyViolations.Add(context.Background(), 1, metric.WithAttributes(attribute.String("reason", "startup_bootstrap")))
			slog.Info("Bootstrap increment emitted", "metric", "ixp_api_policy_violations_total", "reason", "startup_bootstrap")
		}

		// Initialize Atomix metrics from shared module
		localotel.InitAtomixMetrics()

	})
}

// recordAtomixLatency is a convenience wrapper around localotel.RecordAtomixOperation
// Use this in the API gateway
func recordAtomixLatency(ctx context.Context, operation string, startTime time.Time, err error) {
	localotel.RecordAtomixOperation(ctx, operation, startTime, err)
}

func recordAPIPolicyViolation(ctx context.Context, reason string) {
	if apiPolicyViolations == nil {
		slog.ErrorContext(ctx, "apiPolicyViolations counter is nil", "reason", reason)
		return
	}
	ctxMetric := context.WithoutCancel(ctx)
	slog.DebugContext(ctx, "Recording apiPolicyViolations counter", "reason", reason)
	apiPolicyViolations.Add(ctxMetric, 1, metric.WithAttributes(attribute.String("reason", reason)))
	slog.DebugContext(ctx, "Successfully recorded apiPolicyViolations counter", "reason", reason)
}

type Server struct {
	fs       FlowStore
	bs       BidStore
	cs       CreditsStore
	hs       AuctionHistoryStore
	scenario *scenario.Scenario
}

// parseFlowMetricsValue parses the stored string into FlowMetricsValue.
// Backward compatible: if the value is a single number (old format), it is treated as throughput_kbps; other fields are 0.
func parseFlowMetricsValue(raw string) (shared.FlowMetricsValue, error) {
	var v shared.FlowMetricsValue
	if err := json.Unmarshal([]byte(raw), &v); err == nil {
		return v, nil
	}
	// Old format: single float (throughput only)
	var kbps float64
	if _, err := fmt.Sscanf(raw, "%f", &kbps); err != nil {
		return shared.FlowMetricsValue{}, fmt.Errorf("invalid flow metrics format: %q", raw)
	}
	return shared.FlowMetricsValue{ThroughputKbps: kbps}, nil
}

// GET /flows?switch_id=sw-1&ingress_port=1&egress_port=10
func (s *Server) getFlows(w http.ResponseWriter, r *http.Request) {
	ctx, span := localotel.Tracer.Start(r.Context(), "receive-request")
	defer span.End()
	if r.Method != http.MethodGet {
		slog.ErrorContext(ctx, "method not allowed")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	customerID := customerIDFromRequest(r)
	if customerID == "" {
		http.Error(w, "missing customer identity: set X-Customer-ID or Authorization: Bearer <customer_id>", http.StatusUnauthorized)
		return
	}

	switchID, ingress, egress, err := parseFlowParams(r)
	span.SetAttributes(
		attribute.String("switchID", switchID),
		attribute.Int("ingress", ingress),
		attribute.Int("egress", egress),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Enforce that the requested ingress port belongs to the authenticated customer.
	if s.scenario != nil {
		owner, ok := scenario.CustomerForIngressPort(s.scenario, switchID, uint32(ingress))
		if !ok || owner != customerID {
			http.Error(w, "flow not owned by this customer", http.StatusForbidden)
			return
		}
	}

	flowKey := buildFlowKey(switchID, ingress, egress)
	slog.DebugContext(ctx, fmt.Sprintf("Fetching flow: %s", flowKey), "flowKey", flowKey)

	// Check if flow store is available
	if s.fs == nil {
		slog.ErrorContext(ctx, "Atomix store unavailable", "operation", "read_flows")
		span.SetStatus(codes.Error, "atomix-unavailable")
		http.Error(w, "flow store unavailable", http.StatusServiceUnavailable)
		return
	}

	// Add context timeout for Atomix operation to prevent goroutine starvation
	atomixCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	startAtomixTime := time.Now()
	value, err := s.fs.Get(atomixCtx, flowKey)
	recordAtomixLatency(context.Background(), "read_flows", startAtomixTime, err)

	if err != nil {
		slog.ErrorContext(ctx, "flow store read failed", "operation", "read_flows", "flow_key", flowKey, "error", err)
		span.SetStatus(codes.Error, "error-fetching-flow")
		span.RecordError(err)
		http.Error(w, fmt.Sprintf("error fetching flow: %v", err), http.StatusInternalServerError)
		return
	}
	if value == "" {
		http.Error(w, "flow not found", http.StatusNotFound)
		return
	}

	metrics, err := parseFlowMetricsValue(value)
	if err != nil {
		http.Error(w, fmt.Sprintf("error parsing flow metrics: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]shared.FlowMetricsValue{flowKey: metrics})
}

// customerIDFromRequest returns the customer ID from X-Customer-ID or Authorization: Bearer <customer_id>. Empty if missing.
func customerIDFromRequest(r *http.Request) string {
	if id := r.Header.Get("X-Customer-ID"); id != "" {
		return strings.TrimSpace(id)
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

// POST /bids — requires X-Customer-ID or Authorization: Bearer <customer_id>; validates ingress port ownership.
func (s *Server) postBid(w http.ResponseWriter, r *http.Request) {
	ctx, span := localotel.Tracer.Start(r.Context(), "receive-bid")
	defer span.End()
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	customerID := customerIDFromRequest(r)
	if customerID == "" {
		http.Error(w, "missing customer identity: set X-Customer-ID or Authorization: Bearer <customer_id>", http.StatusUnauthorized)
		return
	}

	var bid shared.BidRequest
	if err := json.NewDecoder(r.Body).Decode(&bid); err != nil {
		span.SetStatus(codes.Error, "invalid-JSON")
		span.RecordError(err)
		slog.ErrorContext(ctx, "invalid JSON", "error", err)
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if err := validateBid(ctx, bid); err != nil {
		recordAPIPolicyViolation(ctx, "bid_validation_failed")
		span.SetStatus(codes.Error, "bid-validation-failed")
		span.RecordError(err)
		slog.ErrorContext(ctx, "bid validation failed", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if s.scenario != nil {
		switchID := ""
		if len(s.scenario.Switches) > 0 {
			switchID = s.scenario.Switches[0].ID
		}
		if switchID != "" {
			owner, ok := scenario.CustomerForIngressPort(s.scenario, switchID, uint32(*bid.IngressPort))
			if !ok || owner != customerID {
				recordAPIPolicyViolation(ctx, "customer_authorization_failed")
				http.Error(w, "ingress port not owned by this customer", http.StatusForbidden)
				return
			}
		}
	}

	// Check if bid store is available
	if s.bs == nil {
		slog.ErrorContext(ctx, "Atomix store unavailable", "operation", "write_bid")
		span.SetStatus(codes.Error, "atomix-unavailable")
		http.Error(w, "bid store unavailable", http.StatusServiceUnavailable)
		return
	}

	// Add context timeout for Atomix operation to prevent goroutine starvation
	atomixCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	startAtomixTime := time.Now()
	err := s.bs.Put(atomixCtx, bid, customerID)
	recordAtomixLatency(context.Background(), "write_bid", startAtomixTime, err)

	if err != nil {
		slog.ErrorContext(ctx, "bid store write failed", "operation", "write_bid", "customer_id", customerID, "error", err)
		span.SetStatus(codes.Error, "bid-storing-failed")
		span.RecordError(err)
		msg := fmt.Sprintf("failed to store bid: %v", err)
		slog.ErrorContext(ctx, msg, "error", err)
		http.Error(w, "failed to store bid", http.StatusInternalServerError)
		return
	}

	slog.DebugContext(ctx, fmt.Sprintf("bid stored for %s in=%d eg=%d", customerID, *bid.IngressPort, *bid.EgressPort))
	slog.DebugContext(ctx, "Bid stored", "customer", customerID, "ingress", *bid.IngressPort, "egress", *bid.EgressPort, "units", *bid.Units, "price", *bid.UnitPrice)

	attrs := metric.WithAttributes(
		attribute.String("customer_id", customerID),
		attribute.Int64("ingress_port", int64(*bid.IngressPort)),
		attribute.Int64("egress_port", int64(*bid.EgressPort)),
	)

	// Record the price and the units requested
	slog.DebugContext(ctx, "Recording bid histogram metrics", "bid_price", *bid.UnitPrice, "bid_units", *bid.Units)
	bidPriceHistogram.Record(ctx, int64(*bid.UnitPrice), attrs)
	bidUnitHistogram.Record(ctx, int64(*bid.Units), attrs)
	slog.DebugContext(ctx, "Bid histogram metrics recorded")

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("bid accepted"))
}

// GET /credits — returns total_spent and optionally starting_balance for the authenticated customer
func (s *Server) getCredits(w http.ResponseWriter, r *http.Request) {
	ctx, span := localotel.Tracer.Start(r.Context(), "get-credits")
	defer span.End()

	if r.Method != http.MethodGet {
		span.SetStatus(codes.Error, "method-not-allowed")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	customerToken := customerIDFromRequest(r)
	span.SetAttributes(attribute.String("customer_id", customerToken))
	if customerToken == "" {
		span.SetStatus(codes.Error, "missing-customer-identity")
		http.Error(w, "missing customer identity: set X-Customer-ID or Authorization: Bearer <customer_id>", http.StatusUnauthorized)
		return
	}

	// Check if credits store is available
	if s.cs == nil {
		slog.ErrorContext(ctx, "Atomix store unavailable", "operation", "read_credits")
		span.SetStatus(codes.Error, "atomix-unavailable")
		http.Error(w, "credits store unavailable", http.StatusServiceUnavailable)
		return
	}

	// Add context timeout for Atomix operation to prevent goroutine starvation
	atomixCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	startAtomixTime := time.Now()
	cred, err := s.cs.Get(atomixCtx, customerToken)
	recordAtomixLatency(context.Background(), "read_credits", startAtomixTime, err)

	if err != nil {
		slog.ErrorContext(ctx, "credits store read failed", "operation", "read_credits", "customer_id", customerToken, "error", err)
		span.SetStatus(codes.Error, "error-fetching-credits")
		span.RecordError(err)
		http.Error(w, fmt.Sprintf("error fetching credits: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cred); err != nil {
		span.SetStatus(codes.Error, "error-encoding-credits-response")
		span.RecordError(err)
		http.Error(w, "error encoding response", http.StatusInternalServerError)
		return
	}
}

// GET /auctions?egress_port=0 — returns auction history (clearing prices and own allocations) for the authenticated customer.
func (s *Server) getAuctions(w http.ResponseWriter, r *http.Request) {
	ctx, span := localotel.Tracer.Start(r.Context(), "get-auctions")
	defer span.End()

	if r.Method != http.MethodGet {
		span.SetStatus(codes.Error, "method-not-allowed")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	customerID := customerIDFromRequest(r)
	span.SetAttributes(attribute.String("customer_id", customerID))
	if customerID == "" {
		span.SetStatus(codes.Error, "missing-customer-identity")
		http.Error(w, "missing customer identity: set X-Customer-ID or Authorization: Bearer <customer_id>", http.StatusUnauthorized)
		return
	}

	var filterEgress uint64
	if egressStr := r.URL.Query().Get("egress_port"); egressStr != "" {
		v, err := strconv.ParseUint(egressStr, 10, 64)
		if err != nil {
			span.SetStatus(codes.Error, "invalid-egress-port")
			span.RecordError(err)
			http.Error(w, "invalid egress_port: must be an integer", http.StatusBadRequest)
			return
		}
		filterEgress = v
	}
	span.SetAttributes(attribute.Int64("egress_port_filter", int64(filterEgress)))

	// Check if auction history store is available
	if s.hs == nil {
		slog.ErrorContext(ctx, "Atomix store unavailable", "operation", "read_auctions")
		span.SetStatus(codes.Error, "atomix-unavailable")
		http.Error(w, "auction history store unavailable", http.StatusServiceUnavailable)
		return
	}

	// Add context timeout for Atomix operation to prevent goroutine starvation
	atomixCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	startAtomixTime := time.Now()
	keys, err := s.hs.List(atomixCtx)
	recordAtomixLatency(context.Background(), "read_auctions", startAtomixTime, err)

	if err != nil {
		slog.ErrorContext(ctx, "auction history list failed", "operation", "read_auctions", "error", err)
		span.SetStatus(codes.Error, "error-listing-auction-history")
		span.RecordError(err)
		http.Error(w, fmt.Sprintf("error listing auction history: %v", err), http.StatusInternalServerError)
		return
	}

	var out []shared.AuctionHistoryRecord

	for _, key := range keys {
		startAtomixTime := time.Now()
		raw, err := s.hs.Get(atomixCtx, key)
		recordAtomixLatency(context.Background(), "read_auctions_item", startAtomixTime, err)

		if err != nil || raw == "" {
			if err != nil {
				slog.ErrorContext(ctx, "auction history read failed", "operation", "read_auctions", "key", key, "error", err)
				span.RecordError(err)
			}
			continue
		}
		var rec shared.AuctionHistoryRecord
		if err := json.Unmarshal([]byte(raw), &rec); err != nil {
			slog.ErrorContext(ctx, "failed to unmarshal auction history", "key", key, "error", err)
			continue
		}
		if filterEgress != 0 && rec.EgressPort != filterEgress {
			continue
		}

		// Filter allocations so the caller only sees its own entries.
		if len(rec.Allocations) > 0 {
			var own []shared.AuctionCustomerAllocation
			for _, alloc := range rec.Allocations {
				if alloc.CustomerID == customerID {
					own = append(own, alloc)
				}
			}
			rec.Allocations = own
		}

		out = append(out, rec)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		span.SetStatus(codes.Error, "error-encoding-auctions-response")
		span.RecordError(err)
		http.Error(w, "error encoding response", http.StatusInternalServerError)
		return
	}
}

// ============================================================
// Helpers
// ============================================================

func parseFlowParams(r *http.Request) (string, int, int, error) {
	q := r.URL.Query()
	switchID := q.Get("switch_id")
	ingressStr := q.Get("ingress_port")
	egressStr := q.Get("egress_port")

	if switchID == "" || ingressStr == "" || egressStr == "" {
		return "", 0, 0, fmt.Errorf("missing required query params: switch_id, ingress_port, egress_port")
	}

	ingress, err := strconv.Atoi(ingressStr)
	if err != nil {
		return "", 0, 0, fmt.Errorf("invalid ingress_port: must be an integer")
	}

	egress, err := strconv.Atoi(egressStr)
	if err != nil {
		return "", 0, 0, fmt.Errorf("invalid egress_port: must be an integer")
	}

	return switchID, ingress, egress, nil
}

func buildFlowKey(switchID string, ingress, egress int) string {
	return fmt.Sprintf("%s|%d|%d", switchID, ingress, egress)
}

func parseFlowKey(key string) (string, int, int, error) {
	parts := strings.Split(key, "|")
	if len(parts) != 3 {
		return "", 0, 0, fmt.Errorf("invalid flow key format: %s", key)
	}

	ingress, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, 0, fmt.Errorf("invalid ingress port in flow key: %s", key)
	}

	egress, err := strconv.Atoi(parts[2])
	if err != nil {
		return "", 0, 0, fmt.Errorf("invalid egress port in flow key: %s", key)
	}

	return parts[0], ingress, egress, nil
}

func validateBid(ctx context.Context, bid shared.BidRequest) error {
	ctx, span := localotel.Tracer.Start(ctx, "bid-validation")
	defer span.End()
	if bid.IngressPort == nil {
		span.SetStatus(codes.Error, "ingress port is required")
		slog.ErrorContext(ctx, "ingress port is required")
		return fmt.Errorf("ingress_port is required")
	}
	if bid.EgressPort == nil {
		span.SetStatus(codes.Error, "egress port is required")
		slog.ErrorContext(ctx, "egress port is required")
		return fmt.Errorf("egress_port is required")
	}
	if bid.Units == nil {
		span.SetStatus(codes.Error, "units is required")
		slog.ErrorContext(ctx, "units is required")
		return fmt.Errorf("units is required")
	}
	if bid.UnitPrice == nil {
		span.SetStatus(codes.Error, "unit_price is required")
		slog.ErrorContext(ctx, "unit_price is required")
		return fmt.Errorf("unit_price is required")
	}
	if *bid.Units <= 0 {
		span.SetStatus(codes.Error, "units must be > 0")
		span.SetAttributes(attribute.Int("units", int(*bid.Units)))
		slog.ErrorContext(ctx, "units must be > 0", "units", *bid.Units)
		return fmt.Errorf("units must be > 0")
	}
	if *bid.UnitPrice <= 0 {
		span.SetStatus(codes.Error, "unit_price must be > 0")
		span.SetAttributes(attribute.Int("unit price", int(*bid.UnitPrice)))
		slog.ErrorContext(ctx, "unit_price must be > 0", "unit price", *bid.UnitPrice)
		return fmt.Errorf("unit_price must be > 0")
	}
	return nil
}
