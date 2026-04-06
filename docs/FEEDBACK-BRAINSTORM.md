# Brainstorm: Feedback, Feasibility, Design Decisions, and Experiments

This document analyses four feedback points from the project supervisor, assesses their feasibility against the existing codebase, identifies the design decisions each introduces, and proposes concrete experiments for the bachelor thesis report.

---

## Context Recap

The system implements a **rapid-bandwidth auction at an IXP**. Instead of the traditional multi-week contract negotiation, customer agents (one per AS) bid every 30 seconds in a uniform-price auction for guaranteed egress bandwidth. The core components are:

- **Agent** — per-customer bidding bot (`agent/`)
- **API Gateway** — bid submission, flow metrics, credits, auction history (`api/`)
- **Auction Runner** — periodic clearing algorithm (`auction/`)
- **Telemetry Service** — Kafka → Atomix, computing throughput/drop metrics (`telemetry/`)
- **Dummy Switch** — traffic simulator for development (`dummy/`)
- **Shared Scenario** — topology, customers, capacities (`etc/scenario/scenario.yaml`)

---

## 1. Agent Algorithm: Drop-Rate Aware Bidding

### Problem Statement

The current heuristic in `agent/agent.go` computes the bid quantity as:

```
units = max(1, throughput_kbps * 1.1)
price = max(reservation_price, last_clearing_price)
```

This 10% headroom is **insufficient** when there is a significant drop rate. Consider: an ingress port receives 80 kbps, 50 kbps reaches egress, and 30 kbps is dropped (drop rate ~37.5%). The agent bids only 88 kbps. However, the true demand — what the AS actually wants to forward — is 80 kbps (the ingress rate), not 50 kbps. The correct minimum allocation to eliminate drops is the full ingress rate.

The relationship in the telemetry model is:

```
ingress_kbps = egress_kbps + drop_kbps
```

So `throughput_kbps` (which records `IngressKbps`) already encodes the demand, but `drop_kbps` reveals the gap between demand and current allocation.

### Proposed Fix

Replace the fixed 10% headroom with a drop-aware formula:

```
effective_demand = throughput_kbps + drop_kbps
units = effective_demand * (1 + margin)
```

where `margin` is a small constant (e.g. 0.05) to absorb measurement noise. When drop_kbps is zero, this degrades gracefully to the current behaviour. When drops are high, the bid immediately targets the actual demand rather than the already-inadequate egress rate.

An extension is to make `margin` **adaptive**: if drop_rate_pct exceeds a threshold, increase `margin` to bid more aggressively until drops fall back to near zero.

### Design Decisions to Explain

| Decision | Options | Trade-off |
|----------|---------|-----------|
| Bid quantity formula | Fixed margin vs. drop-aware vs. adaptive margin | Simplicity vs. accuracy vs. responsiveness |
| Reaction speed | Bid correction in one round vs. gradual ramp-up | Risk of overbidding vs. slow recovery from congestion |
| When to skip bidding | Skip if throughput=0 AND drop=0 (current) vs. always bid a minimum | Avoids wasted credits vs. avoids losing position in the market |
| Measurement lag | Telemetry is 1s interval, auction is 30s interval | Stale metrics may under/overestimate demand |

### Feasibility

High. Only `agent/agent.go` needs changing — the `drop_kbps` field is already computed by the telemetry service and exposed via `GET /flows`. No schema or infrastructure changes required.

---

## 2. Multiple Agent Strategies

### Motivation

Comparing the behaviour of heterogeneous agents is a classic mechanism-design question: does the auction produce efficient outcomes regardless of agent strategy, or is it sensitive to agent mix? For the thesis, different agent types are experiment variables.

### Proposed Agent Strategies

#### A. Conservative (current baseline)
- Bids `throughput * 1.1` at `max(reservation_price, last_clearing_price)`.
- Represents a risk-averse AS that tracks the market closely.

#### B. Demand-Corrected (from Feedback 1)
- Bids `(throughput + drop) * 1.05` at market clearing price.
- Reduces persistent drops faster than the baseline.

#### C. Price-Insensitive (high-value AS)
- Bids a fixed high price (e.g. `10 × reservation_price`) regardless of market history.
- Models an AS that values guaranteed bandwidth above cost (e.g. latency-sensitive traffic).
- Expected behaviour: always wins at clearing price but overpays when competition is low.

#### D. Backoff Agent
- After consecutive intervals where the clearing price exceeds a budget threshold, halves its bid price. Resets to `last_clearing_price + delta` after a losing interval.
- Models budget-constrained behaviour or deliberate market cooling.

#### E. Budget-Aware Agent
- Tracks `credits_remaining = starting_balance - total_spent`.
- Scales bid price downward as remaining balance falls below defined thresholds (e.g. 75%, 50%, 25%).
- At credit exhaustion, submits only minimum bids.

#### F. Exploratory / Learning Agent (Advanced)
- Maintains a sliding window of clearing prices.
- Uses exponential moving average to estimate the minimum winning price.
- Bids at `EMA + epsilon` rather than the raw last clearing price.
- Models a smarter agent that learns the market over time.

### Implementation Approach

The cleanest approach is to make the bidding strategy a **pluggable interface** in `agent/`:

```go
type BiddingStrategy interface {
    ComputeBid(ctx BidContext) (units float64, price float64)
}
```

where `BidContext` bundles the telemetry metrics, credits, and auction history already fetched each cycle. The strategy is then selected via an environment variable (e.g. `AGENT_STRATEGY=conservative|demand_corrected|price_insensitive|backoff|budget_aware`). This avoids building separate binaries per strategy and makes it easy to run mixed-strategy experiments by deploying two agents with different env vars.

### Design Decisions to Explain

| Decision | Options | Trade-off |
|----------|---------|-----------|
| Strategy encapsulation | Single binary with env-var selection vs. multiple binaries | Operational simplicity vs. compile-time isolation |
| How many strategies to implement | 2–3 core strategies vs. full set | Thesis scope vs. experiment coverage |
| Metrics for evaluating strategies | Credit spend, allocated units, drop rate, allocation efficiency | Need to define clear evaluation criteria |
| Whether agents can observe each other | Current: no (only own flows and clearing price) vs. shared telemetry | Privacy vs. richer market information |

### Feasibility

Medium-high. Requires refactoring the agent's main loop to delegate computation to a strategy struct, but the data fetching layer remains unchanged. Unit-testable strategies are a thesis-friendly design.

---

## 3. Multiple AS per Ingress Port (VLAN/MAC-based)

### Problem Statement

Currently, the scenario enforces a strict one-to-one mapping between ingress ports and customer AS IDs. In real IXPs, multiple AS peering sessions may terminate on the same physical port, differentiated by **VLAN ID** and **source MAC address**. The professor flags this is not fully fleshed out yet.

### Implications

Adopting per-VLAN flow tracking has cascading effects across the entire system:

1. **Telemetry** — the switch must emit per-VLAN counters, not just per-port counters. The `TelemetryRecord` in `shared/` would need a `VLAN ID` field. The flow key in Atomix (`switchID|ingressPort|egressPort`) would become `switchID|ingressPort|egressPort|vlanID`.

2. **Bid system** — a single ingress port could have bids from multiple customers (one per VLAN). The bid key in Atomix (`bids-<egressPort>`: key = ingress port) would need to change to `ingressPort|vlanID`.

3. **Scenario config** — customers would be assigned `(ingress_port, vlan_id)` tuples rather than just ports. The scenario validation logic in `shared/scenario/load.go` would need updating.

4. **Auction runner** — with multiple customers on one port, the proportional allocation at the margin is already per-bid, so the algorithm itself may need minimal changes. However, the switch programming output (the `AuctionResultRecord`) would need to carry VLAN-keyed allocations so the switch can enforce per-VLAN shaping.

5. **MAC address disambiguation** — MAC addresses are relevant at L2 for identifying which AS a frame belongs to at ingress, before VLAN tagging is applied. This is a switch-level concern rather than a control-plane concern, but it means the dummy switch simulator would need to be extended.

### Recommendation for Thesis Scope

This change is architecturally significant and not fully specified. The recommended approach is to treat it as **future work** with a short design section in the report, while keeping the current one-AS-per-port model as the experimental baseline. The design section should clearly articulate:

- What changes each component would require
- Why VLAN IDs are necessary (not just ports) in a multi-tenant IXP
- The trade-off between implementation complexity and realism

### Design Decisions to Document (even without implementation)

| Decision | Options |
|----------|---------|
| VLAN granularity in telemetry | Per-port (current) vs. per-VLAN |
| Flow key format | `switch|ingress|egress` vs. `switch|ingress|egress|vlan` |
| Scenario configuration | Port-to-customer vs. (port, vlan)-to-customer |
| Backward compatibility | Breaking change vs. optional VLAN field (zero = no VLAN) |

### Feasibility

Low for full implementation in thesis scope. High for a design writeup and partial prototype (e.g. extending the scenario and telemetry schema without changing the switch simulator).

---

## 4. Kafka Configuration: External Cluster Support

### Problem Statement

Kafka is deployed in-cluster via Strimzi. All services resolve it via the in-cluster DNS name `ixp-kafka-kafka-bootstrap:9092`, which is the default value of the `KAFKA_BOOTSTRAP` environment variable present in every service. Switching to an external Kafka cluster (e.g. a production cluster with a known IP or hostname) requires a configuration mechanism rather than a code change.

### Good News: Already Partially Supported

The `KAFKA_BOOTSTRAP` environment variable is already the single point of configuration for all Kafka clients (used by `auction/`, `telemetry/`, `dummy/`). The only change needed is:

1. A way to **inject a non-default `KAFKA_BOOTSTRAP`** into all service deployments from a single place.
2. A way to **disable the in-cluster Strimzi deployment** when an external cluster is used.

### Proposed Implementation

Switching Kafka clusters is a rare, deliberate operational event — not a frequent runtime change. Hot-reloading a new Kafka bootstrap address is not worth the complexity, because reconnecting `kafka-go` readers and writers cleanly (draining in-flight messages, preserving offsets) is non-trivial and a rolling restart achieves the same result safely with one command. Therefore Option B (Makefile variable) is the right approach.

**Option B — Makefile variable (chosen):**

Pass `KAFKA_BOOTSTRAP` as a Makefile variable at deploy time. All service Deployments already read `KAFKA_BOOTSTRAP` from their environment, so `kubectl set env` propagates it immediately and triggers a rolling restart:

```makefile
KAFKA_BOOTSTRAP ?= ixp-kafka-kafka-bootstrap:9092

deploy-services:
    kubectl set env deployment/auction   KAFKA_BOOTSTRAP=$(KAFKA_BOOTSTRAP)
    kubectl set env deployment/telemetry KAFKA_BOOTSTRAP=$(KAFKA_BOOTSTRAP)
    kubectl set env deployment/dummy     KAFKA_BOOTSTRAP=$(KAFKA_BOOTSTRAP)
```

To switch to an external cluster:

```bash
make deploy-services KAFKA_BOOTSTRAP=192.168.1.50:9092
```

Strimzi can be skipped for external-cluster setups with a conditional Makefile flag:

```makefile
infra:
ifndef KAFKA_EXTERNAL
    kubectl apply -f kafka/kafka.yaml
endif
```

The rolling restart is the intentional behaviour: Kafka connections are long-lived and the cleanest reconnection is at process startup. One command is sufficient.

### Design Decisions to Explain

| Decision | Options | Trade-off |
|----------|---------|-----------|
| Config source for Kafka address | Makefile variable (chosen) vs. ConfigMap vs. Helm values | Simplicity vs. Kubernetes-idiomatic style |
| Hot reload vs. rolling restart | Rolling restart (chosen) vs. fsnotify + reconnect | Safety and simplicity vs. zero-downtime reconfiguration |
| Strimzi vs. external cluster | In-cluster Strimzi (dev) vs. external cluster (production) | Reproducibility vs. integration realism |
| Topic creation | Strimzi `KafkaTopic` CRDs vs. manual topic creation on external cluster | Declarative vs. procedural |
| TLS/authentication | Currently plain (port 9092) vs. TLS + SASL on external cluster | Simplicity vs. security |

### Feasibility

High. The environment variable mechanism already exists in every service. The main work is:
- Propagating `KAFKA_BOOTSTRAP` via `kubectl set env` in the Makefile deploy targets.
- Adding the `KAFKA_EXTERNAL` guard to skip Strimzi.
- Documenting the switch procedure.

---

## 5. Scenario ConfigMap Hot-Reload

### Problem Statement

The `scenario.yaml` file is the single source of truth for topology, customer ownership, egress capacity, auction interval, and timing parameters. Currently every service reads it once at startup (via `shared/scenario/load.go`). Changing any scenario parameter — for example the auction interval, the egress capacity, or adding a new customer — requires restarting all affected pods.

For **interactive experimentation**, this is a friction point. Running Experiment 6 (auction interval sensitivity) means manually restarting all services three times and waiting for Kubernetes rolling updates between each run. Hot-reloading the scenario would let the operator patch the ConfigMap and have services adapt immediately, making the experiment loop faster and less error-prone.

Unlike the Kafka bootstrap address, hot-reloading the scenario is feasible because:

- **No external connections need to be re-established.** The scenario is pure configuration — numbers and strings used in local logic.
- The scenario is already mounted from a Kubernetes ConfigMap (`scenario-config`) into each pod at `/etc/scenario/scenario.yaml`. When a ConfigMap volume is updated, Kubernetes automatically refreshes the mounted file within ~1–2 minutes (kubelet sync period). The process only needs to re-read and re-apply it.

### What Each Service Would Do on Reload

| Service | Parameters that may change | Reload action |
|---------|---------------------------|---------------|
| **Auction runner** | `auction_interval`, `max_capacity`, `reservation_price` | Reset the interval ticker; update capacity passed to clearing algorithm |
| **Agent** | `auction_interval`, owned ingress ports | Reset the poll ticker; re-derive owned ports for the agent's customer ID |
| **API gateway** | Customer → port ownership map | Re-derive ownership map used for bid validation in `POST /bids` |
| **Telemetry** | `telemetry_interval`, switch topology | Reset the processing cadence; update flow key set |

### Implementation Approach

Each service that uses the scenario wraps its loaded `Scenario` struct in a thin **watched config** helper:

```go
type WatchedScenario struct {
    mu       sync.RWMutex
    current  *scenario.Scenario
    path     string
    onChange func(*scenario.Scenario)
}

func (w *WatchedScenario) watch(ctx context.Context) {
    ticker := time.NewTicker(15 * time.Second)  // poll, or use fsnotify
    for {
        select {
        case <-ticker.C:
            s, err := scenario.Load(w.path)
            if err == nil && !reflect.DeepEqual(s, w.current) {
                w.mu.Lock()
                w.current = s
                w.mu.Unlock()
                w.onChange(s)
            }
        case <-ctx.Done():
            return
        }
    }
}
```

The `onChange` callback is service-specific. For the auction runner it resets the ticker; for the API it rebuilds the ownership map. Services access the current scenario through `w.Get()` (a read-locked getter) so there is no race between a reload and an in-flight request.

**Polling vs. fsnotify:** Kubernetes ConfigMap volume updates use a symlink swap rather than a regular file write, which can confuse `fsnotify`. A simple 15-second poll of the file's modification time is more reliable and introduces at most 15 seconds of lag — acceptable given auction intervals of 30 seconds or more.

### What Requires Care

- **Auction in progress:** If the scenario reloads mid-auction-round (between reading bids and publishing results), the clearing algorithm should use the scenario snapshot that was valid at the start of that round, not the new one. This is achieved by capturing `scenario.Get()` once at the top of each auction iteration rather than calling it inline.
- **Validation:** The reload must re-run `scenario.Validate()` before applying. An invalid scenario (e.g. a port assigned to two customers) should be rejected and the previous scenario kept.
- **Customer removal:** If a customer is removed from the scenario mid-run, their in-flight bids in Atomix are orphaned. This edge case can be documented as unsupported; removing a customer should still require a restart.

### Design Decisions to Explain

| Decision | Options | Trade-off |
|----------|---------|-----------|
| File watch mechanism | Poll (chosen) vs. fsnotify | Reliability with ConfigMap symlink swaps vs. lower latency |
| Poll interval | 15s | Must be substantially shorter than `auction_interval` to ensure changes take effect before the next round |
| Reload granularity | Reload entire scenario vs. individual fields | Simplicity vs. fine-grained change detection |
| Behaviour during active auction round | Snapshot at round start (chosen) vs. live reads | Consistency within a round vs. immediate effect |

### Feasibility

Medium. The scenario loading code is already centralised in `shared/scenario/load.go`, so the watcher can be implemented once and reused by all services. The per-service `onChange` callbacks are small. The main risk is correctly handling the mid-auction snapshot, which requires a small refactor of how the auction runner accesses the scenario. Estimated effort: 1–2 days.

### Experimental Value

This feature is not itself an experiment, but it is an **experiment enabler**. It makes Experiment 6 (auction interval sensitivity) and any future capacity-variation experiment significantly easier to run: the operator patches `scenario.yaml` in the ConfigMap, waits one poll cycle, and observes the system adapt — without a redeploy.

---

## 6. Suggested Experiments for the Thesis

Each experiment should have a clearly stated **independent variable**, **dependent variable(s)**, and **hypothesis**. The system already exposes Prometheus metrics and Grafana dashboards, making data collection straightforward.

### Experiment 1: Baseline Agent Correctness
**Goal:** Verify that the existing conservative agent eliminates drops over time.
- **Setup:** Run two conservative agents (`as12345`, `as67890`), each owning 5 ingress ports, against a dummy switch that generates traffic approaching capacity (e.g. 90 kbps out of 100 kbps capacity).
- **Independent variable:** None (baseline characterisation).
- **Metrics:** Drop rate per flow, clearing price over time, allocation per customer, credits spent.
- **Expected outcome:** Clearing price stabilises near `reservation_price + small_delta`; drop rate approaches zero after a few intervals.
- **Report value:** Establishes the baseline against which all other experiments are compared.

---

### Experiment 2: Drop-Rate Algorithm vs. Fixed-Margin Algorithm
**Goal:** Quantify how much faster the demand-corrected agent recovers from congestion compared to the current 10% headroom.
- **Setup:** Inject a traffic spike such that a flow suddenly jumps from 50 kbps to 80 kbps (exceeding current allocation). Run the experiment twice: once with the fixed-margin agent and once with the demand-corrected agent.
- **Independent variable:** Agent bidding formula (fixed 10% vs. drop-corrected).
- **Metrics:** Number of auction intervals until drop rate returns to < 5%; total credits spent to achieve recovery; peak drop rate.
- **Expected outcome:** Demand-corrected agent recovers in 1–2 intervals; fixed-margin agent may take 3–5 intervals.
- **Design decision tested:** Does correcting for drops in the bid quantity actually reduce persistent congestion, or do other factors (e.g. competition, market price dynamics) dominate?

---

### Experiment 3: Heterogeneous Agent Strategies — Market Dynamics
**Goal:** Understand how a price-insensitive agent affects the clearing price for all participants.
- **Setup:** Replace one conservative agent (`as67890`) with a price-insensitive agent. Keep `as12345` as conservative. Both generate similar traffic.
- **Independent variable:** Strategy of `as67890` (conservative vs. price-insensitive).
- **Metrics:** Clearing price over time, allocation split between the two customers, total credit spend per customer, drop rate.
- **Expected outcome:** Clearing price rises; conservative agent may lose allocation in heavily contested rounds; price-insensitive agent consistently wins but overpays.
- **Design decision tested:** Does the uniform-price mechanism protect conservative bidders (by capping what they pay at the clearing price), or are they crowded out?

---

### Experiment 4: Budget Awareness and Credit Exhaustion
**Goal:** Evaluate whether a budget-aware agent can sustain throughput longer than an unconstrained agent given the same starting balance.
- **Setup:** Give both agents a finite starting balance. One agent is unconstrained (bids aggressively); the other scales down bids as balance depletes.
- **Independent variable:** Budget-awareness strategy.
- **Metrics:** Total credits spent, allocation received, time until effective credit exhaustion (defined as credits_remaining < one_auction_round_cost), cumulative drop rate.
- **Expected outcome:** Budget-aware agent sustains lower but consistent allocation; unconstrained agent wins large early allocations but degrades sharply once budget is exhausted.
- **Design decision tested:** How important is credit accounting to long-term agent behaviour? Does the current system (no bid rejection for insufficient balance) create problematic incentive misalignments?

---

### Experiment 5: Auction Convergence and Stability
**Goal:** Characterise whether and how quickly the clearing price converges to a stable equilibrium.
- **Setup:** Run two symmetric conservative agents with identical traffic loads for 20+ auction intervals.
- **Independent variable:** None; this is a convergence study.
- **Metrics:** Clearing price per interval (time series), variance of clearing price in the last 10 intervals vs. first 10 intervals.
- **Expected outcome:** Clearing price stabilises within a few intervals at `reservation_price` (when demand < capacity) or at a competitive price (when demand > capacity).
- **Design decision tested:** Is the reservation-price virtual bid an effective floor, and does uniform pricing lead to stable equilibria with simple agents?

---

### Experiment 6: Sensitivity to Auction Interval
**Goal:** Assess how the auction interval length affects agent responsiveness and credit efficiency.
- **Setup:** Run the same scenario at intervals of 10s, 30s (default), and 60s. Use the demand-corrected agent.
- **Independent variable:** `auction_interval` in `scenario.yaml`.
- **Metrics:** Drop rate (especially during traffic spikes), total credits spent over a fixed wall-clock window, clearing price stability.
- **Expected outcome:** Short intervals reduce drop duration but increase total credit spend due to overhead; long intervals reduce spend but increase drop duration during demand changes.
- **Report value:** Provides a quantitative argument for the choice of auction interval — a key design parameter distinguishing this system from traditional IXP peering timescales (weeks vs. minutes).

---

## 6. Key Design Decisions for the Report

The following design decisions cut across all feedback areas and should each have a dedicated subsection in the report, explaining the options considered and the rationale for the choice made.

| # | Decision | Where |
|---|----------|--------|
| D1 | Uniform-price vs. pay-as-bid auction mechanism | `auction/algo/` |
| D2 | Reservation price as a virtual bid vs. as a hard floor | `auction/algo/reservation.go` |
| D3 | Proportional allocation at the margin vs. strict priority | `auction/algo/reservation.go` |
| D4 | How agents translate telemetry (throughput, drop rate) into bid quantity | `agent/agent.go` |
| D5 | Strategy encapsulation: pluggable interface vs. hard-coded logic | `agent/` |
| D6 | One agent per AS vs. one shared agent with customer multiplexing | Deployment |
| D7 | Bid expiry: last-write-wins per round vs. persistent open orders | `api/atomix.go` |
| D8 | Credits accounting only (no rejection) vs. hard credit limits | `api/`, `auction/` |
| D9 | Kafka for auction results vs. direct API callback to switch | `auction/runner/runner.go` |
| D10 | Strimzi in-cluster Kafka vs. external cluster configuration | `kafka/`, Makefile |
| D11 | Single egress port per switch vs. multi-egress generalisation | `etc/scenario/scenario.yaml` |
| D12 | Per-port ownership vs. per-VLAN ownership (future work) | `shared/scenario/` |

---

## 8. Architectural Design Decisions

These are the structural choices that shape the entire system and are worth dedicated treatment in the thesis — not because they are controversial, but because each one has meaningful alternatives and the choice affects correctness, scalability, and experimental reproducibility.

### 7.1 Microservices vs. Monolith

The system is split into five independently deployable services (`api`, `auction`, `telemetry`, `dummy`, `agent`), each a separate Go module with its own container image. The alternative is a single process.

**Why this matters for the thesis:** The split makes it possible to scale or swap one component (e.g. replace the agent strategy) without touching the others. However, it also means state must live outside the processes — which drives the choice of Atomix. The report should justify why loose coupling is worth the operational complexity for an experimental IXP system.

**Alternatives considered:**
- A single Go binary with goroutines: simpler deployment but cannot scale components independently or substitute agents at runtime.
- A service mesh (e.g. Istio): unnecessary overhead for a research prototype.

---

### 7.2 Distributed State: Atomix (Raft) vs. Alternatives

All shared mutable state (flow metrics, bids, credits, auction history) lives in **Atomix** maps backed by a 3-replica Raft consensus store (`atomix/store.yaml`). The services themselves are stateless — any service replica can serve any request.

**Why Raft/Atomix:** Strong consistency guarantees mean that the auction runner reads exactly the bids that were committed before the clearing round begins. No bid is double-counted or lost due to a race between the API writer and the runner reader.

**Alternatives and their trade-offs:**

| Option | Consistency | Complexity | Notes |
|--------|------------|-----------|-------|
| **Atomix (Raft)** *(chosen)* | Strong (linearisable) | Medium | Native Kubernetes CRDs; primitives include maps and counters |
| **Redis** | Eventual (without RedLock) | Low | Widely known; no native Kubernetes operator; consistency requires care |
| **etcd** | Strong | Low | Already in every Kubernetes cluster; API is key-value only; not designed for application data |
| **PostgreSQL** | Strong (ACID) | Medium | Relational model is over-specified for simple map primitives; requires schema migrations |
| **In-memory (per-process)** | None (single-node) | Very low | No fault tolerance; unusable once services scale beyond one replica |

**Design decision for the report:** Justify why linearisable consistency matters specifically for the auction: if two bids arrive for the same (ingress, egress) slot and the runner reads a stale map, it could execute a clearing round against an incomplete bid set, producing allocations that do not reflect actual customer demand.

---

### 7.3 Event Bus: Kafka vs. Direct RPC

Two communication patterns coexist in the system:

- **REST (HTTP)** — synchronous request/response between agents and the API gateway (`POST /bids`, `GET /flows`, etc.).
- **Kafka** — asynchronous, durable event stream for telemetry (`switch-telemetry`) and auction results (`auction-results`).

Kafka is used specifically where the producer and consumer have different lifetimes or rates: the switch emits telemetry every 1 second; the telemetry service processes it and writes to Atomix; the auction runner publishes results once every 30 seconds; the dummy switch consumes results and adjusts its simulated traffic. Neither side needs to be alive at the same instant.

**Alternatives considered:**

| Option | Durability | Decoupling | Notes |
|--------|-----------|-----------|-------|
| **Kafka** *(chosen)* | High (log retention) | High | Allows replay; supports multiple consumers |
| **gRPC streaming** | None | Low (tight coupling) | Both sides must be alive simultaneously |
| **NATS / MQTT** | Low (memory-based unless JetStream) | High | Simpler than Kafka; less operational tooling |
| **Direct HTTP callback** | None | None | Switch must expose an endpoint; fragile |

**Design decision for the report:** Explain why the telemetry-to-Atomix path uses Kafka as an intermediary rather than having the switch write directly to Atomix. The answer: the switch should know nothing about the control-plane state store. Kafka is the integration boundary between the data plane (switch) and the control plane (Atomix-backed services).

---

### 7.4 Auction Mechanism: Uniform-Price with Reservation Floor

The clearing algorithm (`auction/algo/reservation.go`) is a **single-round, sealed-bid, uniform-price auction** with a reservation price enforced by a virtual bid. All winners pay the same clearing price regardless of their submitted price.

**Why uniform-price:** Compared to pay-as-bid (discriminatory price), uniform-price auctions reduce incentive for shading (bidding below true value to reduce payment) and are simpler to reason about for agents. The clearing price is a public signal that agents can observe and adapt to in subsequent rounds.

**The virtual bid trick:** Rather than rejecting under-subscribed auctions or defaulting to zero price when demand is below capacity, a virtual bid at `(units=capacity, price=reservation_price)` is inserted before sorting. This guarantees `clearing_price ≥ reservation_price` even with a single real bidder, and it absorbs remaining capacity in the proportional-allocation step without billing any real customer.

**Alternatives:**

| Mechanism | Price Efficiency | Strategy Complexity | Notes |
|-----------|----------------|---------------------|-------|
| **Uniform-price** *(chosen)* | Good | Low for agents | Clearing price is a transparent market signal |
| **Pay-as-bid** | Variable | Higher (must shade bids) | Used in some electricity markets; harder to reason about in experiments |
| **VCG (Vickrey–Clarke–Groves)** | Theoretically optimal | Very high | Truthful dominant strategy but computationally complex for multi-unit |
| **Posted price (fixed tariff)** | Fixed | Minimal | No market dynamics; cannot adapt to demand changes |

**Design decision for the report:** Justify the choice of uniform-price over VCG. For a bandwidth auction with many repeated rounds, the auction mechanism needs to be simple enough that agents can reason about it in real time — VCG's complexity is not justified for this prototype.

---

### 7.5 Scenario-Driven Configuration

All topology, customer ownership, capacities, and timing parameters live in a single `scenario.yaml`. Services load this file at startup; there is no runtime reconfiguration API.

**Why this is a good design for experiments:** Changing the scenario (e.g. adding more ingress ports, changing capacity, adjusting the auction interval) requires only editing one file and restarting services — not changing code. This is essential for running the experiments in Section 6.

**Limitation to acknowledge:** The scenario must be consistent (every ingress port owned by exactly one customer) and is validated at load time. This means you cannot hot-swap the topology during a running experiment — a deliberate trade-off favouring reproducibility over flexibility.

---

### 7.6 SDN Control Plane Architecture: Centralised Auction Runner

The auction runner is a **single centralised process** that holds the exclusive responsibility for clearing bids and publishing allocations. This is a deliberate centralisation within an otherwise distributed system.

**Why centralised:** The auction clearing algorithm is inherently global — it must see all bids across all customers to compute the correct clearing price and proportional allocation. Distributing it would require a distributed consensus protocol for the clearing computation itself, adding significant complexity for no benefit in a single-switch scenario.

**Scalability consideration for the report:** If the system were extended to multiple switches, each with its own egress port and capacity, the runner could be sharded by egress port (one runner instance per egress port). The existing code already organises bid maps by egress port (`bids-<egressPort>`), so this would be a natural extension.

---

## 9. Tools and Technology Stack

This section enumerates the tools used in the system, the rationale for each choice, and what alternatives exist — all suitable for a "technology choices" section in the thesis.

### 8.1 Programming Language: Go

All services are written in Go. Each service is a separate Go module with its own `go.mod`, vendored dependencies, and Docker image.

**Rationale:** Go's static compilation produces small, single-binary containers with no runtime dependency. Its standard library HTTP server is production-grade, and goroutine-based concurrency maps naturally to the concurrent polling and stream-processing patterns in the agent and telemetry service.

**Alternatives:** Python (slower, dynamic typing makes concurrency error-prone), Java/Kotlin (JVM startup overhead, heavier containers), Rust (safe concurrency but steeper learning curve for a research prototype).

---

### 8.2 Container Orchestration: Kubernetes (Minikube)

All services are deployed as Kubernetes `Deployment` objects. The development environment uses **Minikube** (single-node local cluster). Manifests are plain YAML — no Helm for first-party services.

**Rationale:** Kubernetes provides declarative deployment, service discovery via DNS (`http://api-service:8080`), and environment variable injection — all used extensively. Minikube makes the full cluster reproducible on a laptop without cloud costs.

**Alternatives:** Docker Compose (simpler but no service DNS or health checks), bare metal (no isolation, harder to reproduce), a managed GKE/EKS cluster (realistic but costs money and adds network latency variability to experiments).

---

### 8.3 Distributed State Store: Atomix

Atomix provides **Raft-backed distributed primitives** (maps, counters, locks) as Kubernetes CRDs. The `ConsensusStore` in `atomix/store.yaml` runs 3 replicas with 3 partition groups.

**Rationale:** Atomix was chosen for its native Kubernetes integration (CRD-based configuration, in-cluster deployment) and its strong consistency model. Using a dedicated store rather than the Kubernetes API server (etcd) avoids polluting cluster state with application data and removes the etcd size/rate limits.

**Operational note for the thesis:** For experiments, the 3-replica Raft store adds overhead on Minikube. Reducing to 1 replica is possible for development but removes fault tolerance — worth noting as a configuration dimension.

---

### 8.4 Message Broker: Apache Kafka via Strimzi

Kafka is deployed in-cluster using the **Strimzi operator** in KRaft mode (no ZooKeeper). A single `dual-role` node acts as both controller and broker. The Go Kafka client is `github.com/segmentio/kafka-go`.

**Strimzi rationale:** Declarative Kafka cluster management via Kubernetes CRDs, consistent with the rest of the infrastructure. The `KafkaTopic` and `KafkaNodePool` CRDs make topic configuration version-controlled alongside the rest of the system.

**kafka-go rationale:** Pure Go, no CGo dependency, simple producer/consumer API. The alternative (`confluent-kafka-go`) wraps `librdkafka` via CGo, which complicates cross-compilation and Docker image builds.

---

### 8.5 Observability: Prometheus + Grafana

Prometheus is deployed via the **prometheus-community Helm chart**. The API gateway exposes metrics on `:9090` via a `ServiceMonitor` CRD. Grafana renders the dashboard defined in `observability/ixp-flows.json`.

**Metrics exposed:**

| Metric | Labels | Purpose |
|--------|--------|---------|
| `ixp_flow_throughput_kbps` | switch_id, ingress_port, egress_port | Ingress traffic rate |
| `ixp_flow_egress_kbps` | same | Egress (post-switch) rate |
| `ixp_flow_drop_kbps` | same | Dropped traffic |
| `ixp_flow_drop_rate_percent` | same | Fraction of ingress traffic dropped |
| `ixp_customer_credits_spent_total` | customer_id | Cumulative credit spend |

**Why Prometheus:** The scrape-pull model integrates naturally with Kubernetes (ServiceMonitor selects pods by label). The metrics above are exactly the dependent variables needed for the experiments in Section 6 — they can be exported via Prometheus HTTP API or visualised live in Grafana during experiment runs.

---

### 8.6 Serialisation: JSON and Protobuf

Two serialisation formats are used:

- **JSON** — for all Atomix map values (`FlowMetricsValue`, `CustomerCredits`, `AuctionHistoryRecord`) and REST API request/response bodies. Human-readable, easy to debug with `kubectl exec` and `curl`.
- **Protobuf** — schema definitions exist in `shared/proto/` but are used as documentation/contract rather than for wire encoding in the current implementation. The telemetry Kafka messages use Go struct serialisation.

**Design decision to note in the report:** Using JSON in Atomix maps makes the stored data inspectable but slightly less efficient than binary encoding. For a research prototype where auditability of state is valuable during debugging, this is the right trade-off.

---

### 8.7 Build and Deployment: Make + Docker

The `Makefile` is the single entry point for all build and deployment operations:

| Target | Action |
|--------|--------|
| `make infra` | Minikube + Kafka (Strimzi) + Atomix + observability |
| `make services` | Build Docker images + deploy all services |
| `make deploy-agent` | Build and deploy customer agents |
| `make grafana-ui` | Port-forward Grafana to localhost:3000 |
| `make test` | Unit tests for `api/` and `agent/` |

**Rationale:** A flat Makefile keeps the entire lifecycle in one place and is familiar to systems researchers. The alternative (a CI/CD pipeline like GitHub Actions) would add overhead without benefit for local experiments.

---

## 10. Summary of Priorities

| Feedback | Feasibility | Thesis Value | Suggested Action |
|----------|------------|--------------|-----------------|
| Drop-rate bidding formula | High | High | Implement; use as Experiment 2 comparison |
| Multiple agent strategies | Medium-High | High | Implement 2–3 strategies; run Experiments 3 & 4 |
| VLAN/MAC multi-AS per port | Low (for full impl) | Medium | Document as design section + future work |
| External Kafka config | High | Low-Medium | Makefile variable + `KAFKA_EXTERNAL` guard; document switch procedure |
| Scenario ConfigMap hot-reload | Medium | Medium (experiment enabler) | Implement `WatchedScenario` poller; enables Experiment 6 without redeploying |

| Feedback | Feasibility | Thesis Value | Suggested Action |
|----------|------------|--------------|-----------------|
| Drop-rate bidding formula | High | High | Implement as Experiment 2 comparison |
| Multiple agent strategies | Medium-High | High | Implement 2–3 strategies, run Experiments 3 & 4 |
| VLAN/MAC multi-AS per port | Low (for full impl) | Medium | Document as design section + future work |
| External Kafka config | High | Low-Medium | Implement ConfigMap approach, document in deployment guide |
