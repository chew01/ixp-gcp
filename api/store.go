package main

import "context"

type TelemetryStore interface {
	// Key: sw-1|1|5
	GetFlow(ctx context.Context, flowKey string) (string, error)
}
