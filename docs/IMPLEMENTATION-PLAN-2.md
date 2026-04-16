# Implementation Plan — Phase 2

This plan translates all decisions from `PROFESSOR-FEEDBACK-ROUND2.md` into concrete, ordered implementation steps. Each phase is self-contained and can be reviewed independently. Estimated effort is given per phase.

---

## Overview

| Phase | Description | Effort |
|-------|-------------|--------|
| 1 | Codebase cleanup — remove backoff, deprecate exploratory | ½ day |
| 2 | Universal `valuation_per_unit` in config and all strategies | 1 day |
| 3 | New agent strategies: `valuation_based`, `throughput_optimizer`; revise Q-learning | 2–3 days |
| 4 | Utility metrics in Prometheus and Grafana | 1 day |
| 5 | Makefile redesign — `make all` (core only) + `make all experiment=X` | 1–2 days |
| 6 | New experiment YAML files | 1 day |
| 7 | Full documentation rewrite | 2–3 days |
| 8 | Tests | 1 day |

**Total estimated effort: ~10–12 days**

Phases 1, 2, and 3 can be worked sequentially as each builds on the previous. Phase 4 is independent and can run in parallel with Phase 3. Phases 5 and 6 depend on Phase 2 (scenario schema) but not on Phase 3. Phase 7 should be done last.

---

## Phase 1: Codebase Cleanup

**Goal:** Remove the `backoff` strategy entirely; mark `exploratory` as deprecated but keep it for the negative-result experiment.

### Step 1.1 — Delete backoff strategy

Delete these two files:
- `agent/strategy/backoff.go`
- `agent/strategy/backoff_test.go`

### Step 1.2 — Remove backoff from strategy selector

In `agent/main.go`, remove the `case "backoff":` branch from `selectStrategy()`.

### Step 1.3 — Mark exploratory as deprecated

Add the following header comment to `agent/strategy/exploratory.go`:

```go
// Package strategy — exploratory (EMA-based)
//
// DEPRECATED: This strategy is retained solely for the negative-result experiment
// (Experiment 9). EMA-based price-following is theoretically misaligned with the
// uniform-price (second-price) auction mechanism used by this system. In a
// second-price auction the dominant strategy is to bid your true valuation, not to
// track the clearing price. See docs/AGENTS.md §EMA for the full argument.
//
// Do not use this strategy in production or new experiments.
```

### Step 1.4 — Update README strategy table

Update the agent strategy table in `README.md` to:
- Remove `backoff` row
- Add a `(deprecated)` note to `exploratory`
- Add placeholder rows for `valuation_based` and `throughput_optimizer` (to be filled in Phase 3)

**Verification:** `go test ./agent/...` passes; `go build ./agent` compiles without the backoff import.

---

## Phase 2: Universal `valuation_per_unit`

**Goal:** Every customer in every scenario has an explicit `valuation_per_unit`. This value caps the bid price for all strategies and is the exact bid price for `valuation_based`. It also enables consistent utility calculation across all agents.

### Step 2.1 — Extend the Customer struct in the scenario schema

In `shared/scenario/types.go`, add a field to the `Customer` struct:

```go
ValuationPerUnit int `yaml:"valuation_per_unit"`
```

Place it after `StrategyParams`. The field is per-customer because different customers may have different valuations (the central point of Experiment 8).

### Step 2.2 — Apply a default in scenario validation

In `shared/scenario/load.go`, after loading the scenario, add a validation pass: if `customer.ValuationPerUnit <= 0`, set it to `10 * scenario.ReservationPrice`. This preserves backward compatibility — all existing YAMLs without the field will behave as before.

```go
for i := range s.Customers {
    if s.Customers[i].ValuationPerUnit <= 0 {
        s.Customers[i].ValuationPerUnit = 10 * s.ReservationPrice
    }
}
```

### Step 2.3 — Pass valuation into BidContext

In `agent/strategy/strategy.go`, add `ValuationPerUnit int` to `BidContext`.

In `agent/main.go`, populate it when building the context in `placeBidForFlow`: look up the customer's `ValuationPerUnit` from the loaded scenario.

**Important — do NOT apply a universal price cap.** `valuation_per_unit` serves two distinct roles:

| Role | Applies to | Mechanism |
|------|-----------|-----------|
| Bid price (strategic) | `valuation_based` only | The strategy bids *exactly* this value |
| Utility calculation (accounting) | Every agent | `utility = (valuation_per_unit − clearing_price) × allocated_units` |

Applying `bid_price = min(strategy_price, valuation_per_unit)` to all strategies would silently alter the semantics of `price_insensitive` (which intentionally bids a fixed multiplier of reservation price, independent of valuation) and would conflate it with `valuation_based`. In normal operation, price-following strategies (`conservative`, `demand_corrected`, `budget_aware`) bid at or below clearing price, which is below valuation — the cap would never fire anyway.

### Step 2.4 — Add a load-time validation warning

In `shared/scenario/load.go`, after defaulting `ValuationPerUnit`, add a log warning if a customer's `ValuationPerUnit < scenario.ReservationPrice`. This would guarantee negative utility on every round and is almost certainly a misconfiguration:

```go
if c.ValuationPerUnit < s.ReservationPrice {
    log.Printf("warning: customer %s valuation_per_unit (%d) is below reservation_price (%d); utility will be negative every round",
        c.ID, c.ValuationPerUnit, s.ReservationPrice)
}
```

This is a warning, not a hard error, to allow deliberate low-valuation experiments (e.g. Experiment 8's low-value agent).

### Step 2.5 — Add `valuation_per_unit` to all existing experiment YAMLs

Add the field to each customer block in all existing experiment files. Use a consistent value of `500` (10× the `reservation_price` of 50) as the default across experiments that are not specifically testing valuation sensitivity:

```yaml
customers:
  - id: as12345
    switch_id: sw-1
    ingress_ports: [1, 2, 3, 4, 5]
    strategy: conservative
    valuation_per_unit: 500
```

Experiments 7 and 8 (new, Phase 6) will use different values per customer.

**Verification:** `go test ./agent/... ./shared/...` passes. `make all experiment=1` deploys with the updated scenario.

---

## Phase 3: New Agent Strategies

**Goal:** Add `valuation_based` and `throughput_optimizer` strategies. Revise the Q-learning reward to align with utility.

### Step 3.1 — `valuation_based` strategy

Create `agent/strategy/valuation_based.go`:

```go
// ValuationBased bids exactly at the agent's configured valuation per unit.
// This is the dominant strategy for a uniform-price (second-price) auction:
// the agent wins whenever clearing_price ≤ valuation, pays the clearing price,
// and earns positive utility. There is no benefit to bidding above or below
// valuation in this mechanism.
type ValuationBased struct{}

func (s ValuationBased) ComputeBid(ctx BidContext) (units uint64, price uint64, skip bool) {
    if ctx.Metrics.ThroughputKbps <= 0 && ctx.Metrics.DropKbps <= 0 {
        return 0, 0, true
    }
    demand := ctx.Metrics.ThroughputKbps + ctx.Metrics.DropKbps
    if demand < 1 {
        demand = 1
    }
    units = uint64(demand * 1.05)
    // Bid exactly at valuation — never above, never below.
    price = uint64(ctx.ValuationPerUnit)
    if price < uint64(ctx.Scene.ReservationPrice) {
        price = uint64(ctx.Scene.ReservationPrice)
    }
    return units, price, false
}
```

Also create `agent/strategy/valuation_based_test.go` with cases for:
- Normal flow: bid equals valuation
- Zero demand: skip
- Valuation below reservation price: floor is applied

### Step 3.2 — `throughput_optimizer` strategy

Create `agent/strategy/throughput_optimizer.go`.

This strategy bids aggressively (at `ValuationPerUnit`) when the market is cheap AND demand is high, and conservatively otherwise. It requires two configurable thresholds passed via `StrategyParams` in the scenario YAML:

| Param | Default | Meaning |
|-------|---------|---------|
| `price_threshold` | `"0.8"` | Fraction below the expected price to consider "cheap" |
| `high_demand_kbps` | `"80"` | Minimum throughput (kbps) to consider "high demand" |

**Bid logic (pseudocode):**
```
demand = throughput + drops
expectedPrice = rolling average of last N clearing prices (maintained in struct state)
priceIsCheap  = lastClearingPrice < expectedPrice * priceThreshold
demandIsHigh  = demand >= highDemandKbps

if demandIsHigh AND priceIsCheap:
    price = valuation_per_unit   // best conditions: bid full valuation
    units = demand * 1.2
elif demandIsHigh AND NOT priceIsCheap:
    price = lastClearingPrice    // must buy; follow market
    units = demand * 1.05
elif NOT demandIsHigh AND priceIsCheap:
    price = lastClearingPrice * 0.9  // opportunistic; buy cheaply
    units = demand * 0.8
else:
    price = reservation_price    // expensive and low demand; conserve budget
    units = max(1, demand * 0.5)

// Budget decay factor — scale price down as balance depletes
if credits.StartingBalance > 0:
    remaining = (credits.StartingBalance - credits.TotalSpent) / credits.StartingBalance
    price = price * remaining  // preserve budget for later rounds
```

The `throughput_optimizer` struct must maintain a small price history slice to compute the rolling average, similar to how `exploratory` maintains EMA state. Make it stateful (pointer receiver, initialised in a `NewThroughputOptimizer(params)` constructor).

The window size defaults to **3** and is configurable via `strategy_params` key `price_window`. Three clears fast enough to react to market shifts without being as noisy as window=1 (which would degrade to just following the last clearing price, equivalent to `conservative`):

```yaml
strategy_params:
  price_window: "3"       # configurable; default 3
  price_threshold: "0.8"
  high_demand_kbps: "80"
```

Create corresponding `_test.go` with cases for each of the four bidding conditions.

### Step 3.3 — Revise Q-learning reward to utility

In `agent/strategy/qlearning.go`, the current reward is the reduction in drop rate:
```go
reward = previousDropRate - currentDropRate
```

Replace this with utility-aligned reward:
```go
reward = float64(ctx.ValuationPerUnit - ctx.LastClearingPrice) * float64(allocatedUnits)
```

**Problem:** the Q-learning agent doesn't currently know `allocatedUnits` at reward time. Two options:
- Option A (simpler): proxy with drop-rate improvement: `reward = (valuation - clearingPrice) * (1 - dropRate)`. Higher reward when clearing price is low and drops are eliminated.
- Option B (accurate): store the most recent allocation from the `/auctions` endpoint and use it as `allocatedUnits` next round.

Prefer **Option B** — pass the most recent allocation into `BidContext` alongside `LastClearingPrice`. The agent already calls `fetchLastClearingPrice` which reads `/auctions`; extend it to also return the caller's allocated units from the most recent record.

Add `LastAllocatedUnits uint64` to `BidContext`. Update `agent/main.go` to populate it.

### Step 3.4 — Register new strategies in the selector

In `agent/main.go` `selectStrategy()`, add:
```go
case "valuation_based":
    return strategy.ValuationBased{}, nil
case "throughput_optimizer":
    return strategy.NewThroughputOptimizer(params), nil
```

**Verification:** `go test ./agent/...` passes for all new and revised strategy files.

---

## Phase 4: Utility Metrics

**Goal:** Track and expose per-customer utility (value gained minus cost paid per round) via Prometheus so it appears in Grafana and in exported experiment data.

### Step 4.1 — Compute utility in the auction runner

After clearing, the auction runner already knows:
- `clearingPrice` per egress port
- `allocatedUnits` per customer (from `alloc`)
- The scenario has `ValuationPerUnit` per customer

In `auction/runner/runner.go`, after billing credits, compute:

```go
utility := (customer.ValuationPerUnit - int(clearingPrice)) * int(allocatedUnits)
```

Store this as a **running total** in the existing `credits-map` or a parallel `utility-map` Atomix map, using the same atomic-add pattern as `ixp_customer_credits_spent_total`. Do not store per-round entries — Prometheus derives per-round values from the counter delta.

### Step 4.2 — Expose utility via the API metrics endpoint

In `api/server.go` `refreshMetrics()`, read the running total from the map and set one new Prometheus counter gauge per customer:

```
ixp_agent_utility_total  {customer_id, egress_port}  — cumulative utility across all rounds
```

Per-round utility is derived in Grafana using `increase(ixp_agent_utility_total[30s])`, where `30s` matches the auction interval — the same pattern already used for credits. No additional Atomix storage is needed.

This is consistent with how `ixp_customer_credits_spent_total` works and keeps Atomix storage bounded regardless of experiment duration.

### Step 4.3 — Update export-metrics

In the Makefile `export-metrics` target, add two new `curl` blocks to capture the utility time series alongside the existing metrics (clearing price, allocation, drop rate, throughput).

### Step 4.4 — Update Grafana dashboard

Add to the **existing** `monitoring/ixp-flows.json` dashboard (not a new file). Add a new row labelled "Agent Utility" with two panels:
- **Utility per round** — line chart, query: `increase(ixp_agent_utility_total[30s])`, one series per `customer_id`
- **Cumulative utility** — line chart, query: `ixp_agent_utility_total`, one series per `customer_id`

---

## Phase 5: Makefile Redesign — `make all` and `make all experiment=X`

**Goal:**
- `make all` deploys only the core control-plane services (API, auction runner, telemetry, Kafka, Atomix, monitoring). No dummy switch, no agents.
- `make all experiment=2a` additionally loads the experiment scenario, deploys the dummy switch (traffic simulator), and dynamically deploys one agent pod per customer defined in the experiment YAML.

### Step 5.1 — Write the agent deployment generator

Create `scripts/gen-agent-deployments/main.go`. This is a small Go program that:
1. Takes the path to a scenario YAML file as its first argument.
2. Loads it using `shared/scenario.Load(path)` (same code as the agent itself).
3. For each customer in `scenario.Customers`, prints a Kubernetes `Deployment` manifest to stdout.

The generated Deployment mirrors `agent/deployment.yaml` but with `CUSTOMER_ID` set to the customer's ID and the name `customer-agent-<customerID>`.

```go
// Usage: go run ./scripts/gen-agent-deployments /etc/scenario/scenario.yaml | kubectl apply -f -
```

Using the existing scenario Go package avoids any shell YAML parsing and reuses the same validation logic. The output is piped directly to `kubectl apply -f -` in the Makefile.

### Step 5.2 — Add a `delete-agents` target

Before deploying new agents for an experiment, old agent pods should be cleaned up:

```makefile
delete-agents:
    kubectl delete deployment -l app=customer-agent --ignore-not-found
```

### Step 5.3 — Refactor `services` and `all` targets

```makefile
# Core services only — no dummy, no agents.
services: vendor deploy-api deploy-auction deploy-telemetry

# Default: deploy everything except experiment traffic + agents.
# Think of this as the "production" control plane.
all: infra services

# Experiment deployment: loads scenario, starts dummy (traffic simulator),
# and deploys one agent per customer defined in the scenario YAML.
experiment ?=
ifdef experiment
EXPERIMENT_SCENARIO = etc/scenario/experiment-$(experiment).yaml
all: infra services load-experiment
endif
```

### Step 5.4 — Add `load-experiment` target

```makefile
load-experiment:
    @test -f $(EXPERIMENT_SCENARIO) || (echo "Unknown experiment: $(experiment)"; exit 1)
    @echo "==> Loading experiment scenario: $(EXPERIMENT_SCENARIO)"
    kubectl create configmap test-scenario \
        --from-file=scenario.yaml=$(EXPERIMENT_SCENARIO) \
        -o yaml --dry-run=client | kubectl apply -f -
    $(MAKE) deploy-dummy
    kubectl rollout restart deployment/auction-runner
    kubectl rollout restart deployment/telemetry-service
    kubectl rollout restart deployment/api-gateway
    kubectl rollout status deployment/auction-runner --timeout=90s
    kubectl rollout status deployment/api-gateway --timeout=90s
    $(MAKE) delete-agents
    go run ./scripts/gen-agent-deployments $(EXPERIMENT_SCENARIO) | kubectl apply -f -
    @echo "==> Experiment $(experiment) is live."
```

### Step 5.5 — Keep individual `deploy-experiment-*` targets as aliases

For backward compatibility and convenience, keep the existing `deploy-experiment-2a` etc. targets but rewrite them to call `load-experiment`:

```makefile
deploy-experiment-2a:
    $(MAKE) load-experiment experiment=2a
```

### Step 5.6 — Remove `deploy-agent` from the default path

The old `deploy-agent` target (which applied the hardcoded `agent/deployment.yaml`) should be demoted to a manual-only helper and removed from the `services` and `all` targets. Document it as "for manual single-agent testing only".

**Verification:** `make all` brings up the control plane with no agent or dummy pods. `make all experiment=1` additionally starts dummy + two agent pods. Changing to `make all experiment=3` stops old agents, starts new ones.

---

## Phase 6: New Experiment YAML Files

**Goal:** Add all new experiment files from the revised experiment set, and add `valuation_per_unit` to all existing files.

### Step 6.1 — Add `valuation_per_unit` to existing files

Already covered by Phase 2.5. Cross-check all 10 existing YAML files have the field.

### Step 6.2 — `experiment-4c-throughput-optimizer.yaml`

Duplicate `experiment-4b-budget-aware.yaml`. Change `as67890`'s strategy from `budget_aware` to `throughput_optimizer`. Add appropriate `strategy_params` for `price_threshold` and `high_demand_kbps`. Add a traffic pattern with distinct low and high phases to make the optimizer's intertemporal reasoning observable.

### Step 6.3 — `experiment-7-valuation-dominant-strategy.yaml` and `experiment-7b-qlearning-convergence.yaml`

**Experiment 7 goal:** answer — *does price-following (`conservative`) lose allocation in rounds where a truthful bidder (`valuation_based`) would win?*

The observable mechanism: after a traffic spike, the clearing price rises above `conservative`'s last known clearing price. `conservative` under-bids and receives **zero allocation** for that round. `valuation_based` bids at `valuation_per_unit` (above the spike) and wins. The utility gap across those rounds is the measurable cost of not bidding truthfully. This directly validates the dominant strategy theorem for second-price auctions.

Keep the scenario as a **clean two-agent comparison** — three agents on one graph complicate interpretation because each additional agent shifts the clearing price for the others.

**`experiment-7-valuation-dominant-strategy.yaml`:**
- `as12345`: strategy `conservative`, `valuation_per_unit: 500`
- `as67890`: strategy `valuation_based`, `valuation_per_unit: 500`
- Same traffic, capacity just below combined demand (forcing rationing so the two agents genuinely compete)
- Include a spike (reuse the spike pattern from experiment-2a) so `conservative` demonstrably under-bids
- Run for 25+ intervals; `auction_interval: 30s`

**`experiment-7b-qlearning-convergence.yaml`** (Q-learning convergence variant):
- `as12345`: strategy `q_learning`, `valuation_per_unit: 500`
- `as67890`: strategy `valuation_based`, `valuation_per_unit: 500`
- Same scenario structure; run for **50+ intervals** to allow the Q-table to converge
- Key graph: `increase(ixp_agent_utility_total[30s])` for `as12345` should trend upward over time toward the `as67890` baseline as the Q-learner discovers that higher bid multipliers produce more utility
- If the RL reward is utility-aligned (Step 3.3), the Q-learner should converge toward bidding near `valuation_per_unit` — effectively discovering the dominant strategy through experience

### Step 6.4 — `experiment-8-mixed-valuations.yaml`

Two customers, same `valuation_based` strategy, deliberately different valuations.
- `as12345`: `valuation_per_unit: 250` (low-value)
- `as67890`: `valuation_per_unit: 1000` (high-value)
- Capacity set so only one customer can win a majority in contested rounds
- Run for 20+ intervals; observe allocation split and clearing price convergence

### Step 6.5 — `experiment-9-ema-negative-result.yaml`

Two customers, competitive scenario.
- `as12345`: strategy `exploratory` (EMA), `valuation_per_unit: 500`
- `as67890`: strategy `valuation_based`, `valuation_per_unit: 500`
- Both have identical traffic; capacity just below combined demand
- Run for 20+ intervals; observe allocation and utility gap

### Step 6.6 — Update Makefile experiment targets

Add `deploy-experiment-4c`, `deploy-experiment-7`, `deploy-experiment-7b`, `deploy-experiment-8`, `deploy-experiment-9` aliases. Remove `deploy-experiment-5` (convergence removed; absorbed into experiment 1).

Update the experiment table comment block in the Makefile to match the revised experiment list.

---

## Phase 7: Documentation

**Goal:** Produce complete, accurate documentation suitable for submission alongside the thesis.

### Step 7.1 — Rewrite `README.md`

The README is the entry point for anyone picking up the project. Structure:

```
1. What this project is (2–3 sentences + scope boundary re: switch project)
2. Prerequisites and setup (make setup)
3. Quick start — core control plane only (make all)
4. Running an experiment (make all experiment=2a)
5. API reference (existing table, updated)
6. Prometheus metrics (existing table, add utility metrics)
7. Agent strategies (updated table with all 8 strategies, mark exploratory deprecated)
8. Experiment reference (updated table with all 9 experiments, remove exp 5)
9. Exporting data (make export-metrics)
10. External Kafka (existing section)
11. References
```

Keep it scannable — no prose paragraphs longer than 3 sentences. Code blocks for every command.

### Step 7.2 — `docs/ARCHITECTURE.md`

One comprehensive architecture document covering:
- System overview with ASCII or Mermaid diagram showing components and data flows
- Each component's responsibility (API, auction runner, telemetry, dummy, agent)
- State management: what lives in Atomix, what flows through Kafka, what is stateless
- The scope boundary: this project = control plane; switch enforcement = separate project (Kafka as the interface)
- Key design decisions table (D1–D12 from FEEDBACK-BRAINSTORM.md)

### Step 7.3 — `docs/AGENTS.md`

One document covering all agent strategies:
- Why uniform-price auctions have a dominant strategy (brief theory, 1–2 paragraphs)
- Why EMA/price-following is suboptimal in this mechanism (cite Vickrey; reference Experiment 9)
- For each strategy: motivation, config parameters (with defaults), pseudocode, which experiments use it
- The universal `valuation_per_unit` parameter: what it means, how to set it, how utility is calculated
- The `BidContext` struct: what information is available to every strategy

### Step 7.4 — `docs/EXPERIMENTS.md`

Complete experiment runner guide:
- Prerequisites (cluster up, `make all` run successfully)
- How to reset Atomix state between experiments (existing reset steps, clearly documented)
- For each experiment (1–9): goal, which agents, scenario file, how to run, what to watch in Grafana, how long to wait, how to export
- How to interpret exported data (`data/experiment-*.json` structure)
- The `make all experiment=X` shortcut explained

### Step 7.5 — `docs/DEPLOYMENT.md`

Operations guide:
- Local development with Minikube (full walkthrough)
- External Kafka (existing section from README, expanded)
- Atomix: how to inspect state, how to reset, single-replica for dev
- Scaling considerations (sharding by egress port for multi-switch)
- Troubleshooting common issues (Atomix not ready, Kafka topic missing, agent crash loop)

---

## Phase 8: Testing

### Step 8.1 — Unit tests for new strategies

- `agent/strategy/valuation_based_test.go`: 3 test cases (normal: bid equals `valuation_per_unit`; zero demand: skip; valuation below reservation price: floor is applied)
- `agent/strategy/throughput_optimizer_test.go`: 4 test cases (one per bidding condition: high demand + cheap, high demand + expensive, low demand + cheap, low demand + expensive)
- `agent/strategy/qlearning_test.go`: update existing tests to reflect the new reward function

### Step 8.2 — Scenario validation test

In `shared/scenario/load_test.go`, add a test that:
- Loads a scenario without `valuation_per_unit` set and confirms it defaults to `10 × reservation_price`
- Loads a scenario with it set and confirms the value is preserved

### Step 8.3 — gen-agent-deployments smoke test

In `scripts/gen-agent-deployments/`, add a test that loads a known scenario file and verifies the output contains one Deployment per customer and the correct `CUSTOMER_ID` env var.

### Step 8.4 — End-to-end smoke test

Run `make all` and verify no agent or dummy pods are created. Then run `make all experiment=1` and verify exactly two agent pods (`customer-agent-as12345`, `customer-agent-as67890`) and one dummy pod come up within 90 seconds.

---

## Implementation Order Summary

```
Phase 1 (cleanup)
    └─▶ Phase 2 (valuation_per_unit in schema, BidContext, validation warning)
            ├─▶ Phase 3 (new strategies, q-learning revision)
            │       └─▶ Phase 4 (utility metrics)
            └─▶ Phase 5 (Makefile redesign + gen-agent-deployments)
                    └─▶ Phase 6 (new experiment YAMLs)
                            └─▶ Phase 7 (documentation)
                                    └─▶ Phase 8 (tests, smoke test)
```

Phase 4 can run in parallel with Phase 3 if needed. Phase 6 can be started as soon as Phase 2 is done (only needs the schema change, not the new strategies).

