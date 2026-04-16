# Professor Feedback — Round 2: Agents, Evaluation, and Report Design

This document captures and expands on the second round of supervisor feedback. It covers agent design revisions, evaluation methodology, ML/RL considerations, proposed experiment additions, a project title brainstorm, and a draft report structure.

---

## 1. Removing EMA-Based Strategies

### Professor's Point
EMA-based strategies do not work well in a **second-price (uniform-price) auction**.

### Why This is Correct

In a **uniform-price sealed-bid auction**, every winner pays the same clearing price — the price of the marginal (lowest-accepted) bid. The dominant strategy for a rational agent is therefore to **bid your true valuation**: there is no strategic benefit to shading your bid below valuation, and bidding above it only risks paying more than you value the good.

EMA strategies (specifically the `exploratory` agent) track the clearing price history and submit bids at `EMA(clearing_prices) + ε`. This pattern makes sense in a **pay-as-bid (discriminatory-price) auction** where you want to bid just enough to win without overpaying. In a uniform-price auction:

- If your EMA underestimates the true clearing price, you lose the auction and get no allocation.
- If your EMA overestimates, you win but pay the same clearing price you would have paid anyway — the EMA tracking bought you nothing.
- The strategy introduces unnecessary complexity and noise without improving utility.

The `backoff` strategy also implicitly tracks price history to adjust its multiplier. While it has some behavioural justification (cooling an overheated market), the price-following logic is still misaligned with the dominant strategy for this mechanism.

### Recommendation

| Strategy | Keep? | Rationale |
|----------|-------|-----------|
| `conservative` | Keep | Useful baseline; simple and honest |
| `demand_corrected` | Keep | Corrects the bid *quantity* formula — orthogonal to price mechanism |
| `price_insensitive` | Keep | Models a high-valuation agent; well-motivated |
| `budget_aware` | Keep | Models budget-constrained behaviour; distinct from price-tracking |
| `exploratory` (EMA) | **Remove** | Price-tracking is misaligned with uniform-price auctions |
| `backoff` | **Remove or demote** | Also based on price history; not strategy-theoretically grounded here |
| `q_learning` | **Revise** | Q-learning itself is valid; redesign state/reward around utility, not raw clearing price |

**In the report:** dedicate a short paragraph explaining why EMA/backoff strategies are not appropriate for this mechanism. This is actually a *positive* finding — it demonstrates understanding of mechanism design theory. Cite Vickrey's result or Milgrom's "Putting Auction Theory to Work" for the claim that dominant strategies in second-price auctions are truthful.

---

## 2. Valuation-Based Agent

### Professor's Point
Add an agent that models **valuation per unit** — reflecting that real providers may serve higher-value customers (e.g. latency-sensitive traffic, premium peering). The agent bids up to its valuation, but **never above it**.

### Design

This agent is the *theoretically correct* agent for a uniform-price auction. Define:

```
valuation_per_unit: a fixed (or traffic-type-dependent) willingness to pay per kbps
```

**Bidding rule:**

```
bid_price  = min(valuation_per_unit, ...)   // never exceed valuation
bid_units  = effective_demand               // same demand formula as demand_corrected
```

The agent always bids its true valuation. It wins whenever `clearing_price ≤ valuation_per_unit` and loses otherwise. This is **truthful bidding** — the theoretically dominant strategy.

### Variations to Consider

| Variant | Description | Motivation |
|---------|-------------|-----------|
| **Fixed valuation** | `valuation_per_unit` is a constant from config (e.g. `10`) | Simplest; establishes a baseline "premium" agent |
| **Traffic-class valuation** | Different valuations for different egress ports or traffic types | Reflects real peering: latency-sensitive, bulk, best-effort |
| **Dynamic valuation** | Valuation decreases when drop rate is 0 (bandwidth not scarce); increases when drops occur | Models an agent that places higher value on guaranteed capacity under congestion |
| **Threshold bidding** | Agent only bids when `last_clearing_price < valuation_per_unit`; abstains otherwise | Hard budget discipline — does not bid into markets it cannot profitably participate in |

### Interaction with Other Agents

A key experiment is mixing this agent with others. Because it bids truthfully at valuation:
- Against `conservative` (which bids at clearing price): the valuation agent sets the clearing price whenever it competes.
- Against `price_insensitive` (which bids a fixed high multiple): if `price_insensitive` valuation > this agent's valuation, the latter loses — an informative outcome.
- Against `budget_aware`: the budget agent may outbid during high-budget phases but under-bid as balance depletes.

### Configuration

Add to agent config YAML or env:

```yaml
strategy: valuation_based
valuation_per_unit: 15   # willingness to pay per kbps, in credits
```

### Feasibility

High. This is a simpler strategy than Q-learning — it removes the EMA logic and replaces it with a single threshold check. The `BiddingStrategy` interface already exists.

---

## 3. Evaluating Agents Through Utility

### Professor's Point
Evaluate each agent by computing **utility**: gain per unit minus cost per unit.

### Utility Definition

Standard mechanism design definition applied to this system:

```
utility = Σ_t [ (valuation_per_unit × allocated_units_t) − (clearing_price_t × allocated_units_t) ]
        = Σ_t [ (valuation_per_unit − clearing_price_t) × allocated_units_t ]
```

When `valuation_per_unit` is not explicitly configured (e.g. for agents like `conservative`), we can proxy it using the throughput demand: the agent's implicit valuation is the bandwidth it actually needed. Define:

```
effective_valuation_t = max(throughput_kbps_t + drop_kbps_t, allocated_units_t)
utility_t = (effective_valuation_t − clearing_price_t) × allocated_units_t
```

### Metrics to Export

Add to the Prometheus/export pipeline:

| Metric | Definition | Notes |
|--------|-----------|-------|
| `ixp_agent_utility_total` | Cumulative utility over all intervals | Primary evaluation metric |
| `ixp_agent_utility_per_interval` | Per-round utility | Shows temporal dynamics |
| `ixp_agent_surplus_per_unit` | `(valuation - clearing_price)` per round | Measures how "cheaply" the agent wins |
| `ixp_agent_win_rate` | Fraction of rounds where agent received allocation | Bidding success rate |
| `ixp_agent_budget_efficiency` | `utility / credits_spent` | Credits-to-value ratio |

### Evaluation Dimensions

When comparing agent strategies in the report, evaluate along these axes:

| Dimension | Question |
|-----------|---------|
| **Total utility** | Which agent extracts the most value from the auction over the experiment duration? |
| **Utility stability** | Which agent has the most consistent per-round utility (low variance)? |
| **Budget efficiency** | Which agent gets the most utility per credit spent? |
| **Recovery speed** | How quickly does utility recover after a traffic spike? |
| **Market impact** | Does the agent's strategy increase or decrease the clearing price for others? |

### Caveats and Reporting

In the report, note that utility measurement is complicated by the fact that `valuation_per_unit` is a **model parameter**, not an observable real-world quantity. For agents without an explicit valuation, we use throughput demand as a proxy. This is a limitation to acknowledge in the evaluation section.

---

## 4. Budget Agent: Maximise Throughput Over Time

### Professor's Point
Design an agent whose objective is to **maximise total throughput over a period given a fixed budget**. The agent should bid aggressively when prices are low and throughput opportunity is high, and conservatively otherwise.

### This Extends the Existing `budget_aware` Strategy

The existing `budget_aware` agent simply scales bid price down as balance depletes — a reactive, risk-averse strategy. The professor's vision is more **forward-looking**: the agent reasons about intertemporal trade-offs.

### Proposed: `throughput_optimizer` Agent

**Objective:** Maximise `Σ_t allocated_units_t` subject to `Σ_t (allocated_units_t × clearing_price_t) ≤ Budget`.

**Core intuition:**
- When the market is cheap (clearing price low), buy a lot of bandwidth — good value.
- When the market is expensive, buy less — preserve budget for cheaper future rounds.
- When throughput demand is high, always bid (dropping traffic has a cost).
- When throughput demand is low, be conservative regardless of price.

**Bidding rule sketch:**

```
expected_price   = rolling_avg(last_N_clearing_prices)
price_is_cheap   = last_clearing_price < expected_price * price_threshold  // e.g. 0.8
throughput_high  = current_throughput > high_throughput_threshold          // e.g. 80% of capacity

if throughput_high AND price_is_cheap:
    bid_price  = valuation_per_unit                     // bid aggressively; market is a bargain
    bid_units  = effective_demand * 1.2                 // buy extra headroom
elif throughput_high AND NOT price_is_cheap:
    bid_price  = last_clearing_price                    // must buy; traffic is high
    bid_units  = effective_demand
elif NOT throughput_high AND price_is_cheap:
    bid_price  = last_clearing_price * 0.9              // opportunistic; buy cheaply for future
    bid_units  = effective_demand * 0.8
else:  // low demand, expensive market
    bid_price  = reservation_price                      // minimal bid; don't waste budget
    bid_units  = max(1, effective_demand * 0.5)
```

**Budget remaining factor:** As budget depletes, scale down the bid price proportionally (similar to `budget_aware`), so the agent never overspends in later rounds.

### Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `price_threshold` | 0.8 | Ratio below expected price to consider "cheap" |
| `high_throughput_threshold` | 0.7 | Fraction of observed capacity to consider "high demand" |
| `valuation_per_unit` | 10 | Max willingness to pay when conditions are ideal |
| `budget` | from scenario | Total credits available for the session |

### Relationship to Existing Strategies

This agent subsumes `budget_aware` (which only reacts to balance level) and adds the price-signal dimension. It is the most strategically sophisticated non-ML agent in the portfolio.

### Experiment Scenario

Run this agent alongside a `conservative` agent with the same starting budget and same traffic profile. Measure:
- Total allocated bandwidth over the session
- Total credits spent
- Number of rounds where allocation was lost entirely (bid too low)
- Utility as defined in Section 3

---

## 5. ML/RL Considerations

### Professor's Point
Consider using an ML or RL library.

### Current State

The `q_learning` agent implements tabular Q-learning manually in Go. Its state space is `(drop_bucket, budget_bucket)` and it learns bid multipliers. This is a solid proof-of-concept but has limitations:
- State space is hand-engineered and very small.
- The reward (reduction in drop rate) is not aligned with the utility metric.
- Training and deployment happen in the same process (no pre-training or transfer).

### Options

#### Option A: Improve Tabular Q-Learning (Low effort, Go)

Redesign the existing Q-learning agent with:
- Better state: `(price_bucket, throughput_bucket, budget_bucket)` — 3D state
- Better reward: `utility_t = (valuation - clearing_price) × allocated_units` — aligned with evaluation metric
- Longer epsilon-greedy exploration phase (first N rounds = exploration)

**Effort:** Low. Only `agent/strategies/q_learning.go` changes.

#### Option B: External RL Library via Python Sidecar (Medium effort)

Run a small Python process alongside the Go agent. The Python process:
1. Receives state observations (clearing price, throughput, drops, budget) via stdin/HTTP/gRPC.
2. Returns an action (bid_price, bid_units) using a library like `stable-baselines3` or `Ray RLlib`.
3. Receives reward after the next auction round.

This allows using DQN, PPO, or A2C without rewriting the Go infrastructure.

**Trade-off:** Adds operational complexity (two containers per agent). Good for a thesis that wants to discuss RL seriously; overkill if RL is a minor contribution.

#### Option C: Pre-trained Model / Offline RL (Simulation + Deploy)

Use the existing experiment data to train an RL agent offline, then deploy the trained policy as a lightweight lookup table or neural net in the Go agent.

**Steps:**
1. Collect state-action-reward trajectories from existing experiment runs (already exported to `data/experiment-*.json`).
2. Train an offline RL model (e.g. Conservative Q-Learning / BCQ) on these trajectories.
3. Export the policy (e.g. as a small JSON lookup table or ONNX model).
4. Load the policy in the Go agent at startup.

**Trade-off:** No need for online learning during deployment; the policy is fixed. Good if experiment data is rich enough; risk is that offline distribution doesn't cover all states.

#### Option D: Multi-Armed Bandit (Simple, No Library Needed)

Model the bid price choice as a multi-armed bandit:
- Arms = discrete price levels `[reservation, 1.5×, 2×, 3×, ...]`
- Reward = utility earned this round
- Policy = UCB or Thompson Sampling

This is simpler than Q-learning (no state, just a reward signal per arm) and can be implemented in ~50 lines of Go. It's a good alternative to EMA for learning "what bid price tends to work in this market".

### Recommendation for Thesis

For a bachelor thesis, the recommendation is:

1. **Redesign the Q-learning agent** (Option A) to use utility-aligned rewards — this fixes the current reward misalignment and makes the Q-learning section of the report coherent.
2. **Add a bandit agent** (Option D) as a lightweight learning baseline — simple to implement, easy to explain, and distinct from Q-learning.
3. **Mention Option B/C** as future work in the report — demonstrates awareness of production-grade RL without over-scoping the implementation.

If ML/RL is intended to be a significant contribution, Option B with `stable-baselines3` is the most impressive for a thesis, but requires more effort.

---

## 6. Project Title

The primary contribution of this project is the **system**: a working SDN-based rapid auction control plane for IXP bandwidth allocation. Agents are a built-in evaluation mechanism, not the primary subject. The title should reflect this — the system comes first, agents are secondary.

### Candidates

| # | Title | Balance | Notes |
|---|-------|---------|-------|
| A | **A Rapid Bandwidth Auction System for Internet Exchange Points** | System-first | Clean and precise; positions the system as the artefact |
| B | **Dynamic Bandwidth Allocation at Internet Exchange Points via Repeated Second-Price Auctions** | System-first | Emphasises mechanism; "repeated" captures the time dimension; long but complete |
| C | **From Contracts to Clearing Prices: An SDN-Based Bandwidth Auction System for Internet Exchange Points** | System-first | Narrative arc; contrasts old model (contracts) with new (clearing prices); memorable |
| D | **Auction-Driven Bandwidth Management at Internet Exchange Points: System Design and Evaluation** | System + evaluation | Signals both a built artefact and a rigorous evaluation |
| E | **Rapid IXP Bandwidth Auctions: System Design, Agent Strategies, and Empirical Evaluation** | Equal | Honest about all three contributions without over-weighting any one |
| F | **Bidding for Bandwidth: An Auction Control Plane for Dynamic IXP Peering** | System-first, catchy | "Bidding for Bandwidth" is memorable; "control plane" is technically precise |

**Preferred candidates:**
- **Option D** if the emphasis is "we built something and evaluated it rigorously" — straightforward, suitable for a technical thesis.
- **Option C** if there is room for a narrative hook — the contrast between multi-week bilateral contracts and sub-minute clearing prices is the central motivation, and making that visible in the title is compelling.
- **Option E** if the examiners expect the thesis to cover agents and evaluation as prominently as the system itself — transparent about all three parts.

---

## 6.5 Should `valuation_per_unit` Be Configured for Every Agent?

**Short answer: yes.**

In a uniform-price auction, every agent's rational upper bound on bid price is their valuation per unit. Making this an explicit universal config parameter has several advantages:

**1. Consistent utility calculation.** Utility is defined as `(valuation − clearing_price) × allocated_units`. If some agents have a configured valuation and others do not, you cannot produce a fair cross-agent utility comparison in the evaluation. With `valuation_per_unit` set for all agents, every experiment can compute and compare utility uniformly.

**2. Enforces rationality as a hard cap.** Regardless of strategy, no agent should ever bid above its valuation — that would produce negative utility. Adding a universal cap `bid_price = min(strategy_price, valuation_per_unit)` prevents any strategy from accidentally overbidding, even during edge cases (e.g. a clearing-price spike inflating the `price_insensitive` base multiplier).

**3. Cleaner experiment design.** Experiments can be structured in two orthogonal ways:
- *Same valuation, different strategy*: isolates the effect of strategy on utility and market outcome.
- *Different valuations, same strategy*: isolates the effect of valuation on allocation efficiency.
Without a universal valuation parameter, the second class of experiment is impossible to specify cleanly.

**4. `price_insensitive` already implicitly has a valuation.** It bids at `10 × reservation_price`. Making that explicit as `valuation_per_unit = 10` (or whatever is configured) is strictly cleaner and more general.

**Implementation note:** Add `valuation_per_unit` to the shared agent config struct (alongside existing params like `ema_alpha`, `ql_alpha`). Default it to a sensible multiple of `reservation_price` (e.g. `10×`) so existing scenarios still behave as before without explicit configuration. The `valuation_based` agent uses it as the *exact* bid price; all other agents use it as a *ceiling*.

---

## 7. Experiments — Revised and Pruned

The experiment set below consolidates the existing six experiments and new proposals. Experiments that show nothing new beyond another experiment, or whose outcome is obvious without measurement, are removed with justification.

### Existing Experiments — Keep / Remove Decisions

| Exp | Scenario | Keep? | Justification |
|-----|----------|-------|---------------|
| 1 — Baseline | conservative vs. conservative, light load | **Keep** | Sanity check: confirms the auction mechanism works correctly and drops reach zero. Essential reference point for all other experiments. Run long enough (20+ intervals) to also demonstrate convergence — this absorbs Experiment 5. |
| 2a — Conservative spike (symmetric) | Both conservative, traffic spike beyond capacity | **Keep** | Reveals the fundamental flaw of the conservative quantity formula: persistent drops because the agent never bids for dropped traffic. Motivates `demand_corrected`. |
| 2b — Conservative vs. demand_corrected spike | One of each, same spike | **Keep** | Directly compares the two quantity formulas under the same conditions. Allocation lines diverge visibly — easy to present and interpret. |
| 3 — Price-insensitive vs. conservative | One of each, symmetric traffic | **Keep, reframed** | In the second-price auction context, `price_insensitive` is effectively a *high-valuation truthful bidder*. This experiment empirically shows that a high-valuation agent wins a disproportionate share of allocation — consistent with theory. Reframe in the report as "effect of valuation heterogeneity on allocation" rather than just "price-insensitive agent". |
| 4a — Conservative with finite budget | Conservative only, finite balance | **Keep as sub-scenario of Exp 4** | On its own this experiment shows something obvious ("credits drain, agent loses allocation"). However, without it Experiment 4b has no comparison point. Fold it into one combined budget experiment: 4a as baseline, 4b as improvement. |
| 4b — Budget-aware with finite budget | Budget_aware, same scenario as 4a | **Keep** | Shows meaningful credit stretching vs. 4a. Clear, measurable difference in allocation duration. |
| 5 — Convergence | Symmetric conservative, long run | **Remove** | Absorbed by Experiment 1. Convergence is trivially observable in a long baseline run; running it as a separate experiment adds no new independent variable and no new finding. |
| 6a/6b/6c — Interval sensitivity | 10s / 30s / 60s | **Keep all three** | Three data points on a single axis (interval length) give a clean result curve: short interval → fast response, high credit overhead; long interval → slow response, cheaper. This is one of the strongest quantitative results in the thesis because it directly addresses the core claim (sub-minute responsiveness). |

---

### New Experiments

### Experiment 7: Valuation-Based Agent — Dominant Strategy Validation

**Goal:** Verify that an agent bidding truthfully at its configured valuation achieves the theoretically predicted outcome in a second-price auction: it wins whenever `clearing_price ≤ valuation`, maximising its utility, without any need for price-following.

| | |
|--|--|
| **Agents** | `valuation_based` vs. `conservative` |
| **Setup** | Same traffic; capacity contested; both agents given the same `valuation_per_unit`; conservative bids at clearing price (below valuation), valuation-based bids at valuation |
| **Metrics** | Utility per round, allocation share, clearing price, credits spent |
| **Hypothesis** | `valuation_based` achieves equal or higher allocation at equal or lower cost because it never loses rounds it should win (conservative may under-bid when clearing price spikes); both agents pay the same clearing price when they win |
| **Research value** | This is the core mechanism-design argument. Empirically confirms: in a second-price auction, following the market price is dominated by bidding your true valuation. |

---

### Experiment 8: Market Efficiency — Mixed Valuations

**Goal:** Test whether the auction allocates bandwidth to the highest-value participant when agents have different valuations — the standard efficiency claim for second-price auctions.

| | |
|--|--|
| **Agents** | `valuation_based` with low valuation (e.g. 5) vs. `valuation_based` with high valuation (e.g. 20); same strategy, different values |
| **Setup** | Capacity is deliberately below combined demand, forcing rationing |
| **Metrics** | Allocation split, clearing price per round, utility per agent |
| **Hypothesis** | High-valuation agent receives majority of allocation in contested rounds; clearing price settles near the low-valuation agent's value (the marginal bidder); low-valuation agent is rationed out without surplus loss |
| **Research value** | Demonstrates the efficiency property of the uniform-price mechanism. Directly citable against auction theory (Vickrey). Complements Experiment 3 (price_insensitive) by showing the same effect with *explicit* valuations rather than a fixed-price agent. |

**Note on relation to Experiment 3:** Experiment 3 (`price_insensitive` vs. `conservative`) also shows a high-value agent dominating, but the price_insensitive agent's valuation is implicit (a fixed multiplier). Experiment 8 makes this explicit and theoretically precise. Both are worth keeping: Exp 3 uses the existing simpler agent, Exp 8 is the theoretically clean version.

---

### Experiment 9 (formerly 10): Traffic Change → Control Loop Latency

**Goal:** Measure the end-to-end latency of the system's response to a sudden traffic change — from the moment demand spikes to the moment allocation increases.

| | |
|--|--|
| **Agents** | `demand_corrected` (best-performing reactive agent) |
| **Setup** | Traffic spike at a known timestamp; measure three timestamps: (1) spike occurs, (2) agent first observes non-zero `drop_kbps`, (3) new allocation takes effect |
| **Metrics** | `t_detect = (2) − (1)` ≈ telemetry interval; `t_react = (3) − (2)` ≈ auction interval; `t_total = t_detect + t_react` |
| **Independent variable** | Auction interval (10s, 30s, 60s) — reusing the same scenarios as Experiment 6 |
| **Hypothesis** | `t_total ≈ telemetry_interval + auction_interval`; the auction interval dominates; total lag is 11–61 seconds depending on configuration |
| **Research value** | Provides a concrete, measurable number to compare against traditional IXP contract timescales (days to weeks). Central to the thesis motivation. Also reveals whether telemetry lag or auction lag is the binding constraint — informing where to invest further optimisation. |

**Note:** This is distinct from Experiment 6. Experiment 6 measures *steady-state* efficiency (drop rate, credits) across different intervals. Experiment 9 measures *transient* step-response latency. They are complementary and share the same scenario files.

---

### Experiment 10: EMA as Negative Result

**Goal:** Empirically confirm that EMA-based price-following underperforms truthful bidding in a second-price auction, validating the theoretical argument for removing it.

| | |
|--|--|
| **Agents** | `exploratory` (EMA) vs. `valuation_based` |
| **Setup** | Same traffic; same `valuation_per_unit` configured for both; competitive scenario (capacity < demand) |
| **Metrics** | Allocation share, utility, number of rounds lost (zero allocation) |
| **Hypothesis** | EMA agent loses rounds when its EMA underestimates the true clearing price; `valuation_based` agent wins consistently; utility gap is measurable |
| **Research value** | Keeping this experiment is justified precisely because it is a **research finding, not just an engineering decision**. The theory predicts EMA doesn't work here; this experiment proves it empirically. A short section in the report: "We verified this prediction by running X against Y…". This is stronger than simply citing theory. |

**Note on keeping `exploratory` in the codebase:** The `exploratory` agent should remain in the repository (marked as deprecated/experimental) specifically to enable this experiment. The `backoff` agent offers no equivalent research value — its failure mode is not theoretically interesting — so it can be removed entirely.

---

## 8. Proposed Report Structure

Based on the provided template and project scope. The key principle: **keep switch-level / data-plane content brief** (one context section); focus depth on the auction mechanism, agent strategies, and experimental evaluation.

---

### Title
*(Selected from Section 6 — e.g. "Rapid Bandwidth Auctions at Internet Exchange Points: Agent Strategy Design and Evaluation")*

### Abstract
One-page summary: problem (slow IXP contracts), approach (SDN + repeated auction), agents designed, experiments run, key findings.

### Acknowledgements

### List of Figures

---

### 1. Introduction

**1.1 The Internet Exchange Point Ecosystem**
Brief: what an IXP is, how peering currently works (bilateral contracts, BGP, weeks of negotiation), and why this is inflexible for dynamic traffic.

**1.2 Motivation: Dynamic Bandwidth Allocation**
The gap between traffic dynamics (sub-second) and peering timescales (weeks). Why an auction-based control plane is a promising direction.

**1.3 Research Questions**
- Can a repeated second-price auction allocate IXP bandwidth efficiently at sub-minute intervals?
- Do agent strategies based on truthful valuation bidding outperform heuristic price-following strategies?
- How responsive is the auction control loop to traffic changes?

**1.4 Scope and Boundaries**
*Explicitly state:* Switch-level enforcement (flow tables, shaping) is handled in a separate project via Kafka. This project covers the control plane: auction mechanism, agent strategies, and evaluation. This boundary should be referenced when presenting the architecture.

**1.5 Contributions**
List: auction system implementation, agent strategy portfolio, utility-based evaluation framework, experiments.

**1.6 Report Organisation**
One paragraph describing each chapter.

---

### 2. Background and Related Work

**2.1 Internet Exchange Points: Architecture and Peering**
Physical and logical structure of an IXP; route servers; bilateral vs. multilateral peering.

**2.2 Software-Defined Networking at IXPs**
SDN control plane; existing IXP SDN projects (e.g. ONOS-based route servers, ARouteServer). Why SDN enables dynamic bandwidth control.

**2.3 Auction Mechanisms**
- Sealed-bid auctions: first-price, second-price (Vickrey), uniform-price
- Multi-unit auctions and their properties
- Why uniform-price is appropriate here (dominant strategy = truthful bidding; clearing price as market signal)
- Why EMA/price-following strategies are misaligned with uniform-price auctions

**2.4 Agent Strategies and Mechanism Design**
Literature on bidding in repeated auctions; budget-constrained bidding; RL for auction bidding.

**2.5 Related Systems**
Brief survey: Bandwidth on Demand (BoD) systems, cloud spot markets, electricity auctions. What makes IXP bandwidth allocation different.

---

### 3. System Design

**3.1 System Overview**
Architecture diagram: switch → Kafka → telemetry service → Atomix; agents → API gateway → Atomix; auction runner → Kafka → switch.

**3.2 Auction Mechanism**
- Uniform-price sealed-bid auction
- Reservation price (virtual bid) as a floor
- Proportional allocation at the margin
- Why uniform-price over pay-as-bid or VCG (design decision)

**3.3 Data Model and State Management**
- Atomix for shared state (bids, flow metrics, credits, auction history)
- Why strong consistency (Raft) matters for correct auction clearing
- Bid map format; auction history format

**3.4 Event Bus: Kafka**
- Switch telemetry → `switch-telemetry` topic
- Auction results → `auction-results` topic
- Why Kafka (durability, decoupling) over direct RPC

**3.5 Scenario-Driven Configuration**
- `scenario.yaml`: topology, customers, capacities, auction interval
- Customer → ingress port ownership
- Reservation price and starting balance

**3.6 API Design**
- Customer-scoped endpoints: `/flows`, `/bids`, `/credits`, `/auctions`
- Token authentication (X-Customer-ID)
- Why credits are accounting-only (no hard rejection)

**3.7 Key Design Decisions Summary**

*(Table of the 12 decisions from existing FEEDBACK-BRAINSTORM.md — D1 through D12)*

---

### 4. Agent Strategies

**4.1 Agent Architecture**
- Pluggable `BiddingStrategy` interface
- Per-agent bid context: telemetry, credits, auction history
- Deployment: one agent pod per customer

**4.2 Demand Estimation**
- The `effective_demand = throughput + drop_kbps` formula
- Why fixed 10% headroom is insufficient under congestion (design decision)

**4.3 Strategy Portfolio**

For each strategy: one paragraph on motivation, pseudocode for bid computation, and which experiment tests it.

| # | Strategy | Core Idea |
|---|----------|-----------|
| 4.3.1 | `conservative` | Bid at last clearing price; 10% throughput headroom |
| 4.3.2 | `demand_corrected` | Drop-aware quantity; market price |
| 4.3.3 | `price_insensitive` | Fixed high price; guaranteed allocation |
| 4.3.4 | `budget_aware` | Scale bid price with remaining balance |
| 4.3.5 | `valuation_based` | Bid at true valuation; never exceed willingness-to-pay |
| 4.3.6 | `throughput_optimizer` | Bid aggressively when price is cheap and demand is high |
| 4.3.7 | `q_learning` | Tabular RL; utility-aligned reward |

**4.4 Why EMA-Based Strategies Were Removed**
Short subsection: theoretical argument for why price-following (EMA, backoff) is dominated by truthful bidding in uniform-price auctions. Reference Vickrey/Milgrom.

**4.5 Utility as an Evaluation Criterion**
Definition, formula, and per-agent utility calculation.

---

### 5. Experimental Evaluation

**5.1 Experimental Setup**
- Hardware: Minikube on a single node; simulated traffic from dummy switch
- Metrics collection: Prometheus scrape → export JSON
- Experiment reset procedure (Atomix state)

**5.2 Experiment 1 — Baseline**
*(Already designed — conservative vs. conservative)*

**5.3 Experiment 2 — Drop-Rate Recovery**
*(Conservative vs. demand_corrected under spike)*

**5.4 Experiment 3 — Heterogeneous Strategies: Price-Insensitive**
*(Conservative vs. price_insensitive)*

**5.5 Experiment 4 — Budget Exhaustion**
*(Conservative vs. budget_aware with finite budget)*

**5.6 Experiment 5 — Auction Convergence**
*(Symmetric agents; clearing price stability)*

**5.7 Experiment 6 — Interval Sensitivity**
*(10s / 30s / 60s)*

**5.8 Experiment 6 — Dominant Strategy Validation (Valuation-Based vs. Conservative)**
*(Includes `q_learning` as an additional agent — convergence behaviour visible within the graphs)*

**5.9 Experiment 7 — Market Efficiency with Mixed Valuations**

**5.10 Experiment 8 — Control Loop Latency (Traffic Change → Allocation Effect)**
*(Reuses Experiment 5 interval scenarios; measures transient step-response, not steady-state)*

**5.11 Experiment 9 — EMA as Negative Result (Exploratory vs. Valuation-Based)**
*(Short experiment; validates the theoretical argument from Section 4.4)*

**5.12 Comparative Analysis**
Cross-experiment utility comparison table; which strategy achieves the best utility under which conditions; summary of which theoretical predictions were confirmed empirically.

---

### 6. Conclusions

**6.1 Summary of Contributions**
Reiterate: what was built, what was shown.

**6.2 Lessons Learned**
- EMA strategies are theoretically misaligned with second-price auctions
- Utility is the right evaluation metric; drop rate alone is insufficient
- Auction interval is the dominant factor in control loop latency

**6.3 Limitations**
- Single switch (one egress port per experiment)
- Simulated traffic (dummy switch, not real BGP)
- Credits accounting only — no hard budget enforcement at bid time
- Tabular Q-learning state space is hand-engineered and small

**6.4 Recommendations for Further Work**
- Multi-switch, multi-egress scenario
- Per-VLAN flow tracking (multiple AS per physical port)
- Deep RL with a Python sidecar (Option B from Section 5)
- Real traffic replay from IXP traffic dumps (e.g. CAIDA datasets)
- Integration with a live BGP route server

---

### References

*(Cite: Vickrey 1961, Milgrom auction theory, relevant IXP papers, SDN papers, Strimzi/Kafka/Atomix docs, Go kafka-go)*

---

### Appendix A — Scenario Configuration Reference
Full annotated `scenario.yaml` format.

### Appendix B — Running the Experiments
Step-by-step reproduction guide (mirrors the README Experiments section but with more detail).

### Appendix C — Agent Strategy Configuration Parameters
Full table of all configurable parameters per strategy.

---

## 9. Final Product: Agent and Experiment Tables

### 9.1 Agent Portfolio

| # | Agent | Status | Strategy Type | Bid Price | Bid Units | `valuation_per_unit` role | Primary Experiment |
|---|-------|--------|---------------|-----------|-----------|--------------------------|-------------------|
| 1 | `conservative` | Keep | Heuristic baseline | Last clearing price (floor: reservation) | 110% of throughput | Upper cap on bid price | Exp 1, 2a, 3, 7 |
| 2 | `demand_corrected` | Keep | Heuristic, improved | Last clearing price (floor: reservation) | 105% of (throughput + drops) | Upper cap | Exp 2b, 9 |
| 3 | `price_insensitive` | Keep, reframe | Implicit high-valuation | Fixed multiple of reservation (e.g. 10×) | 105% of (throughput + drops) | Upper cap (effectively the bid price) | Exp 3 |
| 4 | `budget_aware` | Keep | Budget-constrained | Scales with remaining balance fraction | 105% of (throughput + drops) | Upper cap | Exp 4a/4b |
| 5 | `valuation_based` | **Add (new)** | Truthful / dominant strategy | Exactly `valuation_per_unit` | 105% of (throughput + drops) | **The bid price itself** | Exp 7, 8, 10 |
| 6 | `throughput_optimizer` | **Add (new)** | Intertemporal budget | `valuation_per_unit` when cheap+high-demand; scales down otherwise | Varies by demand and price signal | Upper cap and target price for ideal conditions | Exp 4 (extended) |
| 7 | `q_learning` | Revise | Reinforcement learning | Last clearing price × learned multiplier | 105% of (throughput + drops) | Upper cap; reward redesigned to utility | Exp 7 / 8 (as additional agent) |
| 8 | `exploratory` (EMA) | **Keep, mark deprecated** | Price-following (negative result) | EMA of clearing prices + ε | 105% of (throughput + drops) | Upper cap | Exp 10 (negative result only) |
| — | `backoff` | **Remove** | Price-following | Halves price after expensive rounds | — | — | No experiment; no theoretical interest in second-price auctions |

**Notes:**
- `valuation_per_unit` is configured for **every agent** (see Section 6.5). It acts as a hard ceiling for all strategies except `valuation_based`, where it is exactly the bid price.
- `q_learning` is included in mixed-strategy experiments rather than a standalone convergence experiment; its temporal improvement in utility is visible within those graphs.
- `exploratory` is retained in the codebase solely for Experiment 10 (negative result). It is not part of the recommended strategy portfolio.

---

### 9.2 Experiment Set

| # | Name | Agents | Key Variable | What It Shows | Scenario File |
|---|------|--------|-------------|---------------|---------------|
| 1 | Baseline + Convergence | `conservative` × 2 | None | Auction mechanism correctness; clearing price stabilises; drops reach zero; convergence observed over 20+ intervals | `experiment-1-baseline.yaml` |
| 2a | Conservative Spike — Persistent Drops | `conservative` × 2 | Traffic spike at interval 5 | Conservative never recovers from drops (bids only on egress rate, not ingress demand) | `experiment-2a-conservative-spike.yaml` |
| 2b | Demand Formula Comparison | `conservative` vs. `demand_corrected` | Agent quantity formula | Demand-corrected recovers from the same spike in 1–2 intervals; allocation lines visibly diverge | `experiment-2b-demand-corrected-spike.yaml` |
| 3 | High-Valuation Truthful Bidder | `price_insensitive` vs. `conservative` | Valuation heterogeneity (implicit) | High-valuation agent wins consistently; conservative absorbs all drops; clearing price rises | `experiment-3-heterogeneous.yaml` |
| 4 | Budget Management | `conservative` vs. `budget_aware` vs. `throughput_optimizer` (all with finite balance) | Budget strategy | Conservative exhausts credits fast; budget_aware stretches credits; throughput_optimizer allocates strategically across high/low-demand phases | `experiment-4a-conservative-budget.yaml` + `experiment-4b-budget-aware.yaml` + new `experiment-4c-throughput-optimizer.yaml` |
| 5 | Interval Sensitivity | `demand_corrected` × 2 | Auction interval: 10s / 30s / 60s | Short intervals reduce drop duration but increase credit overhead; long intervals increase sustained drops; quantifies the trade-off | `experiment-6a-interval-10s.yaml`, `6b`, `6c` |
| 6 | Dominant Strategy Validation | `valuation_based` vs. `conservative` (+ `q_learning` as extra) | Agent strategy (same valuation configured) | Truthful bidding achieves equal or better allocation at equal cost; confirms dominant strategy theory empirically | new `experiment-7-valuation-vs-conservative.yaml` |
| 7 | Market Efficiency — Mixed Valuations | `valuation_based` (low val=5) vs. `valuation_based` (high val=20) | Valuation level | High-valuation agent wins majority of contested allocation; clearing price settles near low-valuation agent's value; demonstrates efficient allocation | new `experiment-8-mixed-valuations.yaml` |
| 8 | Control Loop Latency | `demand_corrected` | Auction interval (reuses Exp 5 scenarios) | Measures `t_detect` (telemetry lag) + `t_react` (auction interval) = total response time to a traffic spike; quantifies responsiveness vs. traditional IXP timescales | reuse `experiment-6a/6b/6c` |
| 9 | EMA as Negative Result | `exploratory` vs. `valuation_based` | Agent strategy | EMA agent loses rounds where `EMA < clearing_price`; utility gap confirms theoretical prediction that price-following is dominated in second-price auctions | new `experiment-9-ema-negative.yaml` |

**Removed experiments and justification:**

| Removed | Reason |
|---------|--------|
| Experiment 5 — Convergence | Absorbed by Experiment 1 (run it longer; same setup, same agents, same metrics — no new independent variable) |
| Experiment 4a as standalone | On its own ("conservative exhausts credits") the result is obvious and requires no measurement to predict. Retained only as a comparison baseline within the Experiment 4 group. |
| Standalone Q-learning convergence | Q-learning is included as an additional agent in Experiments 6 and 7; its learning curve is visible in those graphs without a dedicated experiment slot. |
| Throughput optimizer as standalone | Folded into Experiment 4 (budget management group) as a third strategy; same scenario, same metrics — no need for a separate experiment. |

---

## 10. Summary of Actions

| Item | Action | Priority |
|------|--------|----------|
| Remove `backoff` strategy | Delete `agent/strategies/backoff.go`; update README | Medium |
| Mark `exploratory` as deprecated | Add comment header; keep for Experiment 9 | Low |
| Implement `valuation_based` agent | New strategy; ~50 lines | High |
| Implement `throughput_optimizer` agent | New strategy; ~100 lines | High |
| Add `valuation_per_unit` to all agent configs | Edit shared config struct; add default = 10× reservation | High |
| Redesign Q-learning reward to utility | Edit `agent/strategies/q_learning.go` | Medium |
| Add utility metrics to Prometheus export | Edit `api/server.go` metrics | High |
| Add scenario files for Experiments 6–9 | `experiment-7` through `experiment-9` YAML files | Medium |
| Decide project title | Choose from Section 6 candidates (D, C, or E) | Low |
| Draft report outline in LaTeX/Overleaf | Use structure in Section 8 | Low (later) |
