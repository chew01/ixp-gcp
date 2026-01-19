package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
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

func (s *Server) getMetrics(w http.ResponseWriter, r *http.Request) {
	var metrics strings.Builder
	for i := 1; i <= 4; i++ {
		for j := 5; j <= 8; j++ {
			flowKey := fmt.Sprintf("sw-1|%d|%d", i, j)
			ctx := context.Background()
			value, err := s.fs.Get(ctx, flowKey)
			if err != nil {
				value = "0"
			}
			metricLine := fmt.Sprintf("ixp_flow_throughput_bps{switch=\"sw-1\",ingress_port=\"%d\",egress_port=\"%d\"} %s\n", i, j, value)
			metrics.WriteString(metricLine)
		}
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(metrics.String()))
}

func (s *Server) postBid(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var bid Bid
	if err := json.NewDecoder(r.Body).Decode(&bid); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Validation
	if bid.UserID == "" {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return
	}
	if bid.Units <= 0 {
		http.Error(w, "units must be > 0", http.StatusBadRequest)
		return
	}
	if bid.UnitPrice <= 0 {
		http.Error(w, "unit_price must be > 0", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	err := s.bs.Put(ctx, bid.UserID, bid.Units, bid.UnitPrice)
	if err != nil {
		log.Printf("failed to store bid: %v", err)
		http.Error(w, "failed to store bid", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("bid accepted"))
}
