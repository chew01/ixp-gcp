# Agent Strategies

## Second-Price Auctions and the Dominant Strategy

This system implements a **uniform-price auction** (a multi-unit analogue of the Vickrey second-price auction). In each round, all accepted bids pay the same clearing price — the price at which supply meets demand. The winner never pays their own bid; they pay the price set by the marginal bidder.

The **dominant strategy** in a second-price auction is to bid your true valuation. Formally: bidding `valuation_per_unit` weakly dominates every other strategy, regardless of what other agents do. Bidding higher than valuation risks winning rounds where you pay more than the traffic is worth (negative utility). Bidding lower than valuation risks losing rounds you would have won at a profit.

**Why EMA price-following fails:** strategies like `exploratory` track the clearing price using an exponential moving average and bid EMA + epsilon. This conflates "price I will pay" with "optimal bid". After a traffic spike, the clearing price jumps; the EMA lags, and the agent under-bids and loses allocation it would have valued positively. The correct response is to bid true valuation and let the auction mechanism determine the price. See Experiment 9 for the empirical demonstration.

---

## Universal Parameter: `valuation_per_unit`

Every customer in the scenario YAML can set `valuation_per_unit`: the maximum credits per kbps unit they are willing to pay. This field serves two roles:

| Role | Applies to | Mechanism |
|------|-----------|-----------|
| Bid price | `valuation_based` only | Strategy bids exactly `valuation_per_unit` |
| Utility accounting | Every agent | `utility = (valuation_per_unit − clearing_price) × allocated_units` |

**Default:** if unset, `valuation_per_unit` defaults to `10 × reservation_price` at load time, preserving backward compatibility with scenario YAMLs that predate this field.

**Warning:** if `valuation_per_unit < reservation_price`, utility will be negative every round (cleared price always ≥ reservation price). The system logs a warning but does not reject the scenario — deliberately low-valuation experiments (Experiment 8) are valid.

---

## The `BidContext` Struct

Every strategy receives a `BidContext` on each `ComputeBid` call:

```go
type BidContext struct {
    Scene              *scenario.Scenario  // full scenario (switches, reservation_price, etc.)
    CustomerID         string
    SwitchID           string
    IngressPort        uint32
    EgressPort         uint32
    Metrics            shared.FlowMetricsValue  // throughput_kbps, drop_kbps, egress_kbps, drop_rate_pct
    Credits            shared.CustomerCredits   // total_spent, starting_balance
    LastClearingPrice  int    // clearing price from the most recent auction round
    ValuationPerUnit   int    // from scenario; used by valuation_based and utility calc
    LastAllocatedUnits uint64 // units allocated in the most recent round (for Q-learning reward)
}
```

`ComputeBid` returns `(units uint64, price uint64, skip bool)`. Returning `skip=true` means no bid is submitted for this flow this round.

---

## Strategy Reference

### `conservative`

The simplest strategy: bid a fixed 10% above current throughput at the last known clearing price.

- **Units:** `ceil(throughput_kbps × 1.1)`, minimum 1
- **Price:** `max(reservation_price, last_clearing_price)`
- **Skip:** if `throughput_kbps == 0` and `drop_kbps == 0`
- **Weakness:** ignores dropped traffic in the unit estimate — never bids for the bandwidth it is losing to congestion. Recovers slowly after spikes.
- **Experiments:** 1, 2a, 2b (as12345), 3 (as12345), 4a, 5, 6a/6b/6c (comparison)

---

### `demand_corrected`

Like `conservative` but bids based on effective demand (throughput + drops).

- **Units:** `ceil((throughput_kbps + drop_kbps) × 1.05)`, minimum 1
- **Price:** `max(reservation_price, last_clearing_price)`
- **Skip:** if both are zero
- **Strength:** accounts for dropped traffic — bids more units during congestion and recovers faster.
- **Experiments:** 2b (as67890), 6a, 6b, 6c

---

### `price_insensitive`

Bids a fixed multiple of the reservation price regardless of market conditions. Models latency-critical traffic that must guarantee bandwidth at any price.

- **Units:** `ceil((throughput + drops) × 1.05)`, minimum 1
- **Price:** `reservation_price × price_multiplier`
- **Params:** `price_multiplier` (default `"10"`)
- **Strength:** always outbids price-following strategies; wins full allocation in any contested round.
- **Experiments:** 3 (as67890)

---

### `budget_aware`

Scales bid price down through tiers as the credit balance depletes. Falls back to `conservative` when no `starting_balance` is configured.

- **Units:** `ceil((throughput + drops) × 1.05)`, minimum 1
- **Price tiers** (based on `remaining / starting_balance`):
  - `> 75%`: EMA + epsilon (spending freely)
  - `> 50%`: `0.75 × EMA`
  - `> 25%`: `0.50 × EMA`
  - `≤ 25%`: `reservation_price`
- **Params:** `ema_alpha` (default `"0.3"`), `budget_epsilon` (default `"5"`)
- **Experiments:** 4b, default `scenario.yaml`

---

### `valuation_based`

The dominant strategy for this auction mechanism. Bids exactly at the customer's true valuation.

- **Units:** `ceil((throughput + drops) × 1.05)`, minimum 1
- **Price:** `max(valuation_per_unit, reservation_price)`
- **Skip:** if both metrics are zero
- **Theory:** wins whenever `clearing_price ≤ valuation_per_unit`; earns `(valuation − clearing) × units` utility per round. No incentive to deviate.
- **Experiments:** 7 (as67890), 7b (as67890), 8 (both), 9 (as67890)

---

### `throughput_optimizer`

An intertemporal strategy that bids aggressively when conditions are favourable (cheap + high demand) and conserves credits otherwise.

- **Bidding conditions:**

| Market | Demand | Price | Units |
|--------|--------|-------|-------|
| Cheap (< expectedPrice × threshold) | High (≥ high_demand_kbps) | `valuation_per_unit` | `demand × 1.2` |
| Expensive | High | `last_clearing_price` | `demand × 1.05` |
| Cheap | Low | `last_clearing_price × 0.9` | `demand × 0.8` |
| Expensive | Low | `reservation_price` | `max(1, demand × 0.5)` |

- **Budget decay:** price is scaled by `remaining_balance / starting_balance` to preserve credits for later rounds.
- **Params:** `price_threshold` (default `"0.8"`), `high_demand_kbps` (default `"80"`), `price_window` (default `"3"`)
- `expectedPrice` = rolling average of the last `price_window` clearing prices (computed from prior history — the current round's price is not included until after bidding).
- **Experiments:** 4c (as67890)

---

### `q_learning`

Tabular Q-learning that learns a bid multiplier over 16 states.

- **State:** `(drop_bucket, budget_bucket)` — 4 × 4 = 16 states
  - `drop_bucket`: `< 1%` → 0, `< 10%` → 1, `< 30%` → 2, `≥ 30%` → 3
  - `budget_bucket`: `> 75%` → 0, `> 50%` → 1, `> 25%` → 2, `≤ 25%` → 3
- **Actions:** price multiplier applied to `max(last_clearing, reservation_price)`: `[0.8×, 1.0×, 1.25×, 1.5×, 2.0×, 3.0×]`
- **Reward:** `(valuation_per_unit − last_clearing_price) × last_allocated_units` — utility earned in the previous round
- **Update:** standard Q-learning: `Q[s,a] ← Q[s,a] + α(r + γ·max_Q(s') − Q[s,a])`
- **Action selection:** epsilon-greedy
- **Params:** `ql_alpha` (default `"0.1"`), `ql_gamma` (default `"0.9"`), `ql_epsilon` (default `"0.15"`)
- **Convergence hypothesis:** with enough rounds the agent should discover that multipliers producing bids near `valuation_per_unit` yield the highest utility. See Experiment 7b.
- **Experiments:** 7b (as12345)

---

### `exploratory` *(deprecated)*

> **Deprecated.** Retained solely for Experiment 9 (negative result). Do not use in new experiments.

EMA-based price-following strategy.

- **Units:** `ceil((throughput + drops) × 1.05)`, minimum 1
- **Price:** `floor(EMA) + epsilon`, floored at `reservation_price`
  - EMA initialised to `last_clearing_price` on first observation
- **Params:** `ema_alpha` (default `"0.3"`), `ema_epsilon` (default `"5"`)
- **Structural flaw:** EMA tracks past clearing prices; after a clearing-price spike the EMA lags and the agent under-bids. The correct bid in a second-price auction is `valuation_per_unit`, not the smoothed historical price. See §Second-Price Auctions above.
- **Experiments:** 9 (as12345)
