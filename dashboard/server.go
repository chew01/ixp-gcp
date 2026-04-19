package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"

	"github.com/gorilla/websocket"
)

//go:embed frontend/dist
var frontendDist embed.FS

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Server wires together the stores, hub, and health reporters.
type Server struct {
	store             *DashboardStore
	hub               *Hub
	poller            *Poller
	consumer          *AuctionConsumer // may be nil if Kafka is unavailable
	kafkaBootstrap    string
	resolvedBootstrap string
	apiGatewayURL     string
}

func NewServer(store *DashboardStore, hub *Hub, poller *Poller, consumer *AuctionConsumer, kafkaBootstrap, apiGatewayURL string) *Server {
	return &Server{
		store:             store,
		hub:               hub,
		poller:            poller,
		consumer:          consumer,
		kafkaBootstrap:    kafkaBootstrap,
		resolvedBootstrap: resolveBootstrapIP(kafkaBootstrap),
		apiGatewayURL:     apiGatewayURL,
	}
}

// resolveBootstrapIP resolves the hostname in addr to its first IP address.
// Returns addr unchanged if resolution fails or addr is already an IP.
func resolveBootstrapIP(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if net.ParseIP(host) != nil {
		return addr // already an IP
	}
	ips, err := net.LookupHost(host)
	if err != nil || len(ips) == 0 {
		return addr
	}
	return net.JoinHostPort(ips[0], port)
}

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// Admin REST API.
	mux.HandleFunc("/admin/scenario", s.adminScenario)
	mux.HandleFunc("/admin/auctions", s.adminAuctions)
	mux.HandleFunc("/admin/credits", s.adminCredits)
	mux.HandleFunc("/admin/flows", s.adminFlows)
	mux.HandleFunc("/admin/bids", s.adminBids)
	mux.HandleFunc("/admin/health", s.adminHealth)
	mux.HandleFunc("/admin/bid", s.adminBidProxy)

	// WebSocket endpoint.
	mux.HandleFunc("/ws", s.handleWS)

	// Serve React SPA for everything else.
	distFS, err := fs.Sub(frontendDist, "frontend/dist")
	if err != nil {
		log.Fatalf("failed to sub frontend/dist: %v", err)
	}
	fileServer := http.FileServer(http.FS(distFS))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve index.html for all unknown paths so React Router works.
		if r.URL.Path != "/" {
			f, err := distFS.Open(r.URL.Path[1:])
			if err != nil {
				// Not a real file — serve index.html.
				r.URL.Path = "/"
			} else {
				f.Close()
			}
		}
		fileServer.ServeHTTP(w, r)
	}))
}

// handleWS upgrades the connection, registers the client with the hub and
// starts its read/write pumps.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	client := &Client{hub: s.hub, conn: conn, send: make(chan []byte, 256)}
	s.hub.register <- client
	go client.writePump()
	go client.readPump()
}

// ---- Admin REST handlers ----------------------------------------------------

func (s *Server) adminScenario(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.poller.scenario == nil {
		http.Error(w, "no scenario loaded", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, s.poller.scenario)
}

func (s *Server) adminAuctions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := context.Background()
	records, err := s.store.AllAuctions(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, records)
}

func (s *Server) adminCredits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := context.Background()
	credits, err := s.store.AllCredits(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	utility, _ := s.store.AllUtility(ctx)
	writeJSON(w, map[string]any{"credits": credits, "utility": utility})
}

func (s *Server) adminFlows(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := context.Background()
	flows, err := s.store.AllFlows(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, flows)
}

func (s *Server) adminBids(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.poller.scenario == nil {
		writeJSON(w, map[string]any{})
		return
	}
	ctx := context.Background()
	result := make(map[string]any)
	for _, sw := range s.poller.scenario.Switches {
		for _, ep := range sw.EgressPorts {
			bids, err := s.store.AllBids(ctx, uint64(ep))
			if err == nil {
				result[sw.ID] = bids
			}
		}
	}
	writeJSON(w, result)
}

func (s *Server) adminHealth(w http.ResponseWriter, r *http.Request) {
	kafkaOK := s.consumer != nil && s.consumer.KafkaHealthy()
	atomixOK := s.poller.AtomixHealthy()
	brokers := 0
	if s.consumer != nil {
		brokers = s.consumer.BrokerCount()
	}
	writeJSON(w, map[string]any{
		"atomix":          atomixOK,
		"kafka":           kafkaOK,
		"kafka_bootstrap": s.resolvedBootstrap,
		"kafka_brokers":   brokers,
		"atomix_maps":     s.store.MapNames(),
	})
}

func (s *Server) adminBidProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		CustomerID  string  `json:"customer_id"`
		IngressPort *uint64 `json:"ingress_port"`
		EgressPort  *uint64 `json:"egress_port"`
		Units       *uint64 `json:"units"`
		UnitPrice   *int    `json:"unit_price"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.CustomerID == "" {
		http.Error(w, "customer_id required", http.StatusBadRequest)
		return
	}

	fwdPayload := map[string]any{
		"ingress_port": req.IngressPort,
		"egress_port":  req.EgressPort,
		"units":        req.Units,
		"unit_price":   req.UnitPrice,
	}
	body, _ := json.Marshal(fwdPayload)

	fwdReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.apiGatewayURL+"/bids", bytes.NewReader(body))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	fwdReq.Header.Set("Content-Type", "application/json")
	fwdReq.Header.Set("X-Customer-ID", req.CustomerID)

	resp, err := http.DefaultClient.Do(fwdReq)
	if err != nil {
		http.Error(w, "api gateway unreachable: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}
