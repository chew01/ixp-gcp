package main

type FlowRecord struct {
	FlowID        string `json:"flow_id"`
	SwitchID      string `json:"switch_id"`
	IngressPort   int    `json:"ingress_port"`
	EgressPort    int    `json:"egress_port"`
	ThroughputBps string `json:"throughput_bps"`
	WindowEndNS   int64  `json:"window_end_ns"`
}
