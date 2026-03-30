package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/atomix/go-sdk/pkg/atomix"
	"github.com/atomix/go-sdk/pkg/generic"
	atomixmap "github.com/atomix/go-sdk/pkg/primitive/map"
	"github.com/chew01/ixp-gcp/shared"
)

// DashboardStore holds read-only Atomix map handles used by the dashboard.
type DashboardStore struct {
	flowMap     atomixmap.Map[string, string]
	creditsMap  atomixmap.Map[string, string]
	historyMap  atomixmap.Map[string, string]
	utilityMap  atomixmap.Map[string, string]
	// bids maps are keyed by egress port and opened lazily.
	bidMaps map[uint64]atomixmap.Map[string, string]
}

func NewDashboardStore(ctx context.Context) (*DashboardStore, error) {
	flowMap, err := atomix.Map[string, string]("throughput-map").
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("throughput-map: %w", err)
	}

	creditsMap, err := atomix.Map[string, string]("credits-map").
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("credits-map: %w", err)
	}

	historyMap, err := atomix.Map[string, string]("auction-history").
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("auction-history: %w", err)
	}

	utilityMap, err := atomix.Map[string, string]("utility-map").
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("utility-map: %w", err)
	}

	return &DashboardStore{
		flowMap:    flowMap,
		creditsMap: creditsMap,
		historyMap: historyMap,
		utilityMap: utilityMap,
		bidMaps:    make(map[uint64]atomixmap.Map[string, string]),
	}, nil
}

// BidMapForEgress opens (or reuses) the bids-<egressPort> Atomix map.
func (s *DashboardStore) BidMapForEgress(ctx context.Context, egressPort uint64) (atomixmap.Map[string, string], error) {
	if m, ok := s.bidMaps[egressPort]; ok {
		return m, nil
	}
	m, err := atomix.Map[string, string](fmt.Sprintf("bids-%d", egressPort)).
		Codec(generic.Scalar[string]()).
		Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("bids-%d: %w", egressPort, err)
	}
	s.bidMaps[egressPort] = m
	return m, nil
}

// ---- helpers ----------------------------------------------------------------

func listMap(ctx context.Context, m atomixmap.Map[string, string]) (map[string]string, error) {
	result := make(map[string]string)
	iter, err := m.List(ctx)
	if err != nil {
		return nil, err
	}
	for {
		entry, err := iter.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		result[entry.Key] = entry.Value
	}
	return result, nil
}

// AllFlows returns the current flow metrics keyed by flow-key ("ingress|egress").
func (s *DashboardStore) AllFlows(ctx context.Context) (map[string]shared.FlowMetricsValue, error) {
	raw, err := listMap(ctx, s.flowMap)
	if err != nil {
		return nil, err
	}
	out := make(map[string]shared.FlowMetricsValue, len(raw))
	for k, v := range raw {
		var fmv shared.FlowMetricsValue
		if err := json.Unmarshal([]byte(v), &fmv); err == nil {
			out[k] = fmv
		}
	}
	return out, nil
}

// AllBids returns all pending bids for the given egress port as
// map[ingressPort string] → BidEntry.
func (s *DashboardStore) AllBids(ctx context.Context, egressPort uint64) (map[string]BidEntry, error) {
	m, err := s.BidMapForEgress(ctx, egressPort)
	if err != nil {
		return nil, err
	}
	raw, err := listMap(ctx, m)
	if err != nil {
		return nil, err
	}
	out := make(map[string]BidEntry, len(raw))
	for ingressKey, val := range raw {
		parts := strings.Split(val, "|")
		if len(parts) < 3 {
			continue
		}
		units, _ := strconv.ParseUint(parts[0], 10, 64)
		price, _ := strconv.Atoi(parts[1])
		out[ingressKey] = BidEntry{CustomerID: parts[2], Units: units, UnitPrice: price}
	}
	return out, nil
}

// BidEntry holds a single decoded bid from the Atomix bids map.
type BidEntry struct {
	CustomerID string
	Units      uint64
	UnitPrice  int
}

// AllAuctions returns all AuctionHistoryRecords in chronological key order.
func (s *DashboardStore) AllAuctions(ctx context.Context) ([]shared.AuctionHistoryRecord, error) {
	raw, err := listMap(ctx, s.historyMap)
	if err != nil {
		return nil, err
	}
	out := make([]shared.AuctionHistoryRecord, 0, len(raw))
	for _, v := range raw {
		var rec shared.AuctionHistoryRecord
		if err := json.Unmarshal([]byte(v), &rec); err == nil {
			out = append(out, rec)
		}
	}
	return out, nil
}

// AllCredits returns CustomerCredits for every customer in the credits map.
func (s *DashboardStore) AllCredits(ctx context.Context) (map[string]shared.CustomerCredits, error) {
	raw, err := listMap(ctx, s.creditsMap)
	if err != nil {
		return nil, err
	}
	out := make(map[string]shared.CustomerCredits, len(raw))
	for k, v := range raw {
		var cred shared.CustomerCredits
		if err := json.Unmarshal([]byte(v), &cred); err == nil {
			out[k] = cred
		}
	}
	return out, nil
}

// AllUtility returns cumulative utility per customer.
func (s *DashboardStore) AllUtility(ctx context.Context) (map[string]int, error) {
	raw, err := listMap(ctx, s.utilityMap)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(raw))
	for k, v := range raw {
		out[k], _ = strconv.Atoi(v)
	}
	return out, nil
}

// LatestAuction fetches the most recent AuctionHistoryRecord for a given egress port.
func (s *DashboardStore) LatestAuction(ctx context.Context, egressPort uint64) (*shared.AuctionHistoryRecord, error) {
	records, err := s.AllAuctions(ctx)
	if err != nil {
		return nil, err
	}
	var latest *shared.AuctionHistoryRecord
	for i := range records {
		r := &records[i]
		if r.EgressPort != egressPort {
			continue
		}
		if latest == nil || r.Interval > latest.Interval {
			latest = r
		}
	}
	return latest, nil
}
