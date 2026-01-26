package main

type Bid struct {
	IngressPort *uint64 `json:"ingress_port"`
	EgressPort  *uint64 `json:"egress_port"` // maps to auction
	VlanID      *int    `json:"vlan_id"`
	Units       *uint64 `json:"units"`      // bandwidth units (kbps)
	UnitPrice   *int    `json:"unit_price"` // price per unit
	// TODO: let unit price be float
}

// TODO: eventually map user id to ingress port - vlanid
