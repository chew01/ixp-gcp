package main

type Bid struct {
	UserID    string  `json:"user_id"`
	Units     int64   `json:"units"`      // bandwidth units (e.g. Mbps)
	UnitPrice float64 `json:"unit_price"` // price per unit, stored to 4 decimal points
}
