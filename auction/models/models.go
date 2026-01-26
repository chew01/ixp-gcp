package models

// Bid represents a user bid (from Atomix)
type Bid struct {
	IngressPort uint64 `json:"ingress_port"`
	EgressPort  uint64 `json:"egress_port"` // maps to auction
	VlanID      int    `json:"vlan_id"`
	Units       uint64 `json:"units"`      // bandwidth units (kbps)
	UnitPrice   int    `json:"unit_price"` // price per unit
	Interval    string
}

// Allocation represents auction output
type Allocation struct {
	IngressPort    uint64 `json:"ingress_port"`
	EgressPort     uint64 `json:"egress_port"` // maps to auction
	VlanID         int    `json:"vlan_id"`
	AllocatedUnits uint64
	ClearingPrice  int
	Interval       string
}
