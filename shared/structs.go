package shared

type AuctionResultRecord struct {
	IngressPort   uint64 `json:"ingress_port"`
	EgressPort    uint64 `json:"egress_port"`
	BandwidthKbps uint64 `json:"bandwidth_kbps"`
}
