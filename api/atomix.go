package main

import (
	"context"
	"encoding/json"
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
		// Treat missing keys as "not found" and let callers decide how to surface
		// this (e.g. 404 from the API or a skipped flow in an agent).
		return "", nil
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

// ============================================================
// AuctionHistoryStore
// ============================================================

type AuctionHistoryStore interface {
	Get(ctx context.Context, key string) (string, error)
	List(ctx context.Context) ([]string, error)
}

type AtomixAuctionHistoryStore struct {
	historyMap atomixmap.Map[string, string]
}

func NewAtomixAuctionHistoryStore(ctx context.Context) (*AtomixAuctionHistoryStore, error) {
	m, err := atomix.Map[string, string]("auction-history").
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to init auction history map: %w", err)
	}
	return &AtomixAuctionHistoryStore{historyMap: m}, nil
}

func (s *AtomixAuctionHistoryStore) Get(ctx context.Context, key string) (string, error) {
	entry, err := s.historyMap.Get(ctx, key)
	if err != nil {
		return "", fmt.Errorf("auction history %s not found: %w", key, err)
	}
	return entry.Value, nil
}

func (s *AtomixAuctionHistoryStore) List(ctx context.Context) ([]string, error) {
	var keys []string
	entries, err := s.historyMap.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list auction history: %w", err)
	}
	for {
		entry, err := entries.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("failed to iterate auction history: %w", err)
		}
		keys = append(keys, entry.Key)
	}
	return keys, nil
}

// ============================================================
// CreditsStore
// ============================================================

type CreditsStore interface {
	Get(ctx context.Context, customerID string) (shared.CustomerCredits, error)
	List(ctx context.Context) ([]string, error) // customer IDs
	AddSpent(ctx context.Context, customerID string, amount int) error
	// InitCustomerIfMissing ensures the customer has a credits entry; no-op if already present.
	// startingBalance sets the finite credit budget (0 = unlimited / no budget constraint).
	InitCustomerIfMissing(ctx context.Context, customerID string, startingBalance int) error
}

type AtomixCreditsStore struct {
	creditsMap atomixmap.Map[string, string]
}

func NewAtomixCreditsStore(ctx context.Context) (*AtomixCreditsStore, error) {
	m, err := atomix.Map[string, string]("credits-map").
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to init credits map: %w", err)
	}
	return &AtomixCreditsStore{creditsMap: m}, nil
}

func (s *AtomixCreditsStore) Get(ctx context.Context, customerID string) (shared.CustomerCredits, error) {
	entry, err := s.creditsMap.Get(ctx, customerID)
	if err != nil {
		return shared.CustomerCredits{}, nil // no entry yet
	}
	var cred shared.CustomerCredits
	if err := json.Unmarshal([]byte(entry.Value), &cred); err != nil {
		return shared.CustomerCredits{}, fmt.Errorf("invalid credits value for %s: %w", customerID, err)
	}
	return cred, nil
}

func (s *AtomixCreditsStore) List(ctx context.Context) ([]string, error) {
	var keys []string
	entries, err := s.creditsMap.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list credits: %w", err)
	}
	for {
		entry, err := entries.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("iterate credits: %w", err)
		}
		keys = append(keys, entry.Key)
	}
	return keys, nil
}

func (s *AtomixCreditsStore) AddSpent(ctx context.Context, customerID string, amount int) error {
	cred, _ := s.Get(ctx, customerID)
	cred.TotalSpent += amount
	b, err := json.Marshal(cred)
	if err != nil {
		return fmt.Errorf("marshal credits: %w", err)
	}
	if _, err := s.creditsMap.Put(ctx, customerID, string(b)); err != nil {
		return fmt.Errorf("update credits for %s: %w", customerID, err)
	}
	return nil
}

// InitCustomerIfMissing creates a credits entry for the customer if the key is not yet in the map.
// Existing entries are left unchanged so total_spent is never overwritten on redeploy.
// startingBalance is stored in the entry so budget-aware strategies can access it; 0 = unlimited.
func (s *AtomixCreditsStore) InitCustomerIfMissing(ctx context.Context, customerID string, startingBalance int) error {
	_, err := s.creditsMap.Get(ctx, customerID)
	if err == nil {
		return nil // already has an entry
	}
	cred := shared.CustomerCredits{StartingBalance: startingBalance}
	b, err := json.Marshal(cred)
	if err != nil {
		return fmt.Errorf("marshal credits: %w", err)
	}
	if _, err := s.creditsMap.Put(ctx, customerID, string(b)); err != nil {
		return fmt.Errorf("init credits for %s: %w", customerID, err)
	}
	return nil
}
