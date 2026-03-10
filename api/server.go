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

	"github.com/chew01/ixp-gcp/shared"
	localotel "github.com/chew01/ixp-gcp/shared/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
)

var (
	bidMetricsOnce    sync.Once
	flowThroughput    metric.Float64ObservableGauge
	flowDropRate      metric.Float64ObservableGauge
	bidPriceHistogram metric.Int64Histogram
	bidUnitHistogram  metric.Int64Histogram
)

// InitServerMetrics initializes the instruments using the Meter
// created by your boilerplate code.
func (s *Server) InitServerMetrics() {
	bidMetricsOnce.Do(func() {
		var err error

		// OTel will call this automatically every 3 seconds (based on otel.go config).
		callback := func(ctx context.Context, observer metric.Float64Observer) error {
			// 1. Get all flow keys from Atomix (e.g., ["sw-1:1:10", "sw-1:2:20"])
			keys, err := s.fs.List(ctx)
			if err != nil {
				return err
			}

			for _, key := range keys {
				// 2. Fetch the value for this specific flow
				val, err := s.fs.Get(ctx, key)
				if err != nil {
					continue // Skip if a specific flow fails
				}

				// 3. Parse the key and value (using your existing logic)
				swID, ingress, egress, _ := parseFlowKey(key)
				byteCount, _ := strconv.ParseFloat(val, 64)

				attrs := attribute.NewSet(
					attribute.String("switch_id", swID),
					attribute.Int("ingress_port", ingress),
					attribute.Int("egress_port", egress),
				)

				// 4. Record the observation
				observer.Observe(byteCount, metric.WithAttributeSet(attrs))
			}
			return nil
		}

		// localotel.Meter is initialized in your otel-init.go
		flowThroughput, err = localotel.Meter.Float64ObservableGauge(
			"ixp.flow.throughput",
			metric.WithDescription("Flow throughput in Kbps"),
			metric.WithUnit("kbps"),
			metric.WithFloat64Callback(callback),
		)
		flowDropRate, err = localotel.Meter.Float64ObservableGauge(
			"ixp.flow.drop_rate",
			metric.WithDescription("Flow packet drop rate as a percentage"),
			metric.WithUnit("%"),
		)
		if err != nil {
			slog.Error("Failed to initialise flowThroughput and flowDropRate metrics", "error", err)
		}

		// Initialize Bid Price Histogram
		bidPriceHistogram, err = localotel.Meter.Int64Histogram(
			"ixp.bid.price",
			metric.WithDescription("Distribution of bid unit prices"),
			metric.WithUnit("SGD"), // or whatever your currency is
		)

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
	fs FlowStore
	bs BidStore
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{flowKey: value})
}

// POST /bids
func (s *Server) postBid(w http.ResponseWriter, r *http.Request) {
	ctx, span := localotel.Tracer.Start(r.Context(), "receive-bid")
	defer span.End()
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
	attrs := metric.WithAttributes(
		attribute.Int64("ingress_port", int64(*bid.IngressPort)),
		attribute.Int64("egress_port", int64(*bid.EgressPort)),
	)

	// Record the price and the units requested
	bidPriceHistogram.Record(ctx, int64(*bid.UnitPrice), attrs)
	bidUnitHistogram.Record(ctx, int64(*bid.Units), attrs)
	if err := s.bs.Put(ctx, bid); err != nil {
		span.SetStatus(codes.Error, "bid-storing-failed")
		span.RecordError(err)
		slog.ErrorContext(ctx, "bid storing failed", "error", err)
		http.Error(w, "failed to store bid", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("bid accepted"))
}

// GET /metrics — Prometheus scrape endpoint replaced with callback function by OpenTelemetry which pushes periodically to otel collector and promehteus scrape from otel collector
// func (s *Server) getMetrics(w http.ResponseWriter, r *http.Request) {
// 	s.refreshMetrics(r.Context())
// 	promhttp.Handler().ServeHTTP(w, r)
// }

// func (s *Server) refreshMetrics(ctx context.Context) {
// 	keys, err := s.fs.List(ctx)
// 	if err != nil {
// 		log.Printf("failed to list flow keys: %v", err)
// 		return
// 	}

// 	for _, flowKey := range keys {
// 		switchID, ingress, egress, err := parseFlowKey(flowKey)
// 		if err != nil {
// 			log.Printf("failed to parse flow key: %v", err)
// 			continue
// 		}

// 		throughput, err := s.fs.Get(ctx, flowKey)
// 		if err != nil || throughput == "" {
// 			throughput = "0"
// 		}

// 		var kbps float64
// 		fmt.Sscanf(throughput, "%f", &kbps)

// 		// labels := prometheus.Labels{
// 		// 	"switch_id":    switchID,
// 		// 	"ingress_port": fmt.Sprint(ingress),
// 		// 	"egress_port":  fmt.Sprint(egress),
// 		// }

// 		// flowThroughput.With(labels).Set(kbps)
// 		attrs := metric.WithAttributes(
// 			attribute.String("switch_id", switchID),
// 			attribute.Int("ingress_port", ingress),
// 			attribute.Int("egress_port", egress),
// 		)
// 		flowThroughput.Record(ctx, float64(kbps), attrs)
// 	}
// }

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
