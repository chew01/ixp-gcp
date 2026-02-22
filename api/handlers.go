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

	"github.com/chew01/ixp-gcp/shared/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type Server struct {
	fs FlowStore
	bs BidStore
}

func (s *Server) getFlows(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	switchID := q.Get("switch_id")
	ingressStr := q.Get("ingress_port")
	egressStr := q.Get("egress_port")

	// Validate required parameters
	if switchID == "" || ingressStr == "" || egressStr == "" {
		http.Error(w, "Missing required query parameters: switch_id, ingress_port, egress_port", http.StatusBadRequest)
		return
	}

	ingress, err := strconv.Atoi(ingressStr)
	if err != nil {
		http.Error(w, "Invalid ingress_port, must be an integer", http.StatusBadRequest)
		return
	}

	egress, err := strconv.Atoi(egressStr)
	if err != nil {
		http.Error(w, "Invalid egress_port, must be an integer", http.StatusBadRequest)
		return
	}

	// Construct flow key
	flowKey := fmt.Sprintf("%s|%d|%d", switchID, ingress, egress)

	log.Printf("Fetching flow for key: %s", flowKey)

	// Retrieve from Atomix
	ctx := context.Background()
	value, err := s.fs.Get(ctx, flowKey) // returns string throughput
	if err != nil {
		http.Error(w, fmt.Sprintf("Error fetching flow: %v", err), http.StatusInternalServerError)
		return
	}

	// If flow not found
	if value == "" {
		http.Error(w, "Flow not found", http.StatusNotFound)
		return
	}

	log.Printf("Retrieved flow %s: %s", flowKey, value)

	// Return JSON
	resp := map[string]string{
		flowKey: value,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) getMetrics(w http.ResponseWriter, _ *http.Request) {
	var metrics strings.Builder
	for i := 0; i < 10; i++ { // based on telemetry producer
		j := 10
		flowKey := fmt.Sprintf("sw-1|%d|%d", i, j)
		ctx := context.Background()
		value, err := s.fs.Get(ctx, flowKey)
		if err != nil {
			value = "0"
		}
		metricLine := fmt.Sprintf("ixp_flow_throughput_bps{switch=\"sw-1\",ingress_port=\"%d\",egress_port=\"%d\"} %s\n", i, j, value)
		metrics.WriteString(metricLine)

	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(metrics.String()))
}

func (s *Server) postBid(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer.Start(r.Context(), "bid-validation")
	defer span.End()
	if r.Method != http.MethodPost {
		span.SetStatus(codes.Error, "method not allowed")
		span.SetAttributes(attribute.String("method", r.Method))
		slog.ErrorContext(ctx, "method not allowed", "method", r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var bid Bid
	if err := json.NewDecoder(r.Body).Decode(&bid); err != nil {
		span.SetStatus(codes.Error, "invalid JSON")
		span.RecordError(err)
		slog.ErrorContext(ctx, "invalid JSON")
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Validation
	if bid.IngressPort == nil {
		span.SetStatus(codes.Error, "ingress port is required")
		slog.ErrorContext(ctx, "ingress port is required")
		http.Error(w, "ingress port is required", http.StatusBadRequest)
		return
	}
	if bid.EgressPort == nil {
		span.SetStatus(codes.Error, "egress port is required")
		slog.ErrorContext(ctx, "egress port is required")
		http.Error(w, "egress port is required", http.StatusBadRequest)
		return
	}
	if bid.Units == nil {
		span.SetStatus(codes.Error, "unit is required")
		slog.ErrorContext(ctx, "unit is required")
		http.Error(w, "units is required", http.StatusBadRequest)
		return
	}
	if bid.UnitPrice == nil {
		span.SetStatus(codes.Error, "unit price is required")
		slog.ErrorContext(ctx, "unit price is required")
		http.Error(w, "unit price is required", http.StatusBadRequest)
		return
	}
	if *bid.Units <= 0 {
		span.SetStatus(codes.Error, "units must be > 0")
		span.SetAttributes(attribute.Int("units", int(*bid.Units)))
		slog.ErrorContext(ctx, "units must be > 0", "units", *bid.Units)
		http.Error(w, "units must be > 0", http.StatusBadRequest)
		return
	}
	if *bid.UnitPrice <= 0 {
		span.SetStatus(codes.Error, "unit_price must be > 0")
		span.SetAttributes(attribute.Int("unit price", int(*bid.UnitPrice)))
		slog.ErrorContext(ctx, "unit_price must be > 0", "unit price", *bid.UnitPrice)
		http.Error(w, "unit_price must be > 0", http.StatusBadRequest)
		return
	}

	err := s.bs.Put(ctx, bid)
	if err != nil {
		msg := fmt.Sprintf("failed to store bid: %v", err)
		slog.ErrorContext(ctx, msg, "error", err)
		http.Error(w, "failed to store bid", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("bid accepted"))
}
