# Experiments Guide

## Prerequisites

1. Kubernetes cluster running (Minikube recommended).
2. Core control plane deployed: `make all`
3. Grafana accessible: `make grafana-ui` → http://localhost:3000
4. Prometheus accessible: `make prometheus-ui` → http://localhost:9090

---

## Running an Experiment

```bash
make all experiment=<id>
```

This command:
1. Deploys the core control plane (if not already running).
2. Loads the experiment's scenario YAML into the `test-scenario` ConfigMap.
3. Deploys the dummy traffic producer.
4. Restarts auction runner, telemetry, and API server with the new scenario.
5. Removes any old agent pods (`kubectl delete deployment -l app=customer-agent`).
6. Generates and applies one agent Deployment per customer defined in the scenario.

**Switching experiments:** run `make all experiment=<new-id>` again. Old agents are removed and new ones start.

---

## Experiment Reference

| ID | Scenario file | Strategies | What to observe |
|----|--------------|------------|-----------------|
| `1` | `experiment-1-baseline.yaml` | conservative × 2 | Drops fall to ~0 after 1–2 intervals; stable clearing price |
| `2a` | `experiment-2a-conservative-spike.yaml` | conservative × 2 | Drops persist ~23% after spike; neither agent adapts |
| `2b` | `experiment-2b-demand-corrected-spike.yaml` | conservative vs demand_corrected | `as67890` wins more allocation post-spike; allocation diverges |
| `3` | `experiment-3-heterogeneous.yaml` | conservative vs price_insensitive | `as67890` wins full allocation; `as12345` absorbs all drops |
| `4a` | `experiment-4a-conservative-budget.yaml` | conservative × 2 (finite budget) | Abrupt credit exhaustion cliff |
| `4b` | `experiment-4b-budget-aware.yaml` | budget_aware × 2 (finite budget) | Gradual degradation; credits stretch longer |
| `4c` | `experiment-4c-throughput-optimizer.yaml` | conservative vs throughput_optimizer | Optimizer adapts to spike; utility gap widens |
| `5` | `experiment-5-convergence.yaml` | valuation_based × 2 | Clearing price and allocation stabilise within a few intervals |
| `6a` | `experiment-6a-interval-10s.yaml` | demand_corrected × 2 | Fast reaction (10s); higher credit expenditure |
| `6b` | `experiment-6b-interval-30s.yaml` | demand_corrected × 2 | Baseline 30s interval |
| `6c` | `experiment-6c-interval-60s.yaml` | demand_corrected × 2 | Slow reaction; sustained drops between rounds |
| `7` | `experiment-7-valuation-dominant-strategy.yaml` | conservative vs valuation_based | Conservative loses allocation when clearing price spikes; utility gap validates dominant strategy theorem |
| `7b` | `experiment-7b-qlearning-convergence.yaml` | q_learning vs valuation_based | Q-learner's utility trends toward valuation_based baseline over 50+ intervals |
| `8` | `experiment-8-mixed-valuations.yaml` | valuation_based (250) vs valuation_based (1000) | High-value agent wins majority of allocation; clearing price ~250 |
| `9` | `experiment-9-ema-negative-result.yaml` | exploratory vs valuation_based | EMA lags clearing-price spike; valuation_based wins allocation that exploratory misses |

> Reset Atomix state between experiments that track credits (4a, 4b) to avoid carry-over. See **Resetting State** below.

---

## Resetting State Between Experiments

Atomix state (credits, utility, auction history, bids) **persists across experiments**. Always reset before experiments that depend on clean credit balances (4a, 4b) or a fresh utility baseline.

```bash
# Delete all Atomix maps (drops ALL state — clearing price history, credits, utility, bids)
kubectl exec -it $(kubectl get pods -l app=atomix-runtime -o name | head -1) -- \
  /bin/sh -c 'rm -rf /data/*'
kubectl rollout restart deployment/atomix-runtime
kubectl rollout restart deployment/auction-runner
kubectl rollout restart deployment/api-gateway
```

Alternatively, scale the Atomix StatefulSet to 0 and back to 1 to force a clean re-init.

---

## Experiment Procedures

### Experiment 1 — Baseline Agent Correctness

**Goal:** Verify the auction and agent work correctly under normal conditions.

```bash
make all experiment=1
```

**Run duration:** 20 intervals (10 minutes at 30s/interval)

**Watch in Grafana:**
- `ixp_flow_drop_rate_percent`: should fall to ~0 after 1–2 intervals
- `ixp_auction_clearing_price`: stable at reservation price (50)
- `ixp_customer_allocation_kbps`: symmetric between agents

**Export:**
```bash
make export-metrics
```

---

### Experiment 2a — Conservative Symmetric Spike

**Goal:** Establish the symmetric congestion baseline where neither agent adapts.

```bash
make all experiment=2a
```

**Run duration:** 20 intervals (10 minutes)

**Watch in Grafana:**
- Drop rate spikes at interval 5, persists at ~23% — neither agent adapts

---

### Experiment 2b — Conservative vs Demand-Corrected Spike

**Goal:** Demonstrate that demand-corrected bidding recovers from congestion faster.

```bash
make all experiment=2b
```

**Run duration:** 20 intervals. Compare against 2a.

**Watch in Grafana:**
- `as67890` (demand_corrected) allocation diverges upward from `as12345` after the spike
- `as12345` drop rate is higher than in 2a because `as67890` outbids it

---

### Experiment 3 — Heterogeneous Strategies

**Goal:** Show the priority effect — high price bids win allocation regardless of cost.

```bash
make all experiment=3
```

**Run duration:** 20 intervals.

**Watch in Grafana:**
- `as67890` (price_insensitive, 500/unit) wins full allocation every round
- `as12345` (conservative) absorbs all drops; clearing price stays at 50 (not 500)

---

### Experiment 4a — Conservative with Finite Budget

**Goal:** Show abrupt credit exhaustion for a price-following agent.

```bash
make all experiment=4a
```

**Run duration:** Until exhaustion or 40 intervals.

**Watch in Grafana:**
- `ixp_customer_credits_spent_total`: rises linearly, then throughput collapses

---

### Experiment 4b — Budget-Aware with Finite Budget

**Goal:** Show gradual degradation vs. the cliff in 4a.

Reset Atomix state before running.

```bash
make all experiment=4b
```

**Run duration:** Until exhaustion or 40 intervals.

---

### Experiment 4c — Throughput Optimizer vs Conservative

**Goal:** Show that intertemporal bidding outperforms price-following under volatile demand.

```bash
make all experiment=4c
```

**Run duration:** 25 intervals.

**Watch in Grafana:**
- `as67890` (throughput_optimizer) bids aggressively during low-demand phase; adapts to spike
- Utility gap widens at interval 5 (spike onset)

---

### Experiment 5 — Auction Convergence and Stability

**Goal:** Measure how many intervals until clearing price and allocation stabilise.

```bash
make all experiment=5
```

**Run duration:** 30 intervals.

**Watch in Grafana:**
- Clearing price variance; measure steady-state standard deviation

---

### Experiments 6a / 6b / 6c — Auction Interval Sensitivity

**Goal:** Compare reaction speed and credit expenditure across interval lengths.

```bash
make all experiment=6a  # 10s
make all experiment=6b  # 30s (baseline)
make all experiment=6c  # 60s
```

**Run duration:** 10 minutes wall clock for each.

**Compare:** drop duration after spike, total credits spent, number of rounds.

---

### Experiment 7 — Dominant Strategy Validation

**Goal:** Demonstrate that `valuation_based` wins allocation that `conservative` loses after a clearing-price spike.

```bash
make all experiment=7
```

**Run duration:** 25 intervals.

**Watch in Grafana:**
- `ixp_customer_allocation_kbps`: `as12345` (conservative) drops to zero at the spike; `as67890` (valuation_based) maintains full allocation
- `increase(ixp_agent_utility_total[30s])`: utility gap widens at interval 5

---

### Experiment 7b — Q-Learning Convergence

**Goal:** Show that the Q-learner, with utility-aligned reward, trends toward the dominant strategy over 50+ intervals.

```bash
make all experiment=7b
```

**Run duration:** 50 intervals (~25 minutes at 30s/interval).

**Watch in Grafana:**
- `increase(ixp_agent_utility_total[30s])` for `as12345` (q_learning): should trend upward over time toward `as67890` (valuation_based) baseline

---

### Experiment 8 — Mixed Valuations

**Goal:** Show that in a two-agent truthful auction, the higher-valuation agent wins the majority of contested allocation.

```bash
make all experiment=8
```

**Run duration:** 20 intervals.

**Watch in Grafana:**
- `ixp_customer_allocation_kbps`: `as67890` (valuation=1000) wins majority; `as12345` (valuation=250) wins residual
- Clearing price converges near 250 (low-valuation agent's ceiling)

---

### Experiment 9 — EMA Negative Result

**Goal:** Demonstrate that EMA price-following is structurally suboptimal after a spike.

```bash
make all experiment=9
```

**Run duration:** 20 intervals.

**Watch in Grafana:**
- After spike at interval 5: `as12345` (exploratory) loses allocation for 2–3 rounds; `as67890` (valuation_based) wins
- Cumulative utility: `as67890 > as12345` throughout

---

## Interpreting Exported Data

```bash
make export-metrics
# Output: data/experiment-<timestamp>.json
```

The JSON file structure:

```json
{
  "clearing_price": { /* Prometheus range query result */ },
  "allocation_kbps": { /* per-customer per-egress-port */ },
  "flow_drop_rate": { /* per-flow drop rate */ },
  "flow_throughput": { /* per-flow throughput */ },
  "utility_per_round": { /* increase(ixp_agent_utility_total[30s]) */ },
  "cumulative_utility": { /* ixp_agent_utility_total */ }
}
```

Each field is a Prometheus `query_range` result with `resultType`, `result[]` (label-set + `values[]` arrays of `[timestamp, value]`).

Override window: `make export-metrics SINCE=2h`

---

## `make all experiment=X` Shortcut

This is the recommended way to run experiments. It:
1. Calls `make infra` (idempotent — Helm upgrades, not installs)
2. Calls `make services` (rebuilds and reloads service images)
3. Calls `make load-experiment` which:
   - Validates the scenario file exists
   - Updates the ConfigMap
   - Restarts the services
   - Removes old agent pods
   - Generates and applies new agent Deployments via `scripts/gen-agent-deployments`

The legacy `make deploy-experiment-<id>` targets still work as aliases.
