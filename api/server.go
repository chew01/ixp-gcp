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
	"time"

	"github.com/chew01/ixp-gcp/shared"
	localotel "github.com/chew01/ixp-gcp/shared/otel"
	"github.com/chew01/ixp-gcp/shared/scenario"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
)

type Server struct {
	fs       FlowStore
	bs       BidStore
	cs       CreditsStore
	hs       AuctionHistoryStore
	us       UtilityStore
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
	ctx, span := localotel.Tracer.Start(r.Context(), "get-flow")
	defer span.End()
	if r.Method != http.MethodGet {
		slog.ErrorContext(ctx, "method not allowed")
		span.SetStatus(codes.Error, "method-not-allowed")
		span.RecordError(fmt.Errorf("method not allowed"))
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	customerID := customerIDFromRequest(r)
	if customerID == "" {
		span.SetStatus(codes.Error, "missing-customer-identity")
		span.RecordError(fmt.Errorf("missing customer identity: set X-Customer-ID or Authorization: Bearer <customer_id>"))
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	switchID, ingress, egress, err := parseFlowParams(r)
	span.SetAttributes(
		attribute.String("switchID", switchID),
		attribute.Int("ingress", ingress),
		attribute.Int("egress", egress),
	)
	if err != nil {
		span.SetStatus(codes.Error, "invalid-flow-params")
		span.RecordError(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Enforce that the requested ingress port belongs to the authenticated customer.
	if s.scenario != nil {
		owner, ok := scenario.CustomerForIngressPort(s.scenario, switchID, uint32(ingress))
		if !ok || owner != customerID {
			slog.WarnContext(ctx, "flow ownership check failed",
				"switch_id", switchID,
				"ingress_port", ingress,
				"egress_port", egress,
				"customer_id", customerID,
			)
			span.SetAttributes(attribute.String("result", "forbidden"))
			span.SetStatus(codes.Error, "forbidden")
			span.RecordError(fmt.Errorf("flow not owned by this customer"))
			w.WriteHeader(http.StatusForbidden)
			return
		}
	}

	flowKey := buildFlowKey(ingress, egress)
	slog.DebugContext(ctx, "Fetching flow", "flow_key", flowKey)

	value, err := s.fs.Get(ctx, flowKey)
	if err != nil {
		slog.ErrorContext(ctx, "flow store read failed", "operation", "read_flows", "flow_key", flowKey, "error", err)
		span.SetStatus(codes.Error, "error-fetching-flow")
		span.RecordError(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if value == "" {
		slog.WarnContext(ctx, "flow not found",
			"flow_key", flowKey,
			"switch_id", switchID,
			"ingress_port", ingress,
			"egress_port", egress,
		)
		span.SetAttributes(attribute.String("result", "not_found"))
		span.SetStatus(codes.Error, "not-found")
		span.RecordError(fmt.Errorf("flow not found"))
		w.WriteHeader(http.StatusNotFound)
		return
	}

	metrics, err := parseFlowMetricsValue(value)
	if err != nil {
		span.SetStatus(codes.Error, "error-parsing-flow-metrics")
		span.RecordError(err)
		w.WriteHeader(http.StatusInternalServerError)
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
	ctx, span := localotel.Tracer.Start(r.Context(), "place-bid")
	defer span.End()
	if r.Method != http.MethodPost {
		span.SetStatus(codes.Error, "method-not-allowed")
		span.RecordError(fmt.Errorf("method not allowed"))
		return
	}

	customerID := customerIDFromRequest(r)
	if customerID == "" {
		span.SetStatus(codes.Error, "missing-customer-identity")
		span.RecordError(fmt.Errorf("missing customer identity: set X-Customer-ID or Authorization: Bearer <customer_id>"))
		return
	}

	span.SetAttributes(attribute.String("customer_id", customerID))

	var bid shared.BidRequest
	if err := json.NewDecoder(r.Body).Decode(&bid); err != nil {
		span.SetStatus(codes.Error, "invalid-JSON")
		span.RecordError(err)
		slog.ErrorContext(ctx, "invalid JSON", "error", err)
		return
	}

	span.SetAttributes(
		attribute.Int64("ingress_port", int64(*bid.IngressPort)),
		attribute.Int64("egress_port", int64(*bid.EgressPort)),
		attribute.Int64("units", int64(*bid.Units)),
		attribute.Int64("unit_price", int64(*bid.UnitPrice)),
	)

	if err := validateBid(ctx, bid); err != nil {
		recordAPIPolicyViolation(ctx, "bid_validation_failed")
		span.SetStatus(codes.Error, "bid-validation-failed")
		span.RecordError(err)
		slog.ErrorContext(ctx, "bid validation failed", "error", err)
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
				span.SetStatus(codes.Error, "forbidden")
				span.RecordError(fmt.Errorf("ingress port not owned by this customer"))
				return
			}
		}
	}

	// Check if bid store is available
	if s.bs == nil {
		slog.ErrorContext(ctx, "Atomix store unavailable", "operation", "write_bid")
		span.SetStatus(codes.Error, "atomix-unavailable")
		span.RecordError(fmt.Errorf("bid store unavailable"))
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
		return
	}

	slog.DebugContext(ctx, fmt.Sprintf("bid stored for %s in=%d eg=%d", customerID, *bid.IngressPort, *bid.EgressPort))
	slog.DebugContext(ctx, "Bid stored", "customer", customerID, "ingress", *bid.IngressPort, "egress", *bid.EgressPort, "units", *bid.Units, "price", *bid.UnitPrice)

	// Increment bid counter for flood detection
	bidCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("customer_id", customerID),
		attribute.Int64("ingress_port", int64(*bid.IngressPort)),
		attribute.Int64("egress_port", int64(*bid.EgressPort)),
	))

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
	switchID := q.Get("switch_id") // optional; used only for ownership verification
	ingressStr := q.Get("ingress_port")
	egressStr := q.Get("egress_port")

	if ingressStr == "" || egressStr == "" {
		return "", 0, 0, fmt.Errorf("missing required query params: ingress_port, egress_port")
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

func buildFlowKey(ingress, egress int) string {
	return fmt.Sprintf("%d|%d", ingress, egress)
}

func parseFlowKey(key string) (int, int, error) {
	parts := strings.Split(key, "|")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid flow key format: %s", key)
	}

	ingress, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid ingress port in flow key: %s", key)
	}

	egress, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid egress port in flow key: %s", key)
	}

	return ingress, egress, nil
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
