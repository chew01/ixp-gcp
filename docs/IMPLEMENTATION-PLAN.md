# Implementation Plan

This document is the step-by-step plan to implement the agreed features and run the six experiments for the bachelor thesis. It covers:

- **Feedback 1** — Drop-rate aware bidding formula
- **Feedback 2** — Pluggable agent strategies (all six)
- **Feedback 4** — External Kafka cluster support via Makefile variable

Feedback 3 (VLAN/MAC multi-AS per port) and Feedback 5 (scenario hot-reload) are **out of scope** for implementation.

---

## Dependency Graph

```
Phase 0: Kafka Makefile variable          (independent)
Phase 1: Agent strategy interface         (independent, prerequisite for 2 & 3)
Phase 2: Demand-corrected strategy        (requires Phase 1)
Phase 3: Remaining strategies             (requires Phase 1)
Phase 4: Configurable traffic patterns    (independent, prerequisite for experiments)
Phase 5: Experiment observability         (independent, prerequisite for experiments)
Phase 6–11: Experiments                   (requires Phases 1–5)
```

Phases 0, 1, 4, and 5 can be worked on in parallel. Phase 3 can be started alongside Phase 2.

---

## Phase 0: Kafka External Cluster Support (Feedback 4)

**Goal:** Allow switching from in-cluster Strimzi to an external Kafka cluster by passing a single Makefile variable. No code changes — only Makefile and documentation.

**Files:** `Makefile`

### Steps

- [ ] Add `KAFKA_BOOTSTRAP ?= ixp-kafka-kafka-bootstrap:9092` near the top of the Makefile as a user-overridable variable.
- [ ] In the `deploy-dummy`, `deploy-auction`, and `deploy-telemetry` targets, add a `kubectl set env` call that propagates `KAFKA_BOOTSTRAP` into each Deployment:
  ```makefile
  kubectl set env deployment/dummy     KAFKA_BOOTSTRAP=$(KAFKA_BOOTSTRAP)
  kubectl set env deployment/auction   KAFKA_BOOTSTRAP=$(KAFKA_BOOTSTRAP)
  kubectl set env deployment/telemetry KAFKA_BOOTSTRAP=$(KAFKA_BOOTSTRAP)
  ```
- [ ] Wrap the Strimzi install inside `deploy-kafka` with a `KAFKA_EXTERNAL` guard so the in-cluster broker is skipped when an external cluster is provided:
  ```makefile
  deploy-kafka:
  ifndef KAFKA_EXTERNAL
      helm install strimzi-cluster-operator ...
      kubectl apply -f ./kafka/kafka.yaml
      kubectl wait kafka/ixp-kafka --for=condition=Ready --timeout=300s
  else
      @echo "Skipping Strimzi — using external Kafka at $(KAFKA_BOOTSTRAP)"
  endif
  ```
- [ ] Update `infra` to pass `KAFKA_EXTERNAL` through if set.
- [ ] Add a comment block above the variable documenting usage:
  ```makefile
  # To use an external Kafka cluster instead of in-cluster Strimzi:
  #   make infra KAFKA_EXTERNAL=true KAFKA_BOOTSTRAP=192.168.1.50:9092
  #   make deploy-services KAFKA_BOOTSTRAP=192.168.1.50:9092
  ```

**Definition of done:** `make deploy-services KAFKA_BOOTSTRAP=<some-ip>:9092` propagates the address into all three Deployments without touching any Go source files.

---

## Phase 1: Agent Strategy Interface

**Goal:** Refactor `agent/main.go` so that the bidding logic is isolated behind a `BiddingStrategy` interface. The existing heuristic becomes the `ConservativeStrategy`. All future strategies implement the same interface.

**Files:** `agent/main.go`, `agent/main_test.go`; new package `agent/strategy/` with files `strategy.go`, `conservative.go`, `conservative_test.go`

### Directory layout

```
agent/
├── main.go
├── main_test.go
└── strategy/
    ├── strategy.go          ← BidContext + Bidder interface
    ├── conservative.go      ← Conservative struct
    ├── conservative_test.go
    ├── demand_corrected.go  ← (Phase 2)
    ├── price_insensitive.go ← (Phase 3)
    ├── backoff.go           ← (Phase 3)
    ├── budget_aware.go      ← (Phase 3)
    └── exploratory.go       ← (Phase 3)
```

### 1.1 Define the interface and context

Create `agent/strategy/strategy.go` in package `strategy`:

```go
package strategy

import (
    "github.com/chew01/ixp-gcp/shared"
    "github.com/chew01/ixp-gcp/shared/scenario"
)

// BidContext bundles all information a strategy needs for one (ingress, egress) pair.
type BidContext struct {
    Scene             *scenario.Scenario
    CustomerID        string
    SwitchID          string
    IngressPort       uint32
    EgressPort        uint32
    Metrics           shared.FlowMetricsValue
    Credits           shared.CustomerCredits
    LastClearingPrice int
}

// Bidder computes the bid quantity and price for a single flow.
// Returning skip=true means no bid should be submitted for this flow this round.
type Bidder interface {
    ComputeBid(ctx BidContext) (units uint64, price uint64, skip bool)
}
```

### 1.2 Extract the conservative strategy

Create `agent/strategy/conservative.go` containing a `Conservative` struct whose `ComputeBid` method holds the current logic from `placeBidForFlow`:
- `units = max(1, throughput_kbps * 1.1)`
- `price = max(reservation_price, last_clearing_price)`
- `skip = true` when throughput ≤ 0 and drop ≤ 0

### 1.3 Refactor main.go

- [x] Add `AGENT_STRATEGY` to the `config` struct; load from env (default `"conservative"`).
- [x] Add a `selectStrategy(name string) strategy.Bidder` function that returns the correct implementation. Return an error (or log.Fatal) for unknown names.
- [x] In `placeBidForFlow`, assemble a `strategy.BidContext` and call `strat.ComputeBid(ctx)` instead of computing units/price inline.
- [x] Pass the selected strategy down through `runOnce` → `placeBidForFlow`.

### 1.4 Tests

- [x] Add `agent/strategy/conservative_test.go` (package `strategy_test`) with table-driven cases covering: zero traffic (skip), positive traffic, traffic with drops (should still use `throughput * 1.1` at this stage — the corrected formula comes in Phase 2).
- [x] Keep `TestSelectStrategy` and `TestDeriveCustomerPorts` in `agent/main_test.go`.

**Definition of done:** `AGENT_STRATEGY=conservative` produces identical behaviour to the current agent. `AGENT_STRATEGY=unknown` exits with a clear error. Tests pass.

---

## Phase 2: Demand-Corrected Strategy (Feedback 1)

**Goal:** Implement a strategy that bids for the full demand (`throughput + drop`) instead of just 10% above current egress throughput.

**Files:** new `agent/strategy/demand_corrected.go`, `agent/strategy/demand_corrected_test.go`

### Steps

- [ ] Create `DemandCorrected` struct (no internal state needed) in package `strategy`.
- [ ] Implement `ComputeBid`:
  ```
  effective_demand = metrics.ThroughputKbps + metrics.DropKbps
  units = max(1, effective_demand * 1.05)
  price = max(reservation_price, last_clearing_price)
  skip = (effective_demand <= 0)
  ```
  The 1.05 margin absorbs measurement noise without overbidding as severely as adding a flat 10% to an already-insufficient value.
- [ ] Register `"demand_corrected"` in `selectStrategy`.
- [ ] Add unit tests in `demand_corrected_test.go`: zero drops (degrades gracefully), moderate drop rate, extreme drop rate (>50%).

**Definition of done:** `AGENT_STRATEGY=demand_corrected` deploys and observes drops; after 1–2 auction rounds the drop rate should fall toward zero in a controlled test.

---

## Phase 3: Remaining Strategies (Feedback 2)

Each strategy is a separate file. All implement `BiddingStrategy`.

### 3.1 Price-Insensitive Strategy

**File:** `agent/strategy/price_insensitive.go`, `agent/strategy/price_insensitive_test.go`

Models an AS that values guaranteed bandwidth above cost (e.g. latency-critical traffic).

```
units = max(1, (throughput + drop) * 1.05)   // demand-aware quantity
price = reservation_price * PRICE_MULTIPLIER  // fixed high price, e.g. 10x
skip  = (throughput <= 0 && drop <= 0)
```

- [ ] `PRICE_MULTIPLIER` configurable via env `AGENT_PRICE_MULTIPLIER` (default 10).
- [ ] Struct name: `PriceInsensitive`.
- [ ] Register `"price_insensitive"`.
- [ ] Unit tests: verify price is always at least `reservation_price * multiplier` regardless of clearing history.

---

### 3.2 Backoff Strategy

**File:** `agent/strategy/backoff.go`, `agent/strategy/backoff_test.go`

Models a budget-constrained AS that deliberately cools the market after repeated expensive rounds.

Internal state:
```go
type Backoff struct {
    consecutiveExpensive int
    backoffThreshold     int     // number of expensive rounds before backing off
    expensivePrice       int     // clearing price above which a round is "expensive"
    currentMultiplier    float64 // starts at 1.0
}
```

Logic:
```
if last_clearing_price > expensive_price:
    consecutiveExpensive++
else:
    consecutiveExpensive = 0
    currentMultiplier = 1.0

if consecutiveExpensive >= backoffThreshold:
    currentMultiplier = max(0.5, currentMultiplier * 0.5)

price = max(reservation_price, last_clearing_price * currentMultiplier)
units = max(1, (throughput + drop) * 1.05)
```

- [ ] Configurable via env: `AGENT_BACKOFF_THRESHOLD` (default 3), `AGENT_EXPENSIVE_PRICE` (default 2 × reservation_price).
- [ ] Struct name: `Backoff`.
- [ ] Register `"backoff"`.
- [ ] Unit tests: verify multiplier halves after threshold consecutive expensive rounds, resets after a cheap round.

---

### 3.3 Budget-Aware Strategy

**File:** `agent/strategy/budget_aware.go`, `agent/strategy/budget_aware_test.go`

Scales bid price down as remaining credits deplete.

```
remaining = starting_balance - total_spent
fraction  = remaining / starting_balance

if fraction > 0.75:  price = max(reservation_price, last_clearing_price)
if fraction > 0.50:  price = max(reservation_price, last_clearing_price * 0.75)
if fraction > 0.25:  price = max(reservation_price, last_clearing_price * 0.50)
else:                price = reservation_price   // minimum viable bid
```

- [ ] If `starting_balance` is zero (not configured), fall back to conservative behaviour.
- [ ] Struct name: `BudgetAware`.
- [ ] Register `"budget_aware"`.
- [ ] Unit tests: price tiers at each threshold; fallback when balance is zero.

---

### 3.4 Exploratory / Learning Strategy

**File:** `agent/strategy/exploratory.go`, `agent/strategy/exploratory_test.go`

Maintains an exponential moving average (EMA) of observed clearing prices and bids at `EMA + epsilon`.

Internal state:
```go
type Exploratory struct {
    ema         float64
    initialized bool
    alpha       float64  // smoothing factor, e.g. 0.3
    epsilon     int      // fixed margin above EMA, e.g. 5
}
```

Logic per round:
```
if !initialized:
    ema = float64(last_clearing_price)
    initialized = true
else if last_clearing_price > 0:
    ema = alpha * float64(last_clearing_price) + (1 - alpha) * ema

price = max(reservation_price, int(ema) + epsilon)
units = max(1, (throughput + drop) * 1.05)
```

- [ ] Configurable via env: `AGENT_EMA_ALPHA` (default 0.3), `AGENT_EMA_EPSILON` (default 5).
- [ ] Struct name: `Exploratory`.
- [ ] Register `"exploratory"`.
- [ ] Unit tests: EMA converges to stable price, first round uses raw clearing price as seed.

---

## Phase 4: Configurable Traffic Patterns in the Dummy Switch

**Goal:** Make the dummy producer emit controllable, repeatable traffic patterns so experiments have known inputs rather than random noise.

**Files:** `dummy/producer.go`, `dummy/main.go`

### 4.1 Define traffic pattern types

Add a `TrafficPattern` type with three modes selectable via `TRAFFIC_PATTERN` env var:

| Pattern | Description |
|---------|-------------|
| `random` | Current behaviour — random bytes per interval (default, preserves backward compat) |
| `steady` | Each flow emits a fixed rate in kbps (`TRAFFIC_RATE_KBPS`, default 10) |
| `spike` | Starts at `TRAFFIC_RATE_KBPS`, then at auction interval N (`TRAFFIC_SPIKE_AFTER_INTERVALS`, default 5) jumps to `TRAFFIC_SPIKE_RATE_KBPS` (default 18) |

### 4.2 Implement steady and spike patterns

- [ ] In `dummy/producer.go`, add a `patternConfig` struct loaded from env.
- [ ] For `steady`: compute `rxIncrease = (targetKbps * 1000 / 8) * intervalSeconds` (converts kbps to bytes per interval). `txIncrease = rxIncrease` (no drops by default; the switch enforces drops based on allocation).
- [ ] For `spike`: track an `intervalCount` counter; after `SpikeAfterIntervals` ticks, switch the per-flow rate to `SpikeRateKbps`.
- [ ] `random` mode: keep the current `RandRange(10_000, 200_000)` logic unchanged.

### 4.3 Deployment

- [ ] Add `TRAFFIC_PATTERN`, `TRAFFIC_RATE_KBPS`, `TRAFFIC_SPIKE_AFTER_INTERVALS`, `TRAFFIC_SPIKE_RATE_KBPS` to the dummy Deployment manifest and the `deploy-dummy` Makefile target.

**Definition of done:** `TRAFFIC_PATTERN=steady TRAFFIC_RATE_KBPS=9` results in each flow emitting ~9 kbps (9 kbps × 10 flows = 90 kbps, just under the 100 kbps egress capacity).

---

## Phase 5: Experiment Observability

**Goal:** Expose clearing price and per-customer allocation as Prometheus metrics so Grafana captures them automatically during experiments, producing time-series data for the report.

**Files:** `api/server.go` (or wherever `refreshMetrics` lives), `monitoring/ixp-flows.json`

### 5.1 New Prometheus metrics

Add two new gauge families to the API gateway's metrics refresh:

| Metric | Labels | Source |
|--------|--------|--------|
| `ixp_auction_clearing_price` | `egress_port` | Latest record from `auction-history` Atomix map |
| `ixp_customer_allocation_kbps` | `customer_id`, `egress_port` | Per-customer allocations from latest `auction-history` record |

- [ ] In `refreshMetrics` (called on the Prometheus scrape), read the latest `AuctionHistoryRecord` for each egress port from `auction-history` and set both gauges.
- [ ] Register the two gauge families at startup alongside the existing flow metrics.

### 5.2 Grafana dashboard updates

- [ ] Add a **Clearing Price over Time** panel: `ixp_auction_clearing_price{egress_port="0"}`.
- [ ] Add a **Allocation per Customer** panel: `ixp_customer_allocation_kbps` grouped by `customer_id`.
- [ ] Ensure all four flow metrics panels (throughput, egress, drop, drop_rate) have per-port filtering.

### 5.3 Data export for the report

- [ ] Document the Prometheus HTTP API query for exporting a metric as a time-range snapshot:
  ```
  GET /api/v1/query_range?query=<metric>&start=<ts>&end=<ts>&step=30s
  ```
  This produces JSON that can be saved and plotted with any tool (Python/matplotlib, Excel, etc.).
- [ ] Add a `make export-metrics` target that `curl`-fetches the key metrics for the last hour and saves them to `data/experiment-<timestamp>.json`. (Only the curl call — no post-processing script required.)

**Definition of done:** After an auction round, Grafana shows the clearing price gauge updating. `make export-metrics` saves a JSON file.

---

## Phase 6: Experiment 1 — Baseline Agent Correctness

**Goal:** Characterise the conservative agent under steady load at near-capacity. Establishes the baseline for all subsequent comparisons.

**Prerequisites:** Phases 1–5 complete.

### Setup

| Parameter | Value |
|-----------|-------|
| `TRAFFIC_PATTERN` | `steady` |
| `TRAFFIC_RATE_KBPS` | `9` (9 kbps × 10 flows = 90 kbps, 90% of capacity) |
| `AGENT_STRATEGY` (both agents) | `conservative` |
| Duration | 20 auction intervals (10 minutes at 30s interval) |

### Steps

- [ ] Deploy with the above configuration.
- [ ] Let the system run for 20 intervals.
- [ ] Export metrics: `make export-metrics`.
- [ ] Record:
  - Drop rate per flow over time (expected: falls to ~0 after the first 1–2 intervals once allocation ≥ demand).
  - Clearing price per interval (expected: stabilises near `reservation_price`; no competition since demand < capacity).
  - Credits spent per customer (expected: symmetric, predictable).

### Hypothesis

The conservative agent's 10% headroom is sufficient to eliminate drops at 90% load because demand (9 kbps/flow) × 1.1 = 9.9 kbps, which is within the 10 kbps fair share.

---

## Phase 7: Experiment 2 — Drop-Rate Algorithm vs. Fixed-Margin

**Goal:** Quantify how much faster the demand-corrected agent recovers from congestion caused by a traffic spike.

**Prerequisites:** Phases 1–5 complete, Phase 2 complete.

### Setup

Run the experiment **twice** — once per algorithm — with identical traffic:

| Parameter | Value |
|-----------|-------|
| `TRAFFIC_PATTERN` | `spike` |
| `TRAFFIC_RATE_KBPS` | `8` (baseline; 8 × 10 = 80 kbps) |
| `TRAFFIC_SPIKE_AFTER_INTERVALS` | `5` |
| `TRAFFIC_SPIKE_RATE_KBPS` | `13` (spike; 13 × 10 = 130 kbps, exceeds capacity) |
| Duration | 20 intervals total |

Run A: `AGENT_STRATEGY=conservative` (both agents)
Run B: `AGENT_STRATEGY=demand_corrected` (both agents)

### Steps

- [ ] Run A, export metrics, reset.
- [ ] Run B, export metrics.
- [ ] Compare:
  - **Recovery time**: number of intervals from spike (interval 5) until drop_rate returns to < 5%.
  - **Peak drop rate**: worst single-interval drop rate during the spike.
  - **Total credits spent**: cumulative spend to achieve recovery.

### Hypothesis

In Run A, the conservative agent bids `egress_kbps * 1.1` during the spike, which is insufficient since egress is already throttled (drops are high, so egress_kbps is low). Recovery takes 3–5 intervals.

In Run B, the demand-corrected agent bids `(throughput + drop) * 1.05 ≈ ingress_kbps * 1.05`, which approximates the actual demand. Recovery takes 1–2 intervals.

---

## Phase 8: Experiment 3 — Heterogeneous Strategies: Market Dynamics

**Goal:** Observe how a price-insensitive agent affects the clearing price and the allocation for a competing conservative agent.

**Prerequisites:** Phases 1–5 complete, Phase 3.1 complete.

### Setup

| Agent | Strategy | Customer |
|-------|----------|----------|
| `as12345` | `conservative` | ports 1–5 |
| `as67890` | `price_insensitive` | ports 6–10 |

| Parameter | Value |
|-----------|-------|
| `TRAFFIC_PATTERN` | `steady` |
| `TRAFFIC_RATE_KBPS` | `9` |
| `AGENT_PRICE_MULTIPLIER` (as67890) | `10` |
| Duration | 20 intervals |

### Steps

- [ ] Deploy with the above configuration.
- [ ] Export metrics.
- [ ] Compare vs. Experiment 1 baseline:
  - **Clearing price**: expected to rise above `reservation_price` since `as67890` always bids `10 × reservation_price`.
  - **Allocation split**: does the uniform-price mechanism protect `as12345` (it wins at the same clearing price without needing to bid high), or does `as67890`'s high bid crowd it out via the proportional marginal allocation?
  - **Credits spent per customer**: `as12345` should spend more per unit than in the baseline (clearing price is higher) despite bidding conservatively.

### Hypothesis

Clearing price rises to near `as67890`'s bid price when demand > capacity. Under uniform pricing, `as12345` wins at the same (elevated) clearing price as `as67890`, paying more than it would in the all-conservative baseline. This demonstrates the externality a price-insensitive bidder imposes on competitors.

---

## Phase 9: Experiment 4 — Budget Awareness and Credit Exhaustion

**Goal:** Evaluate whether a budget-aware agent sustains throughput longer than an unconstrained agent given the same finite starting balance.

**Prerequisites:** Phases 1–5 complete, Phase 3.3 complete.

### Setup

Add a `starting_balance` field to the scenario for each customer (requires a small extension to the scenario schema and the credits initialisation in the API — see `shared/scenario/types.go` and `api/`).

| Parameter | Value |
|-----------|-------|
| `TRAFFIC_PATTERN` | `steady` |
| `TRAFFIC_RATE_KBPS` | `11` (11 × 10 = 110 kbps — slightly over capacity, so competition is present) |
| `starting_balance` (both) | A finite value, e.g. `5000` credits |
| Duration | Until effective exhaustion or 40 intervals, whichever comes first |

Run A: `AGENT_STRATEGY=conservative` (both agents) — unconstrained bidding
Run B: `AGENT_STRATEGY=budget_aware` (both agents)

### Steps

- [ ] Extend scenario schema and credits initialisation to support `starting_balance` (small change: add field to `Customer` struct and seed the credits map at startup).
- [ ] Run A and Run B, exporting metrics.
- [ ] Compare:
  - **Interval of effective exhaustion**: when `credits_remaining < one_round_cost`.
  - **Cumulative allocation received** up to exhaustion: does the budget-aware agent receive comparable total allocation while spending more slowly?
  - **Drop rate after exhaustion**: the unconstrained agent bids at min price (reservation_price) once exhausted; does drop rate increase?

### Hypothesis

The budget-aware agent depletes credits more slowly by reducing bid price as balance falls. It reaches effective exhaustion later while receiving similar total allocation. The unconstrained agent wins larger allocations early but its throughput degrades sooner.

---

## Phase 10: Experiment 5 — Auction Convergence and Stability

**Goal:** Characterise whether and how quickly the clearing price converges to a stable value, and measure price variance in the steady state.

**Prerequisites:** Phases 1–5 complete (only conservative strategy needed).

### Setup

| Parameter | Value |
|-----------|-------|
| `TRAFFIC_PATTERN` | `steady` |
| `TRAFFIC_RATE_KBPS` | `11` (over capacity: 110 kbps total; clears at a competitive price) |
| `AGENT_STRATEGY` (both) | `conservative` |
| Duration | 30 intervals |

### Steps

- [ ] Deploy and run for 30 intervals.
- [ ] Export `ixp_auction_clearing_price` time series.
- [ ] Compute:
  - **Convergence point**: first interval N after which the price stays within ±10% of its mean for the remaining intervals.
  - **Steady-state variance**: std deviation of price in intervals N..30.
  - **Speed of convergence**: number of intervals to reach steady state.

### Hypothesis

With two symmetric conservative agents both tracking `last_clearing_price`, the clearing price converges to the competitive equilibrium (where `total_bid_units ≈ capacity`) within 3–5 intervals. Steady-state variance is low because both agents shadow the same price signal.

---

## Phase 11: Experiment 6 — Sensitivity to Auction Interval

**Goal:** Measure how the auction interval length affects drop duration during a demand spike and total credits spent over a fixed wall-clock window.

**Prerequisites:** Phases 1–5 complete.

### Setup

Run the experiment three times, changing only `auction_interval` in `scenario.yaml` between runs (redeploy after each change):

| Run | `auction_interval` | Note |
|-----|--------------------|------|
| A | `10s` | Fast |
| B | `30s` | Default |
| C | `60s` | Slow |

| Parameter | Value |
|-----------|-------|
| `TRAFFIC_PATTERN` | `spike` |
| `TRAFFIC_RATE_KBPS` | `8` |
| `TRAFFIC_SPIKE_AFTER_INTERVALS` | `5` |
| `TRAFFIC_SPIKE_RATE_KBPS` | `13` |
| `AGENT_STRATEGY` (both) | `demand_corrected` |
| Wall-clock duration | 10 minutes per run |

### Steps

- [ ] Update `scenario.yaml`, run `make deploy-services`, wait for rollout, run for 10 minutes, export metrics.
- [ ] Repeat for each interval.
- [ ] Compare (normalised to the same 10-minute window):
  - **Drop duration**: total wall-clock seconds spent above 5% drop rate after the spike.
  - **Credits spent**: total across both customers.
  - **Number of auction rounds**: how many clearing events occur.

### Hypothesis

A shorter interval reduces the wall-clock drop duration after the spike (the system corrects faster) but increases total credits spent (more auction rounds). A longer interval reduces spend but means the system takes longer to react. This quantifies the latency-cost trade-off of the auction cadence — a central design decision distinguishing this system from traditional IXP timescales.

---

## Summary Checklist

| Phase | Description | Depends On |
|-------|-------------|------------|
| 0 | Kafka Makefile variable | — |
| 1 | Agent strategy interface + conservative refactor | — |
| 2 | Demand-corrected strategy | 1 |
| 3 | Remaining strategies (price-insensitive, backoff, budget-aware, exploratory) | 1 |
| 4 | Configurable traffic patterns in dummy switch | — |
| 5 | Experiment observability (Prometheus + export) | — |
| 6 | Experiment 1: Baseline | 1, 4, 5 |
| 7 | Experiment 2: Drop-rate algorithm comparison | 2, 4, 5 |
| 8 | Experiment 3: Heterogeneous strategies | 3 (price-insensitive), 4, 5 |
| 9 | Experiment 4: Budget awareness | 3 (budget-aware), 4, 5 + scenario balance field |
| 10 | Experiment 5: Convergence and stability | 1, 4, 5 |
| 11 | Experiment 6: Auction interval sensitivity | 2, 4, 5 |
