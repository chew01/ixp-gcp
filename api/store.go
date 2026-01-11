package main

import "context"

type FlowStore interface {
	// Key: sw-1|1|5
	Get(ctx context.Context, flowKey string) (string, error)
}

type BidStore interface {
	Put(ctx context.Context, userID string, units int64, unitPrice float64) error
}
