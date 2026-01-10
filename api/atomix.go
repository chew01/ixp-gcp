package main

import (
	"context"
	"log"

	"github.com/atomix/go-sdk/pkg/atomix"
	"github.com/atomix/go-sdk/pkg/generic"
)

type AtomixStore struct{}

func (s *AtomixStore) GetFlow(ctx context.Context, flowKey string) (string, error) {
	throughputMap, err := atomix.Map[string, string]("throughput-map").
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		log.Printf("Error getting throughput map: %v", err)
	}
	// Placeholder implementation
	entry, err := throughputMap.Get(ctx, flowKey)
	if err != nil {
		log.Printf("Error getting flow %s: %v", flowKey, err)
		return "", err
	}
	return entry.Value, nil
}
