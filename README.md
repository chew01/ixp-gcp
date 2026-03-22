# ixp-gcp

### Prerequisites
- [Go 1.25+](https://go.dev/doc/install)
- [Docker](https://docs.docker.com/engine/install)
- [Kubectl](https://kubernetes.io/docs/tasks/tools)
- Working Kubernetes cluster
- [Helm](helm.sh/docs/intro/install)

### Setup
```bash
make setup
```
This will register necessary helm repos.

### Quick Start
```bash
make all
```
This will set up all necessary infra and services, including:
- **Infrastructure**: Minikube, Kafka, Atomix
- **Observability**: Prometheus, Grafana, OTEL Collector, Jaeger, Loki
- **Services**: API Gateway, Auction Runner, Telemetry Processor, Dummy Producer

#### Observability & Monitoring

The system includes a complete observability stack for metrics, traces, and logs:
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
    - `GET /flows` only returns flows for the customer’s ingress ports.
    - `GET /auctions` returns auction history (clearing prices and the caller’s own allocations), never other customers’ allocations.

### Bidding, credits, and customer agents

- **Bidding API:**
  - `POST /bids` with body:
    - `ingress_port`: uint64 ingress port (owned by the customer).
    - `egress_port`: uint64 egress port.
    - `units`: requested bandwidth units (kbps).
    - `unit_price`: bid price per unit.
  - Must include `X-Customer-ID` header; the API rejects bids for ports not owned by that customer.
- **Credits (accounting only):**
  - The auction runner attributes allocations to customers and updates an Atomix `credits-map`.
  - `GET /credits` returns `{ "total_spent": <int>, "starting_balance": <int> }` for the authenticated customer.
- **Auction history:**
  - `GET /auctions?egress_port=<port>` returns a list of auction intervals, each with:
    - `interval`, `egress_port`, `clearing_price`.
    - The caller’s own allocations for that interval (if any).
- **Customer agents:**
  - `agent/` contains a customer agent binary that:
    - Reads `CUSTOMER_ID`, `API_BASE_URL`, and `SCENARIO_PATH` from the environment.
    - Periodically fetches customer-scoped flows, credits, and auction history from the API.
    - Submits bids via `POST /bids` for its own ingress ports only.
  - The Makefile target `deploy-agent` builds a `customer-agent:local` image and deploys one agent per configured customer (see `agent/deployment.yaml`).

```bash
# Access Grafana (business metrics dashboard)
make grafana-ui
# Then visit: http://localhost:3000 (admin/admin)

# Access Prometheus (raw metrics)
kubectl port-forward -n monitoring svc/monitoring-kube-prometheus-prometheus 9090:9090

# Access Jaeger (distributed tracing)
kubectl port-forward -n monitoring svc/jaeger-all-in-one-query 16686:16686
```

**Business Metrics Tracked:**
- `ixp_flow_throughput` - Flow throughput in Kbps per switch/port
- `ixp_flow_drop_rate` - Flow packet drop rate (planned)

See [docs/observability-architecture.md](docs/observability-architecture.md) for detailed architecture information.

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