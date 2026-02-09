package shared

type AuctionResultRecord struct {
	IngressPort   uint64 `json:"ingress_port"`
	EgressPort    uint64 `json:"egress_port"`
	BandwidthKbps uint64 `json:"bandwidth_kbps"`
}

type Flow struct {
	IngressPort uint64 `json:"ingress_port"`
	EgressPort  uint64 `json:"egress_port"`
	Bytes       uint64 `json:"bytes"`
}

type TelemetryRecord struct {
	SwitchID      string `json:"switch_id"`
	WindowStartNS int64  `json:"window_start_ns"`
	WindowEndNS   int64  `json:"window_end_ns"`
	Flows         []Flow `json:"flows"`
}
