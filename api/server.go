// server.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/chew01/ixp-gcp/shared"
	localotel "github.com/chew01/ixp-gcp/shared/otel"
	"github.com/chew01/ixp-gcp/shared/scenario"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
)

var (
	bidMetricsOnce       sync.Once
	flowThroughput       metric.Float64ObservableGauge
	flowDropRate         metric.Float64ObservableGauge
	flowDropKbps         metric.Float64ObservableGauge
	flowEgressKbps       metric.Float64ObservableGauge
	customerCreditsSpent metric.Float64ObservableGauge
	bidPriceHistogram    metric.Int64Histogram
	bidUnitHistogram     metric.Int64Histogram
)

// InitServerMetrics initializes the instruments using the Meter
// created by your boilerplate code.
func (s *Server) InitServerMetrics() {
	bidMetricsOnce.Do(func() {
		var err error

		// Callback for flow throughput metric
		callback_refreshThroughput := func(ctx context.Context, observer metric.Float64Observer) error {
			keys, err := s.fs.List(ctx)
			if err != nil {
				return err
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
			keys, err := s.fs.List(ctx)
			if err != nil {
				return err
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
			keys, err := s.fs.List(ctx)
			if err != nil {
				return err
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
			keys, err := s.fs.List(ctx)
			if err != nil {
				return err
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
			keys, err := s.cs.List(ctx)
			if err != nil {
				slog.ErrorContext(ctx, fmt.Sprintf("failed to list credits keys: %v", err))
				return err
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

	})
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
	// log.Printf("Fetching flow: %s", flowKey)

	value, err := s.fs.Get(ctx, flowKey)
	if err != nil {
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
				http.Error(w, "ingress port not owned by this customer", http.StatusForbidden)
				return
			}
		}
	}

	if err := s.bs.Put(ctx, bid, customerID); err != nil {
		span.SetStatus(codes.Error, "bid-storing-failed")
		span.RecordError(err)
		msg := fmt.Sprintf("failed to store bid: %v", err)
		slog.ErrorContext(ctx, msg, "error", err)
		http.Error(w, "failed to store bid", http.StatusInternalServerError)
		return
	}

	slog.DebugContext(ctx, fmt.Sprintf("bid stored for %s in=%d eg=%d", customerID, *bid.IngressPort, *bid.EgressPort))

	attrs := metric.WithAttributes(
		attribute.String("customer_id", customerID),
		attribute.Int64("ingress_port", int64(*bid.IngressPort)),
		attribute.Int64("egress_port", int64(*bid.EgressPort)),
	)

	// Record the price and the units requested
	bidPriceHistogram.Record(ctx, int64(*bid.UnitPrice), attrs)
	bidUnitHistogram.Record(ctx, int64(*bid.Units), attrs)

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("bid accepted"))
}

// GET /credits — returns total_spent and optionally starting_balance for the authenticated customer
func (s *Server) getCredits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	customerToken := customerIDFromRequest(r)
	if customerToken == "" {
		http.Error(w, "missing customer identity: set X-Customer-ID or Authorization: Bearer <customer_id>", http.StatusUnauthorized)
		return
	}

	cred, err := s.cs.Get(r.Context(), customerToken)
	if err != nil {
		http.Error(w, fmt.Sprintf("error fetching credits: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cred)
}

// GET /auctions?egress_port=0 — returns auction history (clearing prices and own allocations) for the authenticated customer.
func (s *Server) getAuctions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	customerID := customerIDFromRequest(r)
	if customerID == "" {
		http.Error(w, "missing customer identity: set X-Customer-ID or Authorization: Bearer <customer_id>", http.StatusUnauthorized)
		return
	}

	var filterEgress uint64
	if egressStr := r.URL.Query().Get("egress_port"); egressStr != "" {
		v, err := strconv.ParseUint(egressStr, 10, 64)
		if err != nil {
			http.Error(w, "invalid egress_port: must be an integer", http.StatusBadRequest)
			return
		}
		filterEgress = v
	}

	keys, err := s.hs.List(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("error listing auction history: %v", err), http.StatusInternalServerError)
		return
	}

	var out []shared.AuctionHistoryRecord

	for _, key := range keys {
		raw, err := s.hs.Get(r.Context(), key)
		if err != nil || raw == "" {
			continue
		}
		var rec shared.AuctionHistoryRecord
		if err := json.Unmarshal([]byte(raw), &rec); err != nil {
			log.Printf("failed to unmarshal auction history %s: %v", key, err)
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
	json.NewEncoder(w).Encode(out)
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
		span.SetStatus(codes.Error, "unit is required")
		slog.ErrorContext(ctx, "unit is required")
		return fmt.Errorf("units is required")
	}
	if bid.UnitPrice == nil {
		span.SetStatus(codes.Error, "unit price is required")
		slog.ErrorContext(ctx, "unit price is required")
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
