package models

// Bid represents a user bid (from Atomix)
type Bid struct {
	UserID    string
	Units     int64
	UnitPrice float64
	Interval  string
}

// Allocation represents auction output
type Allocation struct {
	UserID         string
	AllocatedUnits int64
	ClearingPrice  float64
	Interval       string
}
