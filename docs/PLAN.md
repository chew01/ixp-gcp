# Implementation Plan: Customer Abstraction, Statistics, and Customer Agent

This document is a step-by-step plan to extend the distributed SDN control plane with: (1) richer flow statistics and Prometheus exposure, (2) customer abstraction and bid-credit accounting, (3) token-authenticated bidding, and (4) a customer agent that replaces the dummy bidder and optimizes throughput vs. credit spend.

**Design decisions (from FAQ):**

- **Customer ↔ topology:** One customer can own multiple ingress ports (e.g. AS 12345 owns ports 1, 2, 3). Scenario config will map customer/AS IDs to ingress ports.
- **Bid credits:** Accounting only — track total credits spent per customer (and optional starting balance). Do **not** reject bids for insufficient balance.
- **Customer agent deployment:** One agent process per customer; each agent is identified by env `CUSTOMER_ID` (e.g. `CUSTOMER_ID=as12345`) and only bids for that customer.

---

## Phase 1: Extend flow statistics (same map, new value format)

**Goal:** Expose more than throughput to users and Prometheus, using the same Atomix map and changing only the value format.

### 1.1 Define flow metrics value format

- **Location:** `shared/structs.go` (and/or a small types file used by telemetry + API).
- Add a struct for the **stored flow metrics** (one per flow key), e.g.:

  ```go
  type FlowMetricsValue struct {
      ThroughputKbps float64 `json:"throughput_kbps"`   // ingress Kbps (existing)
      EgressKbps     float64 `json:"egress_kbps"`
      DropKbps       float64 `json:"drop_kbps"`
      DropRatePct    float64 `json:"drop_rate_pct"`
      // optional: last_updated_ts for staleness
  }
  ```

- Keep the **map key** unchanged: `switch_id|ingress_port|egress_port`.
- **Value:** JSON-serialised `FlowMetricsValue` instead of a single float string.

### 1.2 Telemetry consumer: write full metrics to the map

- **File:** `telemetry/consumer.go`.
- In `publishMetrics`, instead of:
  - `c.throughputMap.Put(ctx, m.FlowKey, fmt.Sprintf("%.2f", m.IngressKbps))`
- Serialise `FlowMetricsValue{ThroughputKbps: m.IngressKbps, EgressKbps: m.EgressKbps, DropKbps: m.DropKbps, DropRatePct: m.DropRate}` and `Put` that JSON string.
- Ensure backward compatibility or a one-time migration: if other components still read the old format during rollout, consider a short transition (e.g. write both formats temporarily) or deploy telemetry first, then API.

### 1.3 API: read new format and expose to user + Prometheus

- **Files:** `api/atomix.go`, `api/server.go`.
- **FlowStore:** Continue to use the same Atomix map name (`throughput-map`). `Get`/`List` return the raw string; parse as JSON into `FlowMetricsValue` in the API layer.
- **GET /flows:** For each flow key, return the full metrics object (e.g. `flowKey -> FlowMetricsValue` in JSON response), not just throughput.
- **refreshMetrics (Prometheus):** For each flow key, parse `FlowMetricsValue` and set:
  - `ixp_flow_throughput_kbps` (existing)
  - `ixp_flow_egress_kbps` (new)
  - `ixp_flow_drop_kbps` (new)
  - `ixp_flow_drop_rate_percent` (existing gauge; currently never set — now populate from stored value).
- **Backward compatibility:** If the stored value is the old single number, parse it as `throughput_kbps` and set other metrics to 0 (or omit) so Prometheus and GET /flows still work during/after migration.

### 1.4 Grafana / monitoring

- **File:** `observability/ixp-flows.json` (and any other dashboards).
- Add panels for the new series: e.g. `ixp_flow_egress_kbps`, `ixp_flow_drop_kbps`, `ixp_flow_drop_rate_percent` where useful.
- No change to map topology (same keys); only new metrics and possibly updated panel queries.

### 1.5 Checklist Phase 1

- [ ] `FlowMetricsValue` in shared (or api/telemetry-agreed types).
- [ ] Telemetry writes JSON `FlowMetricsValue` to existing map.
- [ ] API parses JSON; GET /flows returns full metrics; refreshMetrics sets all four Prometheus gauges.
- [ ] Backward compatibility for old single-value format.
- [ ] Dashboard updates for new metrics.

---

## Phase 2: Customer abstraction and scenario config

**Goal:** Introduce customers (AS providers) that own ingress ports and receive auction allocations; scenario config defines customer → ingress port ownership.

### 2.1 Scenario: customer → ingress ports

- **File:** `shared/scenario/types.go`.
- Extend `Scenario` or add a new top-level field, e.g. `Customers []Customer`.
- Define:

  ```go
  type Customer struct {
      ID            string   `yaml:"id"`             // e.g. "as12345"
      IngressPorts  []uint32 `yaml:"ingress_ports"`  // ports this customer owns
      // optional: starting_balance for display/accounting
  }
  ```

- **Validation:** In `shared/scenario/load.go` (or a small validator), ensure every ingress port (across all switches) is assigned to exactly one customer, and that all referenced ports exist in some switch’s `IngressPorts`. Unowned ports must not exist.
- **File:** `etc/scenario/scenario.yaml`: add a `customers` section mapping customer IDs to ingress ports (e.g. `as12345` → [1,2,3], `as67890` → [4,5]).

### 2.2 Helper: which customer owns an ingress port?

- Add a function (e.g. in `shared/scenario` or `shared`) that, given scenario and `(switchID, ingressPort)`, returns the customer ID (if any). This will be used by the API (bids), auction runner (attribution), and agent (filtering).

---

## Phase 3: Bid credits and auction attribution

**Goal:** When a customer wins auction allocation, “bill” them credits (accounting only; no rejection for insufficient balance).

### 3.1 Credits accounting store

- **Storage:** Use an Atomix map, e.g. `credits-map`: key = customer ID, value = JSON `{ "total_spent": <int>, "starting_balance": <int> }` or just `total_spent` and optional separate config for starting balance.
- **Location:** New store in `api/` (or a small shared pkg if both API and auction need it). API will expose credits via an endpoint; auction runner will update spent after each run.
- **Initialization:** At API startup, every customer from the scenario is ensured to have an entry in the credits map with `total_spent: 0` (and optional `starting_balance`). This is done only when the key is missing, so existing totals are never overwritten. As a result, GET /credits and Prometheus (`ixp_customer_credits_spent_total`) show all customers from the start; the first auction bill then updates the existing entry.

### 3.2 Auction runner: attribute wins to customers and update credits

- **File:** `auction/runner/runner.go`.
- After computing allocations, for each allocation:
  - Resolve customer ID from `(switchID, ingressPort)` using scenario (Phase 2).
  - Credits to bill = `allocated_units * clearing_price` (same as uniform-price: pay clearing price for allocated units).
  - Update the customer’s `total_spent` in the credits map (read-modify-write or use Atomix counter/atomic add if available).
- By design, every ingress port is owned by exactly one customer (scenario validation enforces this), so every allocation has a customer to bill.

### 3.3 Expose credits to user and Prometheus

- **API:** New endpoint, e.g. `GET /customers/:id/credits` or `GET /credits?customer_id=as12345`, returning `total_spent` and optionally `starting_balance`.
- **Prometheus:** New gauge (or family), e.g. `ixp_customer_credits_spent_total` with label `customer_id`. Optionally `ixp_customer_credits_balance` (starting - spent) if you track starting balance.
- **Location:** `api/server.go`, `api/atomix.go` (CreditsStore interface + Atomix implementation).

---

## Phase 4: Bidding requires user token (customer identity)

**Goal:** Every bid is tied to a customer via a token; API validates that the token maps to a customer and that the bid’s ingress port belongs to that customer.

### 4.1 Token semantics

- **Token = customer ID.** We use customer ID as the token (e.g. header `X-Customer-ID: as12345`). No opaque tokens or token→customer mapping for now.
- **Where to send:** Custom header `X-Customer-ID: <customer_id>` (or `Authorization: Bearer <customer_id>`); header keeps body format unchanged.

### 4.2 API: validate token and ingress ownership

- **File:** `api/server.go`, `postBid`.
  - Require token (e.g. from `Authorization` or `X-Customer-ID`). If missing → 401 Unauthorized.
  - Customer ID is the token (use it directly).
  - Validate that the bid’s ingress port belongs to that customer (using scenario). If not → 403 Forbidden.
  - Store the customer ID with the bid (Phase 5) so the auction runner can attribute wins and bill credits.

### 4.3 Bid storage: include customer ID

- **File:** `shared/structs.go`: `BidRequest` remains as-is for the HTTP body (ingress_port, egress_port, units, unit_price). Customer is inferred from token, not from body.
- **File:** `api/atomix.go`, BidStore: when storing a bid, the value format in the bid map must include customer ID so the auction runner can attribute. Current format: key = ingress port, value = `units|unitPrice`. Change to e.g. `units|unitPrice|customerID` (or a small JSON object). Ensure the auction runner and any code that reads bids are updated to parse the new format and pass customer ID to the allocation/credits logic.

---

## Phase 5: Bid map value format and runner read path

**Goal:** Bid map entries are keyed by (egress, ingress) or by ingress with value containing customer ID; runner uses this to attribute allocations and bill credits.

### 5.1 Bid map value format

- **Current:** key = ingress port (string), value = `units|unitPrice`.
- **New:** value = `units|unitPrice|customerID` (e.g. `100|50|as12345`). Key = ingress port (one bid per (ingress, egress) per round).
- **Conflict handling:** Last-write-wins. If multiple bids are submitted for the same (ingress, egress) in one round, the last stored bid wins.

### 5.2 Auction runner: read customer ID from bid and pass to algo

- **File:** `auction/runner/runner.go`: when building `models.Bid` from map entries, parse the third field (customer ID) and attach to the bid (e.g. add `CustomerID string` to `models.Bid`).
- **File:** `auction/models/models.go`: add `CustomerID string` to `Bid`.
- **File:** `auction/algo/reservation.go`: allocations already have ingress/egress/units; when producing allocations, include `CustomerID` in `Allocation` (or derive from bid). Runner then uses allocation’s customer ID to update credits (Phase 3).

### 5.3 End-to-end bid flow

- Client sends `POST /bids` with header `X-Customer-ID: as12345` and body `{ ingress_port, egress_port, units, unit_price }`.
- API validates token and ingress ownership, then stores in Atomix with value `units|unitPrice|as12345`.
- Runner lists bids, parses customer ID, runs auction, gets allocations with customer ID, updates credits for each winning customer.

---

## Phase 6: Customer-scoped APIs and auction history

**Goal:** Harden the API so each customer can only access their own data (flows, credits, auction results), and expose auction history — especially clearing prices — as a first-class, token-authenticated API for agents.

### 6.1 Secure customer-scoped data access

- **Flows API:**
  - Ensure that any `GET /flows` (or bulk flows endpoint) is **scoped by the caller’s token**:
    - The server derives the customer ID from `X-Customer-ID` / `Authorization: Bearer`.
    - Returned flows must only include ingress ports owned by that customer (using the scenario mapping).
  - If you later add a “list all flows” style endpoint, it must still apply this customer filter.
- **Credits API:**
  - `GET /credits` (or `GET /customers/:id/credits`) must:
    - Require a valid customer token.
    - Only return credits for the authenticated customer, ignoring or validating any `customer_id` query param so one customer cannot read another’s credits.
- **Prometheus / metrics:**
  - When exposing per-customer metrics (e.g. `ixp_customer_credits_spent_total`), ensure that **agent-facing** access (if any) is restricted or proxied so agents cannot trivially scrape other customers’ labels.

### 6.2 Auction history endpoint

- **New endpoint:** Add an API (e.g. `GET /auctions` or `GET /auctions?egress_port=0`) that exposes **auction history**, including:
  - Per-interval **clearing price**.
  - Optionally, per-customer allocations for the authenticated customer only (never for others).
- **Security / scoping:**
  - Global values that are safe to share (e.g. clearing price per interval) may be returned to any authenticated customer.
  - Any per-customer fields must be filtered so the caller only sees their own allocations / effective prices.
- **Storage:** Back the endpoint with a small history store (e.g. Kafka topic or Atomix map) populated by the auction runner after each interval.

### 6.3 Checklist Phase 6

- [ ] Flows endpoint(s) enforce token-based scoping so customers only see metrics for their own ingress ports.
- [ ] Credits endpoint(s) are token-authenticated and only return the caller’s credits.
- [ ] New auction history endpoint exposes at least clearing prices (and optionally own allocations) in a customer-safe way.
- [ ] Any agent-visible metrics endpoints are configured so agents cannot read other customers’ per-label data.

---

## Phase 7: Customer agent (replace dummy bidder)

**Goal:** One agent process per customer that uses the secured, customer-scoped APIs (Phase 6) plus auction history to submit bids that maximise guaranteed throughput while minimising credit spend. No more dummy bidder in the main path.

### 7.1 New component: customer agent

- **Directory:** e.g. `agent/` (or `customer-agent/`).
- **Binary:** Reads env `CUSTOMER_ID` (e.g. `as12345`). Loads scenario (or gets topology from API). Only considers ingress ports owned by this customer.
- **Dependencies:** HTTP client to API (GET flows/metrics, GET credits, GET auction history, POST bids). Optionally Kafka consumer for auction results (to observe clearing price and own allocations) — or rely only on the HTTP APIs.

### 7.2 Data from API (customer-scoped and auction results)

- The agent must only call the **customer-scoped** endpoints defined in Phase 6:
  - **GET /flows** (or bulk): flow keys and metrics (throughput, drop rate, etc.) for this customer’s ingress ports only.
  - **GET /credits** (or `GET /customers/:id/credits`): current spent (and balance if any) for this customer.
  - **GET /auctions**: historical auction results / clearing prices; the agent uses:
    - Clearing prices over recent intervals for the relevant egress ports.
    - Its own past allocations (if exposed) to understand how aggressively it needed to bid.
  - **POST /bids**: submit bid with header `X-Customer-ID: <CUSTOMER_ID>` and body `{ ingress_port, egress_port, units, unit_price }`.
    - API validates that `ingress_port` belongs to the authenticated customer (Phase 4).

### 7.3 Agent strategy (high level)

- **Objective:** Maximise guaranteed throughput (allocated units) for this customer’s ingress ports while minimising credits spent.
- **Inputs:** Own ingress ports (from scenario or config), flow metrics (current throughput, drop rate) **restricted to own ports**, current credits spent, and **history of auction results / clearing prices** from the API.
- **Strategy ideas (to implement in agent logic):**
  - Bid only on (ingress, egress) where ingress is owned by the customer.
  - Use throughput and drop rate to estimate “need”: e.g. if current throughput is high and drop rate is rising, bid for more units; if throughput is low, bid lower units or lower price.
  - To minimise spend: bid at or slightly above reservation price when competition is low; bid higher only when necessary to win capacity (e.g. when clearing price history or current demand suggests it).
  - Simple heuristic: bid `units = min(current_throughput_kbps * 1.1, max_capacity_per_port)`, `unit_price = reservation_price + margin`; tune margin from observed clearing prices if available.
- **Loop:** Every auction interval (from scenario or env):
  - Fetch **customer-scoped** flow metrics and credits.
  - Fetch recent auction results / clearing prices for the relevant egress ports.
  - Compute bid(s) that take into account:
    - Current congestion (flows, drop rates).
    - Recent market conditions (clearing prices and own allocations).
    - Remaining willingness to spend.
  - POST each bid with the customer token, then sleep until the next interval.

### 7.4 Deployment and removal of dummy bidder

- **Deployment:** One Deployment per customer (or one Deployment with multiple replicas and env `CUSTOMER_ID` different per replica). Use a single image; inject `CUSTOMER_ID` and API base URL (e.g. `http://api-gateway`). Optionally ConfigMap for scenario or customer list.
- **Dummy bidder:** Remove or disable the dummy bidder from the default deployment path (e.g. remove from `dummy/main.go` or deploy dummy only in “demo” mode). Document that production uses customer agents instead.

### 7.5 Compatibility note

- **Dummy bidder:** Currently checks `resp.StatusCode != http.StatusOK` (200). API returns `http.StatusAccepted` (202). Fix dummy to accept 202, or keep as-is if dummy is being removed; the new agent should treat 202 as success.

### 7.6 Checklist Phase 7

- [x] New `agent/` (or `customer-agent/`) package: main, HTTP client, config from env (CUSTOMER_ID, API URL, scenario path or API topology).
- [x] Agent fetches customer-scoped flows, credits, and auction history, computes bids for own ingress ports only, POSTs with `X-Customer-ID`.
- [x] Simple strategy: e.g. bid to sustain or slightly exceed current throughput at low price; document extension points for smarter strategies.
- [x] Dockerfile and Kubernetes manifest (or Helm) for one agent per customer.
- [x] Document removal/deprecation of dummy bidder for production.

---

## Phase 8: Documentation and testing

### 8.1 Docs

- Update **README.md**: describe customers, scenario `customers` section, token (header) for bids, credits (accounting only), and that one agent per customer is the production bidder.
- Update **docs/sequence.md**: add customer in the auction sequence (user/agent → API with token → bid stored with customer ID → runner attributes and bills credits).

### 8.2 Testing

- **Unit:** Parsing of new map value formats (flow metrics JSON, bid value with customer ID); scenario validation (customer port ownership).
- **Integration:** API accepts bid with valid token and correct ingress; rejects missing/forbidden token; runner attributes and updates credits; agent can list flows and submit bids for its customer only.

---

## Phase 9: Dummy bidder deprecation (demo-only)

**Goal:** Keep the dummy bidder code for demos and tests, but remove it from the default deployment path so production-style setups use only customer agents.

### 9.1 Deployment changes

- **Makefile:**
  - Remove `deploy-dummy` from the default `services` target so `make services` / `make all` do **not** deploy the dummy bidder.
  - Keep the `deploy-dummy` target available for manual or demo use.
  - Optionally add a `demo-services` target that includes `deploy-dummy` for quick demo setups.
- **Docs / comments:**
  - Mark the dummy bidder in `dummy/` as **demo-only** and not part of the production path.

### 9.2 Behavior and ownership

- Production-like deployments:
  - Use the auction runner, telemetry, API gateway, and customer agents only.
  - All bids in the system should originate from agents that use customer-scoped APIs and tokens.
- Demo / testing deployments:
  - May still use the dummy bidder to quickly exercise the auction path without configuring agents.

### 9.3 Checklist Phase 9

- [ ] Update `Makefile` so `services` does not deploy the dummy bidder by default; keep `deploy-dummy` for opt-in use (and/or add a `demo-services` target).
- [ ] Add comments and/or README notes clarifying that the dummy bidder is demo-only and that customer agents are the production bidder.

---

## Implementation order summary

| Order | Phase | Depends on |
|-------|--------|------------|
| 1 | Phase 1: Flow statistics (map value, telemetry, API, Prometheus) | — |
| 2 | Phase 2: Customer abstraction (scenario, customer→ports) | — |
| 3 | Phase 5: Bid map value + customer ID in runner (so bids can carry customer) | Phase 2 |
| 4 | Phase 4: Token on POST /bids and validation (API stores customer with bid) | Phase 2, 5 |
| 5 | Phase 3: Credits store, runner attribution, GET /credits, Prometheus | Phase 2, 5 |
| 6 | Phase 6: Customer-scoped APIs and auction history | Phase 1, 3, 4, 5 |
| 7 | Phase 7: Customer agent (new component, replace dummy) | Phase 1, 3, 4, 5, 6 |
| 8 | Phase 8: Docs and tests | All |
| 9 | Phase 9: Dummy bidder deprecation (demo-only) | 7 |

Phase 1 can be done in parallel with Phase 2. Phases 3–5 should be done in the order above so that by the time the agent is implemented, token validation, bid storage with customer ID, and credits attribution are in place.

---

## Decisions (resolved)

- **Token format:** Customer ID is used as the token (e.g. `X-Customer-ID: as12345`). No opaque tokens.
- **Multiple bids per (ingress, egress) per round:** Last-write-wins. One stored bid per (ingress, egress) per round; the last write overwrites.
- **Unowned ports:** Unowned ports must not exist. Scenario validation requires every ingress port to be assigned to exactly one customer. Only owned ports receive bids; there is no anonymous or system customer.
