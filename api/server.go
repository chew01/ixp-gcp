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
	"github.com/chew01/ixp-gcp/shared/scenario"
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

	customerCreditsSpent = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ixp_customer_credits_spent_total",
		Help: "Total credits spent by customer (accounting only)",
	}, []string{"customer_id"})
)

func init() {
	prometheus.MustRegister(flowThroughput, flowEgressKbps, flowDropKbps, flowDropRate, customerCreditsSpent)
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
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	customerID := customerIDFromRequest(r)
	if customerID == "" {
		http.Error(w, "missing customer identity: set X-Customer-ID or Authorization: Bearer <customer_id>", http.StatusUnauthorized)
		return
	}

	switchID, ingress, egress, err := parseFlowParams(r)
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
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if err := validateBid(bid); err != nil {
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

	if err := s.bs.Put(r.Context(), bid, customerID); err != nil {
		log.Printf("failed to store bid: %v", err)
		http.Error(w, "failed to store bid", http.StatusInternalServerError)
		return
	}
	log.Printf("bid stored for %s in=%d eg=%d", customerID, *bid.IngressPort, *bid.EgressPort)

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

// GET /metrics — Prometheus scrape endpoint
func (s *Server) getMetrics(w http.ResponseWriter, r *http.Request) {
	s.refreshMetrics(r.Context())
	s.refreshCreditsMetrics(r.Context())
	promhttp.Handler().ServeHTTP(w, r)
}

func (s *Server) refreshCreditsMetrics(ctx context.Context) {
	keys, err := s.cs.List(ctx)
	if err != nil {
		log.Printf("failed to list credits keys: %v", err)
		return
	}
	for _, customerID := range keys {
		cred, err := s.cs.Get(ctx, customerID)
		if err != nil {
			continue
		}
		customerCreditsSpent.With(prometheus.Labels{"customer_id": customerID}).Set(float64(cred.TotalSpent))
	}
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
