package main

import (
	"context"

	"github.com/chew01/ixp-gcp/shared"
)

type FlowStore interface {
	// Key: sw-1|1|5
	Get(ctx context.Context, flowKey string) (string, error)
}

type BidStore interface {
	Put(ctx context.Context, bid shared.BidRequest) error
}
