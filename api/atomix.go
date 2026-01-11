package main

import (
	"context"
	"fmt"
	"log"

	"github.com/atomix/go-sdk/pkg/atomix"
	"github.com/atomix/go-sdk/pkg/generic"
)

type AtomixFlowStore struct{}

func (s *AtomixFlowStore) Get(ctx context.Context, flowKey string) (string, error) {
	throughputMap, err := atomix.Map[string, string]("throughput-map").
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		log.Printf("Error getting throughput map: %v", err)
	}

	entry, err := throughputMap.Get(ctx, flowKey)
	if err != nil {
		log.Printf("Error getting flow %s: %v", flowKey, err)
		return "", err
	}
	return entry.Value, nil
}

type AtomixBidStore struct{}

func (s *AtomixBidStore) Put(ctx context.Context, userID string, units int64, unitPrice float64) error {
	bidMap, err := atomix.Map[string, string]("bid-map").
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		log.Printf("Error getting bid map: %v", err)
		return err
	}

	bidValue := fmt.Sprintf("%d|%.4f", units, unitPrice)
	_, err = bidMap.Put(ctx, userID, bidValue)
	if err != nil {
		log.Printf("Error putting bid for user %s: %v", userID, err)
		return err
	}

	len, err := bidMap.Len(ctx)
	if err != nil {
		log.Printf("Error getting bid map length: %v", err)
		len = -1
	}
	log.Printf("%d bids", len)
	return nil
}
