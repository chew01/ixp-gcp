// server.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/chew01/ixp-gcp/shared"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	flowThroughput = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ixp_flow_throughput_kbps",
		Help: "Flow throughput in Kbps",
	}, []string{"switch_id", "ingress_port", "egress_port"})

	flowEgressKbps = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ixp_flow_egress_kbps",
		Help: "Flow egress throughput in Kbps",
	}, []string{"switch_id", "ingress_port", "egress_port"})

	flowDropKbps = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ixp_flow_drop_kbps",
		Help: "Flow drop rate in Kbps",
	}, []string{"switch_id", "ingress_port", "egress_port"})

	flowDropRate = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ixp_flow_drop_rate_percent",
		Help: "Flow packet drop rate as a percentage",
	}, []string{"switch_id", "ingress_port", "egress_port"})
)

func init() {
	prometheus.MustRegister(flowThroughput, flowEgressKbps, flowDropKbps, flowDropRate)
}

type Server struct {
	fs FlowStore
	bs BidStore
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
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	switchID, ingress, egress, err := parseFlowParams(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	flowKey := buildFlowKey(switchID, ingress, egress)
	log.Printf("Fetching flow: %s", flowKey)

	value, err := s.fs.Get(r.Context(), flowKey)
	if err != nil {
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

// POST /bids
func (s *Server) postBid(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var bid shared.BidRequest
	if err := json.NewDecoder(r.Body).Decode(&bid); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if err := validateBid(bid); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.bs.Put(r.Context(), bid); err != nil {
		log.Printf("failed to store bid: %v", err)
		http.Error(w, "failed to store bid", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("bid accepted"))
}

// GET /metrics — Prometheus scrape endpoint
func (s *Server) getMetrics(w http.ResponseWriter, r *http.Request) {
	s.refreshMetrics(r.Context())
	promhttp.Handler().ServeHTTP(w, r)
}

func (s *Server) refreshMetrics(ctx context.Context) {
	keys, err := s.fs.List(ctx)
	if err != nil {
		log.Printf("failed to list flow keys: %v", err)
		return
	}

	for _, flowKey := range keys {
		switchID, ingress, egress, err := parseFlowKey(flowKey)
		if err != nil {
			log.Printf("failed to parse flow key: %v", err)
			continue
		}

		raw, err := s.fs.Get(ctx, flowKey)
		if err != nil || raw == "" {
			raw = "0"
		}

		metrics, err := parseFlowMetricsValue(raw)
		if err != nil {
			log.Printf("failed to parse flow metrics for %s: %v", flowKey, err)
			continue
		}

		labels := prometheus.Labels{
			"switch_id":    switchID,
			"ingress_port": fmt.Sprint(ingress),
			"egress_port":  fmt.Sprint(egress),
		}

		flowThroughput.With(labels).Set(metrics.ThroughputKbps)
		flowEgressKbps.With(labels).Set(metrics.EgressKbps)
		flowDropKbps.With(labels).Set(metrics.DropKbps)
		flowDropRate.With(labels).Set(metrics.DropRatePct)
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

func validateBid(bid shared.BidRequest) error {
	if bid.IngressPort == nil {
		return fmt.Errorf("ingress_port is required")
	}
	if bid.EgressPort == nil {
		return fmt.Errorf("egress_port is required")
	}
	if bid.Units == nil {
		return fmt.Errorf("units is required")
	}
	if bid.UnitPrice == nil {
		return fmt.Errorf("unit_price is required")
	}
	if *bid.Units <= 0 {
		return fmt.Errorf("units must be > 0")
	}
	if *bid.UnitPrice <= 0 {
		return fmt.Errorf("unit_price must be > 0")
	}
	return nil
}
