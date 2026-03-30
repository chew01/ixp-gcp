package main

import (
	"encoding/json"
	"time"

	"github.com/chew01/ixp-gcp/shared"
)

// WSEvent is the envelope for every message sent over the WebSocket.
// The "from" and "to" fields use stable node IDs that match the React Flow
// topology graph so the frontend can look up the right edge to animate.
type WSEvent struct {
	Type    string          `json:"type"`
	From    string          `json:"from,omitempty"`
	To      string          `json:"to,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

func newEvent(typ, from, to string, payload any) (WSEvent, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return WSEvent{}, err
	}
	return WSEvent{Type: typ, From: from, To: to, Payload: b}, nil
}

// BidPayload is the payload for "bid" events.
type BidPayload struct {
	CustomerID  string    `json:"customer_id"`
	EgressPort  uint64    `json:"egress_port"`
	IngressPort string    `json:"ingress_port"`
	Units       uint64    `json:"units"`
	UnitPrice   int       `json:"unit_price"`
	Timestamp   time.Time `json:"timestamp"`
}

// AuctionPayload is the payload for "auction" events.
type AuctionPayload struct {
	shared.AuctionHistoryRecord
	Timestamp time.Time `json:"timestamp"`
}

// TelemetryPayload is the payload for "telemetry" events.
type TelemetryPayload struct {
	Flows map[string]shared.FlowMetricsValue `json:"flows"`
}

// PodInfo describes a single workload's replica state.
type PodInfo struct {
	Desired int32 `json:"desired"`
	Ready   int32 `json:"ready"`
}

// PodsPayload maps workload name → PodInfo.
type PodsPayload map[string]PodInfo

// AtomixRWPayload is the payload for "atomix_rw" events (map read/write animations).
type AtomixRWPayload struct {
	Op  string `json:"op"`
	Map string `json:"map"`
}
