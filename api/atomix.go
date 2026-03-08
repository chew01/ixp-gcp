package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"

	"github.com/atomix/go-sdk/pkg/atomix"
	"github.com/atomix/go-sdk/pkg/generic"
	atomixmap "github.com/atomix/go-sdk/pkg/primitive/map"
	"github.com/chew01/ixp-gcp/shared"
)

// ============================================================
// FlowStore
// ============================================================

type FlowStore interface {
	Get(ctx context.Context, flowKey string) (string, error)
	List(ctx context.Context) ([]string, error)
}

type AtomixFlowStore struct {
	throughputMap atomixmap.Map[string, string]
}

func NewAtomixFlowStore(ctx context.Context) (*AtomixFlowStore, error) {
	m, err := atomix.Map[string, string]("throughput-map").
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to init throughput map: %w", err)
	}
	return &AtomixFlowStore{throughputMap: m}, nil
}

func (s *AtomixFlowStore) Get(ctx context.Context, flowKey string) (string, error) {
	entry, err := s.throughputMap.Get(ctx, flowKey)
	if err != nil {
		return "", fmt.Errorf("flow %s not found: %w", flowKey, err)
	}
	return entry.Value, nil
}

func (s *AtomixFlowStore) List(ctx context.Context) ([]string, error) {
	var keys []string
	entries, err := s.throughputMap.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list flows: %w", err)
	}
	for {
		entry, err := entries.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("failed to iterate flows: %w", err)
		}
		keys = append(keys, entry.Key)
	}
	return keys, nil
}

// ============================================================
// BidStore
// ============================================================

type BidStore interface {
	Put(ctx context.Context, bid shared.BidRequest, customerID string) error
}

type AtomixBidStore struct {
	// keyed by egress port, initialized lazily since ports are dynamic
	maps map[uint32]atomixmap.Map[string, string]
}

func NewAtomixBidStore() *AtomixBidStore {
	return &AtomixBidStore{
		maps: make(map[uint32]atomixmap.Map[string, string]),
	}
}

func (s *AtomixBidStore) getOrCreateMap(ctx context.Context, egressPort uint32) (atomixmap.Map[string, string], error) {
	if m, ok := s.maps[egressPort]; ok {
		return m, nil
	}

	mapID := fmt.Sprintf("bids-%d", egressPort)
	m, err := atomix.Map[string, string](mapID).
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to init bid map %s: %w", mapID, err)
	}

	s.maps[egressPort] = m
	return m, nil
}

func (s *AtomixBidStore) Put(ctx context.Context, bid shared.BidRequest, customerID string) error {
	bidMap, err := s.getOrCreateMap(ctx, uint32(*bid.EgressPort))
	if err != nil {
		return err
	}

	key := fmt.Sprintf("%d", *bid.IngressPort)
	// Value format: units|unitPrice|customerID (last-write-wins per ingress/egress)
	value := fmt.Sprintf("%d|%d|%s", *bid.Units, *bid.UnitPrice, customerID)

	if _, err := bidMap.Put(ctx, key, value); err != nil {
		return fmt.Errorf("failed to store bid for ingress port %d: %w", *bid.IngressPort, err)
	}

	length, err := bidMap.Len(ctx)
	if err != nil {
		log.Printf("failed to get bid map length: %v", err)
	} else {
		log.Printf("[bids-%d] %d bids stored", *bid.EgressPort, length)
	}

	return nil
}
