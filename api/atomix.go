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

func (s *AtomixBidStore) Put(ctx context.Context, bid Bid) error {
	mapID := fmt.Sprintf("bids-%d", *bid.EgressPort)
	bidMap, err := atomix.Map[string, string](mapID).
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		log.Printf("Error getting bid map: %v", err)
		return err
	}

	identifier := fmt.Sprintf("%d|%d", *bid.IngressPort, *bid.VlanID)
	bidValue := fmt.Sprintf("%d|%d", *bid.Units, *bid.UnitPrice)
	log.Printf("Putting %s to %s", bidValue, identifier)
	_, err = bidMap.Put(ctx, identifier, bidValue)
	if err != nil {
		log.Printf("Error putting bid for ingress port %d VLAN ID %d: %v", bid.IngressPort, bid.VlanID, err)
		return err
	}

	length, err := bidMap.Len(ctx)
	if err != nil {
		log.Printf("Error getting bid map length: %v", err)
	} else {
		log.Printf("%d bids in %s", length, mapID)
	}
	return nil
}
