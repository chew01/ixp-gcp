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

## Deploying Against a Real Switch

Use this path when the switch is generating live traffic and no dummy producer is needed. The scenario is configured in `etc/scenario/scenario.yaml`.

The real switch publishes telemetry to an **external Kafka broker**, so Strimzi is not deployed and every `make` command that touches services must receive the broker address.

**First time only — cluster setup:**

```bash
make setup            # register Helm repos
make deploy-minikube  # start Minikube (4 CPUs, 8 GiB)
make all KAFKA_EXTERNAL=true KAFKA_BOOTSTRAP=<host:port>
```

If the broker requires mTLS (e.g. Aiven), create the TLS secret first and pass the certificate paths:

```bash
kubectl create secret generic kafka-tls \
  --from-file=ca.pem --from-file=service.cert --from-file=service.key

make infra KAFKA_EXTERNAL=true KAFKA_BOOTSTRAP=<host:port>
make services \
  KAFKA_BOOTSTRAP=<host:port> \
  KAFKA_TLS_CA_FILE=/etc/kafka-tls/ca.pem \
  KAFKA_TLS_CERT_FILE=/etc/kafka-tls/service.cert \
  KAFKA_TLS_KEY_FILE=/etc/kafka-tls/service.key
```

**Every subsequent run (or after editing `scenario.yaml`):**

```bash
make deploy-real
```

This single target:
1. Pushes `etc/scenario/scenario.yaml` as the `test-scenario` ConfigMap
2. Restarts `auction-runner`, `telemetry-service`, and `api-gateway` and waits for rollout
3. Builds the `customer-agent:local` image
4. Removes any previously running agent pods
5. Generates and applies one agent Deployment per customer in the scenario (no dummy producer)

Kafka connection details are baked into the service Deployments at `make services` time, so `deploy-real` does not need them again.

**Observe:**

```bash
make grafana-ui    # Grafana at http://localhost:3000
make prometheus-ui # Prometheus at http://localhost:9090
```

> Before running `deploy-real`, review `etc/scenario/scenario.yaml` and update `max_capacity` on the switch entry to the measured egress line-rate of the physical port (in kbps).

---

## Running an Experiment

Experiments use a dummy traffic producer to simulate switch telemetry on a local cluster. Each has its own scenario YAML under `etc/scenario/`.

**First time only — cluster setup:**

```bash
make setup            # register Helm repos
make deploy-minikube  # start Minikube (4 CPUs, 8 GiB)
```

**Deploy the full stack for a specific experiment:**

```bash
make all experiment=2a
```

This single command:
1. Deploys Atomix, Kafka, Prometheus/Grafana, and the scenario config (`infra`)
2. Builds and deploys the API gateway, auction runner, and telemetry processor (`services`)
3. Loads the scenario YAML for experiment `2a` as the active ConfigMap
4. Starts the dummy traffic producer
5. Builds the `customer-agent:local` image and deploys one agent pod per customer

**Observe:**

```bash
make grafana-ui    # Grafana at http://localhost:3000
make prometheus-ui # Prometheus at http://localhost:9090
```

**After the run:**

```bash
make export-metrics   # saves data/experiment-<timestamp>.json
```

**Switching experiments on a running cluster** (faster than a full redeploy):

```bash
make all experiment=7
```

Or to swap the scenario without rebuilding everything:

```bash
make all
```
This will set up all necessary infra and services (API gateway, auction runner, telemetry, dummy producer, and customer agents).

### Customers, scenario, and tokens

- **Scenario config:** `etc/scenario/scenario.yaml` defines:
  - Switches, ingress/egress ports, and capacities.
  - A `customers` section mapping customer IDs (e.g. `as12345`, `as67890`) to the ingress ports they own on each switch.
- **Customer identity:** Every bid and customer-facing API call is authenticated with a **customer ID token**:
  - Use the `X-Customer-ID: <customer_id>` header (or `Authorization: Bearer <customer_id>`).
  - The API enforces that:
    - `POST /bids` can only submit bids for ingress ports owned by that customer.
    - `GET /credits` only returns credits for the authenticated customer.
    - `GET /flows` only returns flows for the customer's ingress ports.
    - `GET /auctions` returns auction history (clearing prices and the caller's own allocations), never other customers' allocations.

### API Reference

All customer-facing endpoints require authentication. Supply one of:
- `X-Customer-ID: <customer_id>` header
- `Authorization: Bearer <customer_id>` header

| Method | Path | Parameters | Response | Notes |
|--------|------|------------|----------|-------|
| `GET` | `/flows` | **Query:** `switch_id` (string), `ingress_port` (int), `egress_port` (int) — all required | `{ "<switch>\|<in>\|<eg>": { "throughput_kbps": float, "egress_kbps": float, "drop_kbps": float, "drop_rate_pct": float } }` | `throughput_kbps` = raw ingress rate; `egress_kbps` = actual forwarded rate (capped by allocation under congestion); `drop_kbps` = ingress − egress. Returns `403` if port not owned by caller; `404` if no telemetry received for that flow yet. |
| `POST` | `/bids` | **Body (JSON):** `ingress_port` (uint64), `egress_port` (uint64), `units` (uint64 > 0), `unit_price` (int > 0) — all required | `202 Accepted` — `"bid accepted"` | Bid is stored in Atomix and consumed by the auction runner at the next interval. Returns `403` if port not owned by caller; `400` if any field is missing or invalid. |
| `GET` | `/credits` | _(none)_ | `{ "total_spent": int, "starting_balance": int }` | `starting_balance` is `0` when no finite budget is configured in the scenario. Credits are accounting-only — spending never blocks a bid. The auction runner debits `units × clearing_price` per accepted bid. |
| `GET` | `/auctions` | **Query:** `egress_port` (uint64, optional) | `[ { "interval": string, "egress_port": uint64, "clearing_price": int, "allocations": [ { "customer_id": string, "ingress_port": uint64, "units": uint64 } ] } ]` | Returns full auction history filtered to the caller's own allocations. If `egress_port` is provided, only records for that port are returned. `allocations` is omitted from a record if the caller placed no bid in that interval. |
| `GET` | `/metrics` | _(none)_ | Prometheus text exposition format | Prometheus scrape endpoint — no authentication required. Refreshes all gauges from live Atomix state on every scrape. |

#### Prometheus Metrics

| Metric | Labels | Description |
|--------|--------|-------------|
| `ixp_flow_throughput_kbps` | `switch_id`, `ingress_port`, `egress_port` | Raw ingress send rate per flow |
| `ixp_flow_egress_kbps` | `switch_id`, `ingress_port`, `egress_port` | Actual forwarded rate per flow (capped by allocation) |
| `ixp_flow_drop_kbps` | `switch_id`, `ingress_port`, `egress_port` | Dropped traffic per flow (ingress − egress) |
| `ixp_flow_drop_rate_percent` | `switch_id`, `ingress_port`, `egress_port` | Drop rate as a percentage of ingress |
| `ixp_auction_clearing_price` | `egress_port` | Clearing price from the most recent auction round |
| `ixp_customer_allocation_kbps` | `customer_id`, `egress_port` | Total bandwidth allocated across all ingress ports (most recent round) |
| `ixp_customer_credits_spent_total` | `customer_id` | Cumulative credits spent |

### Customer agents

- `agent/` contains a customer agent binary that:
  - Reads `CUSTOMER_ID`, `API_BASE_URL`, and `SCENARIO_PATH` from the environment.
  - Periodically fetches customer-scoped flows, credits, and auction history from the API.
  - Submits bids via `POST /bids` for its own ingress ports only.
- The Makefile target `deploy-agent` builds a `customer-agent:local` image and deploys one agent per configured customer (see `agent/deployment.yaml`).

#### Agent strategies

| Strategy | Bid units | Bid price | Key behaviour |
|----------|-----------|-----------|---------------|
| `conservative` | 110% of current throughput (min 1 kbps) | Last clearing price (floor: reservation price) | Skips flows with no throughput and no drops; never accounts for dropped traffic, so recovers slowly from congestion |
| `demand_corrected` | 105% of throughput + drops (min 1 kbps) | Last clearing price (floor: reservation price) | Includes dropped traffic in the unit estimate; recovers from congestion faster than `conservative` |
| `price_insensitive` | 105% of throughput + drops (min 1 kbps) | Fixed multiple of reservation price (default 10×); ignores clearing history | Models latency-critical traffic that values guaranteed bandwidth above cost; outbids all price-following strategies |
| `budget_aware` | 105% of throughput + drops (min 1 kbps) | Scales with remaining credit fraction: >75% → clearing price; >50% → 75% of clearing; >25% → 50% of clearing; ≤25% → reservation price | Falls back to `conservative` when no starting balance is configured; stretches credits by reducing bid price as balance depletes |
| `exploratory` | 105% of throughput + drops (min 1 kbps) | EMA of clearing prices + fixed epsilon margin (floor: reservation price) | Tracks market price with a smoothed average (`ema_alpha`, default 0.3) rather than following last-round spikes directly |
| `backoff` | 105% of throughput + drops (min 1 kbps) | Last clearing price × multiplier (floor: reservation price); multiplier halved (min 0.5×) after N consecutive expensive rounds, resets on a cheap round | Deliberately cools the market when prices stay high for too long (`backoff_threshold`, default 3 rounds; `expensive_price`, default 2× reservation) |
| `q_learning` | 105% of throughput + drops (min 1 kbps) | Last clearing price × learned multiplier chosen from `[0.8×, 1.0×, 1.25×, 1.5×, 2.0×, 3.0×]` (floor: reservation price) | Tabular Q-learning over `(drop_bucket, budget_bucket)` state; reward = reduction in drop rate; epsilon-greedy exploration. Params: `ql_alpha` (default 0.1), `ql_gamma` (default 0.9), `ql_epsilon` (default 0.15) |

#### Grafana
```bash
make grafana-ui
```
This will port forward Grafana to port 3000.

### References
- [Atomix](https://atomix.github.io)

### Consuming Auction Results
```bash
kubectl exec -it ixp-kafka-dual-role-0 -- \
bin/kafka-console-consumer.sh \
--bootstrap-server localhost:9092 \
--topic auction-results \
--from-beginning
```
This prints all the records since the beginning.

### Telemetry Log Format

- Key: switch id
- Value: see [shared/structs.go]()

### Kafka setup
On Aiven, create topics:
- switch-telemetry
- auction-results

For external broker usage, place ca.pem, service.cert and service.key in the certs directory, then run:
```bash
make deploy-minikube
kubectl create secret generic kafka-tls \
--from-file=ca.pem=certs/ca.pem \
--from-file=service.cert=certs/service.cert \
--from-file=service.key=certs/service.key
make all KAFKA_EXTERNAL=true KAFKA_BOOTSTRAP=$ADDRESS KAFKA_TLS_CA_FILE=/etc/kafka-tls/ca.pem KAFKA_TLS_CERT_FILE=/etc/kafka-tls/service.cert KAFKA_TLS_KEY_FILE=/etc/kafka-tls/service.key
```
Remember to put the actual address.

### Experiments
Step 1 — Deploy the experiment scenario
```bash
make deploy-experiment-1
```
This updates the ConfigMap, rolls all pods, and waits for them to be ready. Takes ~1 minute.

Step 2 — Open Grafana and watch
```bash
make grafana-ui   # opens http://localhost:3000
```

Step 3 — Wait for the run duration specified in the scenario file

Step 4 — Export the data
```bash
make prometheus-ui   # forward Prometheus to localhost:9090 (if not already running)
make export-metrics
```
Saves `data/experiment-<timestamp>.json` with clearing price, per-customer allocation, drop rate, and throughput time-series for the full run.

| Experiment | Scenario file | What to observe |
|------------|--------------|-----------------|
| 1 — Baseline | `experiment-1-baseline.yaml` | Drop rate falls to ~0 after 1–2 intervals; clearing price stable at 50; credits symmetric |
| 2a — Conservative spike (symmetric) | `experiment-2a-conservative-spike.yaml` | Spike at interval 5; both customers lock at 10 kbps allocation with ~23% drops; never recovers |
| 2b — Conservative vs demand_corrected spike | `experiment-2b-demand-corrected-spike.yaml` | After spike, `as67890` (demand_corrected) wins more allocation than `as12345` (conservative); allocation lines diverge |
| 3 — Heterogeneous (price_insensitive) | `experiment-3-heterogeneous.yaml` | `as67890` (price_insensitive) gets full allocation; `as12345` (conservative) absorbs all drops; clearing price stays at 50 |
| 4a — Conservative with finite budget | `experiment-4a-conservative-budget.yaml` | Credits drain fast (~2 rounds); abrupt throughput cliff once balance exhausted |
| 4b — Budget_aware with finite budget | `experiment-4b-budget-aware.yaml` | Credits stretch longer; agent throttles bid price as balance depletes |
| 5 — Convergence | `experiment-5-convergence.yaml` | Clearing price and allocation stabilise within a few intervals; observe steady state |
| 6a — Interval 10s | `experiment-6a-interval-10s.yaml` | Fast reaction to spike; tighter control loop |
| 6b — Interval 30s | `experiment-6b-interval-30s.yaml` | Baseline interval; moderate reaction lag |
| 6c — Interval 60s | `experiment-6c-interval-60s.yaml` | Slow reaction; sustained drops between auction rounds |

> **Note:** Reset Atomix state between experiments that track credits (4a, 4b) to avoid carry-over. See the reset steps in the project documentation.
