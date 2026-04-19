package main

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/chew01/ixp-gcp/shared"
	"github.com/chew01/ixp-gcp/shared/scenario"
)

// Poller watches Atomix maps for changes and emits WebSocket events via the Hub.
type Poller struct {
	store    *DashboardStore
	hub      *Hub
	scenario *scenario.Scenario

	// prevBids tracks the last-seen bids snapshot: egressPort → (ingressKey → BidEntry)
	prevBids map[uint64]map[string]BidEntry
	// prevFlows tracks the last-seen flow metrics snapshot
	prevFlows map[string]shared.FlowMetricsValue

	// atomixHealthy is accessed atomically (1 = healthy, 0 = degraded).
	atomixHealthy atomic.Int32
}

func NewPoller(store *DashboardStore, hub *Hub, scene *scenario.Scenario) *Poller {
	p := &Poller{
		store:     store,
		hub:       hub,
		scenario:  scene,
		prevBids:  make(map[uint64]map[string]BidEntry),
		prevFlows: make(map[string]shared.FlowMetricsValue),
	}
	p.atomixHealthy.Store(1)
	return p
}

func (p *Poller) AtomixHealthy() bool {
	return p.atomixHealthy.Load() == 1
}

// Run starts the polling loop. Call in a goroutine.
func (p *Poller) Run(ctx context.Context) {
	bidTicker := time.NewTicker(2 * time.Second)
	flowTicker := time.NewTicker(2 * time.Second)
	defer bidTicker.Stop()
	defer flowTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-bidTicker.C:
			p.pollBids(ctx)
		case <-flowTicker.C:
			p.pollFlows(ctx)
		}
	}
}

// pollBids reads every bids-<egress> map, diffs against the previous snapshot,
// and emits a "bid" event for each new or changed entry.
func (p *Poller) pollBids(ctx context.Context) {
	if p.scenario == nil || len(p.scenario.Switches) == 0 {
		return
	}

	for _, sw := range p.scenario.Switches {
		for _, egressPort := range sw.EgressPorts {
			ep := uint64(egressPort)
			current, err := p.store.AllBids(ctx, ep)
			if err != nil {
				log.Printf("poller: bids-%d: %v", ep, err)
				p.atomixHealthy.Store(0)
				continue
			}
			p.atomixHealthy.Store(1)

			prev := p.prevBids[ep]
			for ingressKey, entry := range current {
				prevEntry, existed := prev[ingressKey]
				if !existed || prevEntry.Units != entry.Units || prevEntry.UnitPrice != entry.UnitPrice {
					payload := BidPayload{
						CustomerID:  entry.CustomerID,
						EgressPort:  ep,
						IngressPort: ingressKey,
						Units:       entry.Units,
						UnitPrice:   entry.UnitPrice,
						Timestamp:   time.Now(),
					}

					// GET /flows sequence: api reads atomix, atomix returns,
					// then api responds to customer — each step after the previous dot lands.
					if ev, err := newEvent("atomix_rw",
						"api-gateway",
						"atomix",
						AtomixRWPayload{Op: "read", Map: "throughput-map"},
					); err == nil {
						p.hub.Broadcast(ev)
					}
					cid := entry.CustomerID
					go func() {
						time.Sleep(1300 * time.Millisecond)
						if ev, err := newEvent("atomix_rw",
							"atomix",
							"api-gateway",
							AtomixRWPayload{Op: "read", Map: "throughput-map"},
						); err == nil {
							p.hub.Broadcast(ev)
						}
						time.Sleep(1300 * time.Millisecond)
						if ev, err := newEvent("flow_query",
							"api-gateway",
							"agent:"+cid,
							map[string]string{"customer_id": cid},
						); err == nil {
							p.hub.Broadcast(ev)
						}
					}()

					// POST /bids: customer → api immediately, api → atomix write after dot lands.
					if ev, err := newEvent("bid",
						"agent:"+entry.CustomerID,
						"api-gateway",
						payload,
					); err == nil {
						p.hub.Broadcast(ev)
					}
					ik := ingressKey
					go func() {
						time.Sleep(1300 * time.Millisecond)
						if ev, err := newEvent("atomix_rw",
							"api-gateway",
							"atomix",
							AtomixRWPayload{Op: "write", Map: "bids-" + ik},
						); err == nil {
							p.hub.Broadcast(ev)
						}
					}()
				}
			}
			p.prevBids[ep] = current
		}
	}
}

// pollFlows reads the throughput-map, diffs against the previous snapshot,
// and broadcasts a periodic telemetry snapshot to all clients.
func (p *Poller) pollFlows(ctx context.Context) {
	current, err := p.store.AllFlows(ctx)
	if err != nil {
		log.Printf("poller: flows: %v", err)
		p.atomixHealthy.Store(0)
		return
	}
	p.atomixHealthy.Store(1)

	flowChanged := false
	for k, v := range current {
		if prev, ok := p.prevFlows[k]; !ok || prev != v {
			flowChanged = true
			break
		}
	}
	p.prevFlows = current

	// Always show kafka → telemetry-service dot; the payload also drives the chart.
	if ev, err := newEvent("telemetry", "kafka", "telemetry-service",
		TelemetryPayload{Flows: current},
	); err == nil {
		p.hub.Broadcast(ev)
	}

	// When data changed, show telemetry-service → atomix write after the kafka dot lands.
	if flowChanged {
		go func() {
			time.Sleep(1300 * time.Millisecond)
			if ev, err := newEvent("atomix_rw", "telemetry-service", "atomix",
				AtomixRWPayload{Op: "write", Map: "throughput-map"},
			); err == nil {
				p.hub.Broadcast(ev)
			}
		}()
	}
}
