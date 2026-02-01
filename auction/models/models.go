package models

// AuctionBid represents a user bid (from Atomix)
type AuctionBid struct {
	IngressPort uint64 `json:"ingress_port"`
	EgressPort  uint64 `json:"egress_port"` // maps to auction
	Units       uint64 `json:"units"`       // bandwidth units (kbps)
	UnitPrice   int    `json:"unit_price"`  // price per unit
	Interval    string
}

// Allocation represents auction output
type Allocation struct {
	IngressPort    uint64 `json:"ingress_port"`
	EgressPort     uint64 `json:"egress_port"` // maps to auction
	AllocatedUnits uint64
	ClearingPrice  int
	Interval       string
}
