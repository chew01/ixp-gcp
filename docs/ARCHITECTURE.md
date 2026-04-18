# Architecture

## System Overview

```
                         ┌──────────────────────────────────────────────────────────────────┐
                         │  Kubernetes Cluster (Minikube / GCP GKE)                         │
                         │                                                                    │
  ┌──────────────┐       │  ┌─────────────┐    REST     ┌───────────────┐                   │
  │  Customer    │──────▶│  │ API Server │◀───────────▶│ Customer      │                   │
  │  Agent(s)    │       │  │  :8080/:9090│             │ Agent pod(s)  │                   │
  └──────────────┘       │  └──────┬──────┘             └───────────────┘                   │
                         │         │ Atomix                                                   │
                         │         ▼                                                          │
  ┌──────────────┐       │  ┌─────────────┐   Kafka    ┌───────────────┐                   │
  │  Physical /  │──────▶│  │  Telemetry  │──────────▶│ Auction Runner│                   │
  │  Dummy Switch│       │  │  Processor  │            │               │                   │
  └──────────────┘       │  └─────────────┘            └───────┬───────┘                   │
                         │                                      │ Atomix                     │
                         │  ┌─────────────────────────────────┐│                            │
                         │  │  Atomix (distributed state)     ││                            │
                         │  │  • throughput-map  (flows)      ││                            │
                         │  │  • bids-<egressPort>            ││                            │
                         │  │  • credits-map                  ││                            │
                         │  │  • utility-map                  ││                            │
                         │  │  • auction-history              ││                            │
                         │  └─────────────────────────────────┘│                            │
                         │                                      ▼                            │
                         │                             ┌────────────────┐                   │
                         │  ┌──────────────────────┐   │  Kafka         │                   │
                         │  │  Prometheus + Grafana │   │ (auction-      │                   │
                         │  │  (monitoring ns)     │   │  results,      │                   │
                         │  └──────────────────────┘   │  switch-       │                   │
                         │                             │  telemetry)    │                   │
                         │                             └────────────────┘                   │
                         └──────────────────────────────────────────────────────────────────┘

Scope boundary: everything inside the dashed box is this project.
The local switch controller is external to the control plane;
Kafka is the interface between them.
```

---

## Component Responsibilities

### API Server (`api/`)

- **REST API** on `:8080` for agent interactions (`/flows`, `/bids`, `/credits`, `/auctions`).
- **Prometheus metrics** on `:9090` (`/metrics`) — refreshes all gauges from Atomix on each scrape.
- **Bid store:** writes incoming bids to `bids-<egressPort>` Atomix maps (one map per egress port).
- **Flow store:** reads telemetry from `throughput-map` to serve `/flows`.
- **Auction history store:** reads `auction-history` to serve `/auctions`.
- **Credits store:** reads `credits-map` to serve `/credits`.
- **Utility store:** reads `utility-map` to serve `ixp_agent_utility_total` Prometheus metric.
- Enforces customer identity: bids and flow queries are validated against the scenario's port ownership map.

### Auction Runner (`auction/`)

- Ticks once per `auction_interval` (from the scenario YAML).
- For each egress port: reads all bids from `bids-<egressPort>`, runs `RunReservationPriceAuction`, writes Kafka results, updates `credits-map`, `utility-map`, and `auction-history`.
- Implements a **uniform-price (second-price) auction**: a virtual supply bid at `reservation_price` ensures the clearing price is always at least the reservation price. Bids are sorted by price descending; the clearing price is set by the marginal accepted bidder. Proportional splitting is applied at the marginal tier.
- Clears the bid map after each auction to ensure old bids don't carry over to the next interval.

### Telemetry Processor (`telemetry/`)

- Consumes `switch-telemetry` Kafka topic.
- Converts raw `TelemetryRecord` messages (ingress/egress byte counts) into per-flow `FlowMetricsValue` (throughput, egress, drop rate).
- Writes to `throughput-map` in Atomix.

### Dummy Producer (`dummy/`)

- Simulates traffic from a switch: sends `TelemetryRecord` messages on the `switch-telemetry` Kafka topic.
- Supports three traffic patterns (set in scenario YAML):
  - `random`: random rates per flow each interval
  - `steady`: fixed rate per flow
  - `spike`: fixed rate that jumps to a higher rate after `spike_after_intervals`
- Also sends `AuctionResultRecord` messages on `auction-results` to simulate the switch enforcing allocations.

### Customer Agent (`agent/`)

- One pod per customer. Reads `CUSTOMER_ID`, `API_BASE_URL`, `SCENARIO_PATH` from the environment.
- Every auction interval: fetches `/credits`, then for each `(ingress, egress)` pair calls `/flows`, `/auctions` (for clearing price and last allocation), constructs a `BidContext`, runs the configured strategy's `ComputeBid`, and submits to `POST /bids`.
- Strategy is selected by name from the scenario YAML `strategy` field; parameters from `strategy_params`.

---

## State Management

| Store (Atomix map) | Key | Value | Writer | Readers |
|--------------------|-----|-------|--------|---------|
| `throughput-map` | `<switchID>\|<ingress>\|<egress>` | JSON `FlowMetricsValue` | Telemetry Processor | API Server |
| `bids-<egressPort>` | `<ingressPort>` | `units\|unitPrice\|customerID` | API Server | Auction Runner |
| `credits-map` | `<customerID>` | JSON `CustomerCredits` | Auction Runner (spend), API Server (init) | API Server, Agent (via `/credits`) |
| `utility-map` | `<customerID>` | Integer string (cumulative utility) | Auction Runner | API Server |
| `auction-history` | `<intervalID>\|<egressPort>` | JSON `AuctionHistoryRecord` | Auction Runner | API Server |

All maps are **last-write-wins**. Bid maps are cleared after each auction. Utility and credits maps accumulate across the experiment lifetime.

---

## Key Design Decisions

| ID | Decision | Rationale |
|----|----------|-----------|
| D1 | Uniform-price auction | Single clearing price = second-price semantics → dominant strategy is to bid true valuation. |
| D2 | `valuation_per_unit` per customer | Enables utility calculation: `(valuation − clearing) × units`. Required for utility-aligned rewards in Q-learning and for `valuation_based` strategy. |
| D3 | Atomix for shared state | Distributed, consistent key-value store; survives pod restarts. |
| D4 | Kafka as switch interface | Decouples control plane from data plane enforcement. Enables the local switch controller to stand in for a physical switch. |
| D5 | Prometheus + Grafana for observability | Standard ecosystem; scrape interval matches auction interval. |
| D6 | gen-agent-deployments | Generates per-customer K8s Deployments from scenario YAML at deploy time — no hardcoded customer list in the Makefile. |
| D7 | Utility-aligned Q-learning reward | `(valuation − clearing_price) × allocated_units` aligns RL reward with economic efficiency, allowing Q-learner to potentially discover the dominant strategy. |
| D8 | `exploratory` deprecated | EMA price-following is theoretically suboptimal in a second-price auction (bid should equal valuation, not track clearing price). Retained for Experiment 9 negative-result only. |
| D9 | Vendor directories per module | Each Go module (`agent`, `api`, `auction`, etc.) has its own `vendor/` so each can be built in isolation without network access. |
| D10 | `make all` vs `make all experiment=X` | Separates "start the control plane" (production-like) from "run an experiment" (research). Prevents accidental traffic generation in a bare deployment. |
| D11 | Virtual supply bid at reservation price | Prevents the auction from clearing at zero when only one customer bids. Reservation price acts as a price floor. |
| D12 | Proportional splitting at marginal tier | Fair rationing when multiple bidders tie at the clearing price. |
