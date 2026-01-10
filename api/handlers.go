package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
)

type Server struct {
	store TelemetryStore
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
	value, err := s.store.GetFlow(ctx, flowKey) // returns string throughput
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
