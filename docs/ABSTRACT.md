# Abstract

**Auction-Driven Bandwidth Management at Internet Exchange Points: System Design and Evaluation**

Internet exchange points (IXPs) depend on slow bilateral peering arrangements and static capacity, while traffic varies on much shorter timescales. This thesis investigates whether a **repeated uniform-price sealed-bid auction**, driven by a software-defined control plane, can allocate IXP egress bandwidth at sub-minute intervals in a measurable, systematic way.

We describe the **architecture** of an auction-centric control plane: customer agents submit bids from switch telemetry; a service clears each round at a uniform price with a reservation floor and marginal proportional allocation; state lives in a strongly consistent store; results flow over an event bus. Scope emphasises the **auction mechanism, bidding agents, and evaluation**—not switch-level table programming, which is treated as a separate integration surface.

We implement **multiple strategies**: conservative and demand-corrected heuristics, budget-aware and throughput-optimising behaviour, a **valuation-based (truthful)** bidder consistent with the dominant strategy of uniform-price auctions, and tabular Q-learning with utility-based rewards. Price-tracking strategies based on clearing-price history are **excluded** as theoretically inappropriate; one experiment records this as a deliberate negative result.

**Evaluation** uses simulated traffic on Minikube, with scenarios covering congestion recovery, heterogeneous valuations, budget limits, auction-interval sensitivity, and control-loop latency after demand steps. The primary metric is **economic utility**—bandwidth value net of payments at the clearing price—alongside allocation and transient metrics.

**Results**: valuation-based bidding aligns with theory and provides a strong baseline; other agents differ in utility stability and credit efficiency; auction interval dominates responsiveness; exponential moving-average bidding underperforms truthful bidding under uniform pricing, as predicted. We state limitations (simulated workload, single-switch topology, soft credit accounting) and outline extensions to multi-switch settings and richer learning agents.

---

*Adjust length to your faculty’s word limit; `PROFESSOR-FEEDBACK-ROUND2.md` §8 calls for a one-page summary of problem, approach, agents, experiments, and key findings.*
