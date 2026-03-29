# IXP-GCP — Internet Exchange Point Control Plane

A research control plane for studying bidding strategies in a bandwidth auction market. The system runs on Kubernetes (Minikube for local development) and implements a uniform-price (second-price) auction mechanism for egress bandwidth allocation. Customer agents bid autonomously using configurable strategies; results are exposed via Prometheus and Grafana.

**Scope boundary:** this project implements the control plane (API, auction runner, telemetry processor, bidding agents). Traffic enforcement on the switch is handled by a separate project; this system communicates with the switch via Kafka.

---

## Prerequisites and Setup

- [Go 1.25+](https://go.dev/doc/install)
- [Docker](https://docs.docker.com/engine/install)
- [kubectl](https://kubernetes.io/docs/tasks/tools)
- [Helm](https://helm.sh/docs/intro/install)
- A running Kubernetes cluster (Minikube recommended — see `docs/DEPLOYMENT.md`)

```bash
make setup         # register Helm repos (atomix, prometheus-community)
make deploy-minikube  # start Minikube with 4 CPUs and 8 GiB (first time only)
```

---

## Quick Start — Core Control Plane

```bash
make all
```

Deploys Atomix, Kafka, Prometheus/Grafana, and the API + auction runner + telemetry services. No dummy traffic producer or agent pods are started.

---

## Running an Experiment

```bash
make all experiment=2a
```

Deploys the core control plane, loads the scenario YAML for experiment `2a`, starts the dummy traffic producer, and deploys one agent pod per customer defined in the scenario.

```bash
make grafana-ui    # port-forward Grafana to http://localhost:3000
make prometheus-ui # port-forward Prometheus to http://localhost:9090
```

After the run:

```bash
make export-metrics   # saves data/experiment-<timestamp>.json
```

See the [Experiment Reference](#experiment-reference) table for all available experiments. Full run procedures are in `docs/EXPERIMENTS.md`.

---

## API Reference

All customer-facing endpoints require a customer identity header:
- `X-Customer-ID: <customer_id>`, or
- `Authorization: Bearer <customer_id>`

| Method | Path | Parameters | Response | Notes |
|--------|------|------------|----------|-------|
| `GET` | `/flows` | **Query:** `switch_id`, `ingress_port`, `egress_port` | `{ "<key>": { "throughput_kbps", "egress_kbps", "drop_kbps", "drop_rate_pct" } }` | `403` if port not owned; `404` if no telemetry yet |
| `POST` | `/bids` | **Body:** `ingress_port`, `egress_port`, `units`, `unit_price` | `202 Accepted` | Consumed at next auction interval. `403` if port not owned |
| `GET` | `/credits` | — | `{ "total_spent": int, "starting_balance": int }` | Accounting only; spending never blocks a bid |
| `GET` | `/auctions` | **Query:** `egress_port` (optional) | Array of `AuctionHistoryRecord` | Filtered to caller's own allocations |
| `GET` | `/metrics` | — | Prometheus text format | No auth required |

---

## Prometheus Metrics

| Metric | Labels | Description |
|--------|--------|-------------|
| `ixp_flow_throughput_kbps` | `switch_id`, `ingress_port`, `egress_port` | Raw ingress rate per flow |
| `ixp_flow_egress_kbps` | `switch_id`, `ingress_port`, `egress_port` | Forwarded rate (capped by allocation) |
| `ixp_flow_drop_kbps` | `switch_id`, `ingress_port`, `egress_port` | Dropped traffic (ingress − egress) |
| `ixp_flow_drop_rate_percent` | `switch_id`, `ingress_port`, `egress_port` | Drop rate as a percentage of ingress |
| `ixp_auction_clearing_price` | `egress_port` | Clearing price from the most recent auction round |
| `ixp_customer_allocation_kbps` | `customer_id`, `egress_port` | Allocated bandwidth (most recent round) |
| `ixp_customer_credits_spent_total` | `customer_id` | Cumulative credits spent |
| `ixp_agent_utility_total` | `customer_id` | Cumulative utility: Σ `(valuation_per_unit − clearing_price) × allocated_units` |

Per-round utility: `increase(ixp_agent_utility_total[30s])` (matches the default `auction_interval`).

---

## Agent Strategies

Each customer in the scenario YAML specifies a `strategy` and optionally `valuation_per_unit` (the customer's maximum willingness to pay per kbps unit; defaults to `10 × reservation_price`). Utility is calculated as `(valuation_per_unit − clearing_price) × allocated_units`.

| Strategy | Bid units | Bid price | Key behaviour |
|----------|-----------|-----------|---------------|
| `conservative` | 110% of throughput (min 1 kbps) | Last clearing price (floor: reservation price) | Skips zero-traffic flows; never bids on dropped traffic — recovers slowly from congestion |
| `demand_corrected` | 105% of throughput + drops (min 1 kbps) | Last clearing price (floor: reservation price) | Accounts for drops in unit estimate; recovers from congestion faster |
| `price_insensitive` | 105% of throughput + drops (min 1 kbps) | Fixed multiple of reservation price (default 10×) | Ignores market price; models latency-critical traffic. Param: `price_multiplier` |
| `budget_aware` | 105% of throughput + drops (min 1 kbps) | Tiers by remaining credit fraction: >75% → clearing; >50% → 75% of clearing; >25% → 50% of clearing; ≤25% → reservation | Stretches finite credits. Falls back to `conservative` when `starting_balance` is 0. Params: `ema_alpha`, `budget_epsilon` |
| `valuation_based` | 105% of throughput + drops (min 1 kbps) | Exactly `valuation_per_unit` (floor: reservation price) | **Dominant strategy for second-price auctions.** Wins whenever clearing price ≤ valuation; earns positive utility. Requires `valuation_per_unit` in scenario. |
| `throughput_optimizer` | Varies (0.8×–1.2× demand) | Depends on market conditions: `valuation_per_unit` (cheap+high), last clearing (expensive+high), 90% of clearing (cheap+low), reservation price (expensive+low) | Bids aggressively when market is cheap AND demand is high; conserves credits otherwise. Params: `price_threshold` (default 0.8), `high_demand_kbps` (default 80), `price_window` (default 3) |
| `q_learning` | 105% of throughput + drops (min 1 kbps) | Last clearing × learned multiplier `[0.8×, 1.0×, 1.25×, 1.5×, 2.0×, 3.0×]` (floor: reservation price) | Tabular Q-learning over `(drop_bucket, budget_bucket)` state. Reward = `(valuation − clearing_price) × allocated_units`. Params: `ql_alpha` (default 0.1), `ql_gamma` (default 0.9), `ql_epsilon` (default 0.15) |
| `exploratory` *(deprecated)* | 105% of throughput + drops (min 1 kbps) | EMA of clearing prices + epsilon (floor: reservation price) | **Retained for Experiment 9 (negative result).** EMA price-following is suboptimal in a second-price auction. Params: `ema_alpha` (default 0.3), `ema_epsilon` (default 5) |

---

## Experiment Reference

Run any experiment with `make all experiment=<id>` or the legacy `make deploy-experiment-<id>`.

| ID | Scenario file | Strategies | What to observe |
|----|--------------|------------|-----------------|
| `1` | `experiment-1-baseline.yaml` | conservative × 2 | Drops fall to ~0 after 1–2 intervals; stable clearing price |
| `2a` | `experiment-2a-conservative-spike.yaml` | conservative × 2 | Drops persist ~23% after spike; neither agent adapts |
| `2b` | `experiment-2b-demand-corrected-spike.yaml` | conservative vs demand_corrected | `as67890` wins more allocation post-spike; allocation diverges |
| `3` | `experiment-3-heterogeneous.yaml` | conservative vs price_insensitive | `as67890` wins full allocation; `as12345` absorbs all drops |
| `4a` | `experiment-4a-conservative-budget.yaml` | conservative × 2 (finite budget) | Abrupt credit exhaustion cliff |
| `4b` | `experiment-4b-budget-aware.yaml` | budget_aware × 2 (finite budget) | Gradual degradation; credits stretch longer |
| `4c` | `experiment-4c-throughput-optimizer.yaml` | conservative vs throughput_optimizer | Optimizer adapts to spike; utility gap widens |
| `5` | `experiment-5-convergence.yaml` | conservative × 2 | Clearing price and allocation stabilise within a few intervals |
| `6a` | `experiment-6a-interval-10s.yaml` | demand_corrected × 2 | Fast reaction (10s); higher credit expenditure |
| `6b` | `experiment-6b-interval-30s.yaml` | demand_corrected × 2 | Baseline 30s interval |
| `6c` | `experiment-6c-interval-60s.yaml` | demand_corrected × 2 | Slow reaction; sustained drops between rounds |
| `7` | `experiment-7-valuation-dominant-strategy.yaml` | conservative vs valuation_based | Conservative loses allocation when clearing price spikes; utility gap validates dominant strategy theorem |
| `7b` | `experiment-7b-qlearning-convergence.yaml` | q_learning vs valuation_based | Q-learner's utility trends toward valuation_based baseline over 50+ intervals |
| `8` | `experiment-8-mixed-valuations.yaml` | valuation_based (250) vs valuation_based (1000) | High-value agent wins majority of allocation; clearing price ~250 |
| `9` | `experiment-9-ema-negative-result.yaml` | exploratory vs valuation_based | EMA lags clearing-price spike; valuation_based wins allocation that exploratory misses |

> Reset Atomix state between experiments that track credits (4a, 4b) to avoid carry-over. See `docs/EXPERIMENTS.md` for reset steps.

---

## Exporting Data

```bash
make prometheus-ui     # port-forward Prometheus (if not running)
make export-metrics    # saves data/experiment-<timestamp>.json
```

The JSON file contains time-series for: `clearing_price`, `allocation_kbps`, `flow_drop_rate`, `flow_throughput`, `utility_per_round`, and `cumulative_utility`.

Override the time window: `make export-metrics SINCE=2h`

---

## External Kafka

**Plaintext:**
```bash
make all KAFKA_BOOTSTRAP=192.168.1.50:9092
```

**mTLS (e.g. Aiven):**
```bash
kubectl create secret generic kafka-tls \
  --from-file=ca.pem --from-file=service.cert --from-file=service.key
make all \
  KAFKA_BOOTSTRAP=kafka-xxx.aivencloud.com:12345 \
  KAFKA_TLS_CA_FILE=/etc/kafka-tls/ca.pem \
  KAFKA_TLS_CERT_FILE=/etc/kafka-tls/service.cert \
  KAFKA_TLS_KEY_FILE=/etc/kafka-tls/service.key
```

On Aiven, create topics: `switch-telemetry`, `auction-results`.

---

## References

- [Atomix](https://atomix.github.io)
- [Vickrey, W. (1961). Counterspeculation, Auctions, and Competitive Sealed Tenders. *Journal of Finance*, 16(1), 8–37.](https://doi.org/10.1111/j.1540-6261.1961.tb02789.x)
