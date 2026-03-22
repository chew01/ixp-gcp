package shared

// FlowMetricsValue is the stored value format for flow metrics (one per flow key in the throughput map).
type FlowMetricsValue struct {
	ThroughputKbps float64 `json:"throughput_kbps"` // ingress Kbps
	EgressKbps     float64 `json:"egress_kbps"`
	DropKbps       float64 `json:"drop_kbps"`
	DropRatePct    float64 `json:"drop_rate_pct"`
}

type BidRequest struct {
	IngressPort *uint64 `json:"ingress_port"`
	EgressPort  *uint64 `json:"egress_port"` // maps to auction
	Units       *uint64 `json:"units"`       // bandwidth units (kbps)
	UnitPrice   *int    `json:"unit_price"`  // price per unit
}

type AuctionResultRecord struct {
	IngressPort   uint64 `json:"ingress_port"`
	EgressPort    uint64 `json:"egress_port"`
	BandwidthKbps uint64 `json:"bandwidth_kbps"`
}

// AuctionCustomerAllocation is a per-customer view of an auction allocation.
// It is used in auction history records and filtered per customer by the API.
type AuctionCustomerAllocation struct {
	CustomerID  string `json:"customer_id"`
	IngressPort uint64 `json:"ingress_port"`
	Units       uint64 `json:"units"`
}

// AuctionHistoryRecord stores the clearing price and (optionally) per-customer
// allocations for a single auction interval and egress port.
type AuctionHistoryRecord struct {
	Interval      string                      `json:"interval"`
	EgressPort    uint64                      `json:"egress_port"`
	ClearingPrice int                         `json:"clearing_price"`
	Allocations   []AuctionCustomerAllocation `json:"allocations,omitempty"`
}

type Flow struct {
	IngressPort  uint32 `json:"ingress_port"`
	EgressPort   uint32 `json:"egress_port"`
	SourceVLANID uint32 `json:"source_vlan_id"`
	DestVLANID   uint32 `json:"dest_vlan_id"`
}

type TelemetryRecord struct {
	FlowID      Flow   `json:"flow_id"`
	RxByteCount uint64 `json:"rx_byte_count"` // Receive byte count
	TxByteCount uint64 `json:"tx_byte_count"` // Transmit byte count
}

// CustomerCredits is the value format for the credits map (key = customer ID).
type CustomerCredits struct {
	TotalSpent      int `json:"total_spent"`
	StartingBalance int `json:"starting_balance,omitempty"`
}
