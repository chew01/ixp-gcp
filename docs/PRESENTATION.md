# Presentation Slides — 25 Minutes

## Slide 1: Title

**Auction-Based Dynamic Bandwidth Allocation at Internet Exchange Points**

- Name, Advisor: Assoc Prof Ma Tianbai
- Keywords: Internet exchange point, bandwidth allocation, second-price auction
- Tech stack: Go 1.25, Kubernetes 1.35, Apache Kafka 4.1

---

## Slide 2: The Problem

**IXP bandwidth allocation is static; traffic demand is not.**

- IXPs are interconnection hubs where autonomous systems (ASes) peer to exchange traffic directly, avoiding transit costs.
- The largest IXPs carry multiple Tbit/s and host hundreds of member networks.
- Yet peering agreements are still negotiated manually at peering forums — "handshake agreements."
- Traffic demand fluctuates on timescales of seconds (diurnal cycles, spikes, flash crowds).
- Policies change on timescales of days to weeks.

**Consequence**: A network with a sudden traffic surge cannot quickly acquire more bandwidth. A network whose demand has dropped holds capacity others could use.

---

## Slide 3: Proposed Solution

**Replace manual renegotiation with a repeated sealed-bid auction.**

- Every N seconds (configurable, e.g. 30s), member networks submit sealed bids:
  - How much bandwidth they want (quantity in kbps)
  - Maximum price per unit they're willing to pay (credits/kbps)
- A uniform-price clearing algorithm allocates capacity and charges all winners the same market-clearing price.
- Allocation adapts to demand changes at sub-minute intervals — no human intervention.

---

## Slide 4: Why a Uniform-Price Auction?

**Truthful bidding is the dominant strategy.**

- Multi-unit analogue of the Vickrey (second-price) auction.
- All winners pay the clearing price, not their own bid.
- Each participant's optimal move: bid their true valuation per unit.
  - No incentive to shade downward (risk losing allocation you'd value positively).
  - No incentive to inflate (risk paying more than your valuation).
- Honest demand revelation without trust between participants or central knowledge of valuations.

> Talk track: "This is the key theoretical property that makes the whole system work. The auction mechanism itself forces participants to reveal their true demand."

---

## Slide 5: Scope & Contributions

**This project implements and evaluates the control plane.**

Three contributions:

1. **Design & implementation** of the auction-based control plane: auction mechanism, event-driven telemetry pipeline, distributed state (Raft), REST API.
2. **Bidding agents** implementing distinct strategies (truthful valuation-based, conservative baseline) to drive and validate the control plane.
3. **Performance evaluation**: API throughput/latency, auction pipeline cycle time, Kafka consumer lag, end-to-end control loop responsiveness.

**Scope boundary**: The switch-level enforcement layer (translating auction results into flow table rules) is handled by a partner project via a shared Kafka topic.

---

## Slide 6: Architecture Overview

**Show Figure 3.1 — Control plane architecture diagram.**

Four components + two infrastructure services:

| Component | Role |
|---|---|
| **API Server** | REST interface for agents (bids, flows, credits, auctions). Enforces customer identity via `X-Customer-ID`. |
| **Telemetry Processor** | Kafka consumer → computes per-flow metrics (throughput, drops) → writes to Atomix. Stateless. |
| **Auction Runner** | Core: reads bids at each interval tick, runs clearing algorithm per egress port, publishes results to Kafka, updates credits. |
| **Atomix (Raft)** | Distributed consistent KV store. 4 maps: throughput, bids, credits, auction history. |

Infrastructure glue:
- **Kafka**: switch-telemetry (in), auction-results (out). Durable, decoupled.
- **Prometheus + Grafana**: Monitoring. Scrapes /metrics every 5s.

> Talk track: "Everything inside the dashed box is my project. The switch and enforcement layer sit outside — we communicate via Kafka topics."

---

## Slide 7: Auction Sequence — One Round

**Show Figure 3.2 — Sequence diagram of the auction stack.**

Walk through one complete auction cycle:

1. Agents query `/flows` for current throughput + drop metrics.
2. Agents compute bid using their strategy.
3. Agents submit bid via `POST /bids` → written to per-egress-port Atomix map.
4. Auction interval fires → Runner collects all bids from Atomix.
5. Insert virtual bid at reservation price (price floor, quantity = full port capacity).
6. Sort bids by price descending, allocate top-down until capacity exhausted.
7. Clearing price = marginal bid's price. All winners pay this.
8. Proportional splitting for tied bids at the margin.
9. Publish results to `auction-results` Kafka topic.
10. Debit credits, update history, clear bid maps.

---

## Slide 8: Key Design Decisions

**Three decisions worth calling out:**

**1. Reservation Price (Virtual Bid)**
- A virtual supply bid at the reservation price with quantity = full port capacity.
- Prevents clearing price from collapsing to zero under low demand.
- Preserves the price signal the mechanism relies on.

**2. Strong Consistency (Raft via Atomix)**
- If the bid map allowed stale reads, the auction runner might miss a bid → wrong clearing price, wrong allocation, wrong credits.
- Raft-based strong consistency: every read reflects all completed writes.

**3. Scenario-Driven Configuration**
- One YAML file defines everything: topology, customers, strategies, auction parameters.
- No hardcoded identities anywhere. Swap the file and redeploy.
- Makefile generates per-customer Kubernetes Deployments from the scenario.

---

## Slide 9: Data Model

**Four Atomix maps:**

| Map | Key | Value |
|---|---|---|
| `throughput-map` | switchID \| ingressPort \| egressPort | FlowMetricsValue (JSON) |
| `bids-{egressPort}` | ingressPort | units \| unitPrice \| customerID |
| `credits-map` | customerID | CustomerCredits (JSON) |
| `auction-history` | intervalID \| egressPort | AuctionHistoryRecord (JSON) |

Key properties:
- Bid maps cleared after each round — no stale carryover. Miss a round → get no allocation.
- One bid per (ingress, egress) pair enforced by key structure — second submission overwrites.
- Credits are accounting-only (soft, not hard-enforced at submission time).

---

## Slide 10: API Design

**Four customer-scoped endpoints:**

| Method | Endpoint | Purpose |
|---|---|---|
| `GET` | `/flows` | Per-flow traffic metrics for the customer's ports |
| `GET` | `/credits` | Credit balance (total spent + starting balance) |
| `GET` | `/auctions` | Auction history — clearing prices and the customer's own allocations only |
| `POST` | `/bids` | Submit a bid (ingress, egress, quantity, unit price) |

- Every request requires `X-Customer-ID` header, validated against scenario's customer list.
- Customer A cannot see Customer B's data — sealed-bid confidentiality preserved.
- `/metrics` endpoint (port 9090) exposes all Atomix state to Prometheus on each scrape.

---

## Slide 11: Bidding Agents

**One Kubernetes pod per customer. Strategy selected from scenario YAML.**

All strategies receive a `BidContext`: flow metrics, credits, last clearing price, valuation per unit.

**Demand estimation** is critical:
- `effective_demand = throughput_kbps + drop_kbps`
- Without drops, you perpetually under-bid under congestion — you only see what got through, not what you tried to send.

**Two key strategies:**

| Strategy | Bid Price | Bid Quantity | Behaviour |
|---|---|---|---|
| `conservative` | max(reservation, last clearing) | throughput × 1.1 | Ignores drops. Under-bids in congestion. Slow recovery. |
| `valuation_based` | valuation per unit | (throughput + drops) × 1.05 | Theoretically optimal. Wins whenever clearing price ≤ valuation. |

> Talk track: "The whole point is that valuation_based — bidding your true value — dominates. That's what auction theory predicts, and it's what the experiments confirm."

---

## Slide 12: Demo Introduction

**"Let me show you the system running."**

Transition to Grafana dashboard / live demo. Key things to show:

- The dashboard panels: per-customer allocation, clearing price, throughput, drops, credits.
- A steady-state period where agents compete and the auction clears normally.
- A traffic spike: one customer's demand surges — watch how allocation shifts.
- How `valuation_based` adapts quickly vs. `conservative` lagging behind.
- The auction re-equilibrating after the spike subsides.

---

## [DEMO — ~5 minutes]

---

## Slide 13: Evaluation — API Performance

**"The API server is not the bottleneck."**

POST /bids throughput and latency:

| Concurrency | Req/s | P50 (ms) | P95 (ms) | P99 (ms) |
|---|---|---|---|---|
| 2 | 56.5 | 30.4 | 68.3 | 92.5 |
| 5 | 155.8 | 28.9 | 53.3 | 74.5 |
| 10 | 266.3 | 33.7 | 64.0 | 97.2 |
| 20 | 370.3 | 50.3 | 88.5 | 120.8 |

GET /flows throughput and latency:

| Concurrency | Req/s | P50 (ms) | P95 (ms) | P99 (ms) |
|---|---|---|---|---|
| 2 | 124.4 | 13.4 | 30.6 | 49.0 |
| 5 | 326.6 | 13.1 | 27.2 | 43.7 |
| 10 | 558.3 | 15.8 | 31.4 | 49.0 |
| 20 | 898.4 | 19.8 | 40.5 | 59.9 |

Key points:
- 100% success rate across all tests. Zero errors.
- GET /flows is 2.2–2.4× faster than POST /bids (reads vs. Raft writes).
- Real demand: ~1 req/s for 10 bidders at 30s intervals → **two orders of magnitude** below measured capacity.
- No latency cliff at c=20 — neither API server nor Atomix is saturated.

---

## Slide 14: Evaluation — Auction Pipeline

**"The clearing algorithm is trivial. Kafka publishing is the bottleneck."**

Clearing algorithm latency (all bidder counts):

| Bidders | Mean | Max |
|---|---|---|
| 2 | < 1 ms | 0 ms |
| 5 | < 1 ms | 0 ms |
| 10 | < 1 ms | 2 ms |

Total pipeline latency (bids-collected → published-to-kafka), 10 bids/round:

| Bidders | Mean (ms) | SD | Min | Max |
|---|---|---|---|---|
| 2 | 9,350 | 926 | 8,007 | 10,079 |
| 5 | 10,065 | 19 | 10,035 | 10,105 |
| 10 | 10,014 | 14 | 10,007 | 10,066 |

Each additional bid adds ~1,000 ms due to sequential single-message Kafka writes with 1s batch timeout.

**This is a known implementation artefact, not a fundamental limit.**
Fix: batch all allocation messages into a single `WriteMessages` call → pipeline drops to ~1s regardless of bid count.

---

## Slide 15: Evaluation — Kafka Consumer Lag

**"The telemetry pipeline runs in real time."**

| Metric | Value |
|---|---|
| Observation window | 8 min 9 s |
| Messages produced | 4,900 |
| Production rate | ~10 msg/s |
| Samples with lag = 0 | 57% |
| Mean lag | 2.4 messages |
| Max lag | 9 messages (~0.9 s) |

- Lag never accumulates — returns to zero within 1–2 sampling intervals.
- Agents always see flow metrics current to within ~1 second.
- Single partition is sufficient at this scale; real IXP deployment would partition by switch/port.

---

## Slide 16: Putting It Together

**"Does the system meet its goals?"**

| Goal | Result |
|---|---|
| Sub-minute auction intervals | Yes — 30s intervals sustained with headroom |
| API not a bottleneck | Yes — 2 orders of magnitude capacity margin |
| Clearing algorithm fast | Yes — < 2 ms for all tested bidder counts |
| Real-time telemetry | Yes — max lag 0.9s |
| Truthful bidding dominates | Yes — `valuation_based` outperforms all alternatives, as theory predicts |

The repeated uniform-price auction control plane allocates IXP bandwidth at sub-minute intervals with acceptable throughput and latency on modest infrastructure (3 nodes, 2 vCPU / 4 GiB each).

---

## Slide 17: Limitations

- **Single-switch, single-egress topology** — multi-switch scaling untested.
- **Synthetic traffic only** — dummy producer, not real IXP traces or BGP data.
- **Soft credit system** — tracks spending but doesn't reject overdraft bids.
- **Sequential Kafka writes** — known artefact consuming ~1/3 of each 30s interval (easy fix).
- **Load patterns** — uniform synthetic load may not capture real temporal correlation (e.g. everyone bidding near the deadline).

---

## Slide 18: Future Work

1. **Multi-switch topology** with 50–100 agents — test horizontal scaling of Atomix and auction runner.
2. **Real traffic replay** from IXP dumps (e.g. CAIDA datasets) — validate under realistic burstiness.
3. **Batch Kafka writes** — single `WriteMessages` call per round, enabling shorter auction intervals.
4. **Hard credit enforcement** — reject bids that would overdraw, closing the gap to a production market mechanism.
5. **BGP route server integration** — extend beyond the switch controller boundary for end-to-end validation.

---

## Slide 19: Conclusion

**Summary:**
- Designed, implemented, and evaluated an auction-based control plane for dynamic IXP bandwidth allocation.
- The uniform-price auction mechanism allocates capacity at sub-minute intervals with sub-second clearing and real-time telemetry.
- API performance provides two orders of magnitude headroom beyond the evaluated bidder count.
- The dominant-strategy prediction from auction theory holds empirically: truthful bidding outperforms all alternatives.

**Thank you. Questions?**

---

## Speaker Notes — Timing Guide

| Slides | Section | Target Duration |
|---|---|---|
| 1 | Title | 0:15 |
| 2–3 | Problem + Solution | 1:45 |
| 4 | Why Uniform-Price Auction | 1:30 |
| 5 | Scope & Contributions | 1:00 |
| 6–7 | Architecture + Sequence | 3:30 |
| 8–9 | Design Decisions + Data Model | 2:00 |
| 10 | API Design | 1:00 |
| 11 | Bidding Agents | 1:30 |
| 12 | Demo Intro | 0:30 |
| — | **DEMO** | **5:00** |
| 13 | API Performance | 1:30 |
| 14 | Pipeline Latency | 1:30 |
| 15 | Kafka Lag | 1:00 |
| 16 | Summary of Results | 0:30 |
| 17–18 | Limitations + Future Work | 1:30 |
| 19 | Conclusion + Q&A | 1:00 |
| | **Total** | **~25:00** |

## General Presentation Tips

- **Slides 6 and 7** (architecture + sequence diagram) are your most important visuals. Spend time on them. Point at specific components as you explain.
- **During the demo**, narrate what the audience should be watching: "Notice the clearing price here..." / "Watch what happens to AS12345's allocation when the spike hits..."
- **Slides 13–15** (evaluation): Don't read every number. Highlight the headline result for each, then point to one or two supporting data points. The tables are there for credibility, not for reading aloud.
- **If you're running short on time**: Compress slides 8–10 (design decisions, data model, API) into a single "the details are in the report" slide and reclaim 2–3 minutes.
- **If you're running long**: The demo is the most flexible segment. You can cut it to 3 minutes by showing just the spike scenario.
