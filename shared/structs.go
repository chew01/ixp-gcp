package shared

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
