# Observability Guide

Complete reference for IXP SDN observability stack: deployment, metrics, dashboards, closed-loop feedback, and troubleshooting.

---

## Quick Start

```bash
# First time setup
make setup

# Deploy everything (infrastructure + services + observability)
make all

# Access Grafana (business metrics)
make grafana-ui
# → http://localhost:3000 (admin/admin)
```

---

## 1. Architecture Overview

### Component Stack

```
┌─────────────────────────────────────────────────────────────────────────┐
│                           Application Services                            │
│                  (API Gateway, Auction, Telemetry, Dummy)                │
└─────────────────┬───────────────────────────────────────────────────────┘
                  │ OTLP (metrics/traces/logs) via gRPC:4317 or HTTP:4318
                  ↓
┌─────────────────────────────────────────────────────────────────────────┐
│                     OpenTelemetry Collector                              │
│  - Receives: OTLP metrics, traces, logs                                 │
│  - Processes: Batching, memory limiting                                 │
│  - Exports:                                                              │
│    • Metrics → Prometheus endpoint (port 8889)                          │
│    • Traces → Jaeger (OTLP)                                             │
│    • Logs → Loki (OTLP HTTP)                                            │
└───┬─────────────────┬─────────────────┬───────────────────────────────┘
    │                 │                 │
    │ Scrape :8889    │ OTLP            │ OTLP HTTP
    ↓                 ↓                 ↓
┌──────────┐    ┌──────────┐    ┌──────────┐
│Prometheus│    │  Jaeger  │    │   Loki   │
│ (metrics)│    │ (traces) │    │ (logs)   │
└────┬─────┘    └────┬─────┘    └────┬─────┘
     │               │               │
     └───────────────┴───────────────┘
                     ↓
         ┌───────────────────────┐
         │       Grafana         │
         │  - IXP Flows Dashboard│
         │  - Auction Metrics    │
         │  - System Health      │
         └───────────────────────┘
```

### Key Components

- **OTEL Collector** - Central hub for all telemetry (metrics, traces, logs)
- **Prometheus** - Time-series metrics storage (via kube-prometheus-stack)
- **Grafana** - Unified visualization (metrics, traces, logs)
- **Jaeger** - Distributed tracing (all-in-one mode)
- **Loki** - Log aggregation

---

## 2. Access & Dashboards

### UI Access Commands

| Service | Command | URL | Login |
|---------|---------|-----|-------|
| **Grafana** | `make grafana-ui` | http://localhost:3000 | admin / admin |
| **Prometheus** | `kubectl port-forward -n observability svc/observability-kube-prometheus-prometheus 9090:9090` | http://localhost:9090 | - |
| **Jaeger** | `kubectl port-forward -n observability svc/jaeger 16686:16686` | http://localhost:16686 | - |

### Dashboards in Grafana

#### **Auction Metrics Dashboard** (`observability/ixp-auction.json`)
- **Total Auctions** - Stat panel showing cumulative auction runs (`sum(ixp_auction_runs_total)`)
- **Egress Port Latest Clearing Price** - Timeseries showing real-time clearing price per egress port (`max by (egress_port) (ixp_auction_clearing_price_latest_SGD)`)
- **Ingress Demand by Port (Real-time Rate)** - Timeseries showing bandwidth demand per ingress port (`sum by (ingress_port) (increase(ixp_auction_units_requested_kbps_total[1m]))`)

#### **Flow Metrics Dashboard** (`observability/ixp-flows.json`)
- **Ingress 1-10 Throughput** - Stat panels for each ingress port's current throughput (kbps)
- **Egress Port 0 Throughput** - Aggregated egress throughput
- **Egress Kbps (Ingress 1)** - Egress traffic for specific ingress-egress pair
- **Drop Kbps & Drop Rate %** - Shows packet drops and drop rate percentage for specific flows

#### **Bids Dashboard** (`observability/ixp-bids.json`)
- **Bid Submission Rate** - Timeseries showing bids per minute globally and per ingress port
- **Total Bids Received** - Cumulative bid count stat (`sum(ixp_bid_price_SGD_count)`)
- **Bandwidth Demand Quantiles** - Timeseries with p50, p90, p99 quantiles of bid bandwidth requests

---

## 3. Business Metrics Reference

### Metric Naming Convention

OpenTelemetry instruments are transformed to Prometheus metric names following this rule:
- **Dots to underscores:** `ixp.auction.units.requested` → `ixp_auction_units_requested`
- **Unit suffix:** `metric.WithUnit("kbps")` → `_kbps` in metric name
- **Counter suffix:** `ixp_auction_units_requested_kbps` → `ixp_auction_units_requested_kbps_total`

### Flow Metrics (from Telemetry → API)

| Metric | Unit | Labels | Example Query |
|--------|------|--------|---|
| `ixp_flow_throughput_kbps` | kbps | switch_id, ingress_port, egress_port | `ixp_flow_throughput_kbps{switch_id="sw-1",ingress_port="1"}` |
| `ixp_flow_drop_rate_percent` | % | switch_id, ingress_port, egress_port | `ixp_flow_drop_rate_percent{switch_id="sw-1"}` |
| `ixp_flow_drop_kbps` | kbps | switch_id, ingress_port, egress_port | `ixp_flow_drop_kbps{switch_id="sw-1"}` |
| `ixp_flow_egress_kbps` | kbps | switch_id, egress_port | `sum by (egress_port) (ixp_flow_egress_kbps)` |

### Auction Metrics (from Auction Runner)

#### Counters (Cumulative)

| Metric | Unit | Labels | Description |
|--------|------|--------|---|
| `ixp_auction_runs_total` | 1 | egress_port | Total auction runs executed |
| `ixp_auction_units_allocated_kbps_total` | kbps | egress_port | Total bandwidth allocated to winning bids |
| `ixp_auction_units_requested_kbps_total` | kbps | egress_port | Total bandwidth requested by eligible bids (post-reservation filter) |
| `ixp_auction_units_requested_served_bids_kbps_total` | kbps | egress_port | Bandwidth of eligible bids that won allocation |
| `ixp_auction_units_submitted_kbps_total` | kbps | egress_port | Total bandwidth from all bids pre-filter (tracks filtering effect) |

#### Gauge Metrics

| Metric | Unit | Labels | Description |
|--------|------|--------|---|
| `ixp_auction_clearing_price_latest_SGD` | SGD | egress_port | Most recent clearing price for the port |

#### Histograms

| Metric | Unit | Labels | Description |
|--------|------|--------|---|
| `ixp_auction_clearing_price_SGD_bucket` | SGD | egress_port, le | Distribution of clearing prices per auction (histogram buckets) |
| `ixp_bid_price_SGD_bucket` | SGD | ingress_port, le | Distribution of bid prices (histogram buckets) |
| `ixp_bid_units_kbps_bucket` | kbps | ingress_port, le | Distribution of bid bandwidth amounts (histogram buckets) |

### Bid Metrics (from Dummy Producer)

| Metric | Unit | Labels | Description |
|--------|------|--------|---|
| `ixp_bid_price_SGD_count` | 1 | ingress_port | Total bid count (includes all bid price observations) |
| `ixp_bid_units_kbps_count` | 1 | ingress_port | Total observations of bid bandwidth amounts |

#### Example Queries

```promql
# Auction success rate (allocated bids / total bids submitted)
sum(rate(ixp_auction_units_allocated_kbps_total[5m])) 
/ sum(rate(ixp_auction_units_submitted_kbps_total[5m]))

# Allocation effectiveness (served / requested)
sum(rate(ixp_auction_units_requested_served_bids_kbps_total[5m])) 
/ sum(rate(ixp_auction_units_requested_kbps_total[5m]))

# Median clearing price (last 5 minutes)
histogram_quantile(0.5, sum by (le) (rate(ixp_auction_clearing_price_SGD_bucket[5m])))

# Bid volume per ingress port
sum by (ingress_port) (rate(ixp_bid_price_SGD_count[1m]))

# Bandwidth demand (p50, p90, p99 quantiles)
histogram_quantile(0.50, sum by (le) (rate(ixp_bid_units_kbps_bucket[5m])))
histogram_quantile(0.90, sum by (le) (rate(ixp_bid_units_kbps_bucket[5m])))
histogram_quantile(0.99, sum by (le) (rate(ixp_bid_units_kbps_bucket[5m])))
```

---

## 4. OpenTelemetry Collector Configuration

### Overview

The OpenTelemetry Collector is deployed as the central hub for telemetry collection. All application services export metrics, traces, and logs to the collector via OTLP (OpenTelemetry Protocol).

### Receiver Configuration

The collector listens on:
- **OTLP gRPC:** `0.0.0.0:4317` - Primary receiver for OTLP metrics, traces, and logs
- **OTLP HTTP:** `0.0.0.0:4318` - Alternative HTTP endpoint for OTLP data
- **Prometheus:** `0.0.0.0:8889` - Custom endpoint for Prometheus scraping (exposes collected metrics)

### Export Configuration

```
Services (OTLP gRPC:4317)
    ↓
OTEL Collector (Processors: batch, memory limiting)
    ├→ Prometheus Exporter (endpoint :8889) → Scraped by Prometheus
    ├→ Jaeger Exporter (OTLP) → jaeger.observability.svc.cluster.local:4317
    └→ Loki Exporter (OTLP HTTP) → loki.observability.svc.cluster.local:3100/otlp
```

### Important Configuration Notes

- **Namespace:** The Prometheus exporter does NOT include a namespace prefix. Since instrument names already have `ixp.` prefix (e.g., `ixp.auction.units.requested`), this avoids the double prefix problem.
- **Processors:** Batch processor configured with 10s timeout and 1024 send batch size for efficient telemetry forwarding.
- **Labels:** Const label `environment: development` added to all metrics.

### Service DNS Names

When configuring OTEL_EXPORTER_OTLP_ENDPOINT in application deployments, use:
```
otel-collector-opentelemetry-collector.observability.svc.cluster.local:4317
```

---

## 5. Deployment & Configuration

### Make Targets

```bash
# Infrastructure
make deploy-minikube         # Start Kubernetes cluster
make deploy-kafka            # Deploy Kafka message bus
make deploy-atomix           # Deploy Atomix distributed store

# Application Services
make deploy-api              # API Gateway
make deploy-auction          # Auction runner
make deploy-telemetry        # Telemetry processor
make deploy-dummy            # Dummy producer
make deploy-agent            # Customer agents

# Observability
make deploy-observability    # Full observability stack (included in 'make all')

# Utilities
make stop                    # Stop all services
make all                     # Deploy everything
make setup                   # Configure Helm repos
```

### Configuration Files

| Path | Purpose |
|------|---------|
| `observability/values.yaml` | Prometheus, Grafana, Alertmanager config |
| `helm/opentelemetry-collector/values.yaml` | OTEL Collector pipelines and processors |
| `helm/loki/values.yaml` | Log storage and retention |
| `etc/scenario/scenario.yaml` | Topology, customers, default reservation price |
| `Makefile` | Deployment orchestration |

---

## 6. Sequence Diagrams

### Auction System

```
Customer Agent → API (bid) → Atomix BidMap
                             ↓
                       Auction Runner (runs auction)
                             ↓
                       Atomix CreditsMap, AuctionHistoryMap
                             ↓
                       Kafka → Switch (apply allocation)

Customer Agent ← API (get credits/auctions/flows) ← Atomix
```

### Telemetry & Metrics

```
Switch → Kafka telemetry → Telemetry Processor → Atomix flow store
                                                      ↓
                                              API gauge callbacks
                                                      ↓
                                              OTEL Metrics
                                                      ↓
                                              OTEL Collector → Prometheus
                                                                  ↓
                                                              Grafana
```

## 7. Troubleshooting

### No Metrics in Grafana?

```bash
# 1. Verify OTEL Collector is receiving metrics
kubectl logs -n observability -l app.kubernetes.io/name=opentelemetry-collector \
  | grep "ixp\|received\|exported"

# 1b. Verify the Atomix error counter name after OTel normalization
# (should appear as ixp_api_atomix_operation_errors_total)
kubectl port-forward -n observability svc/otel-collector-opentelemetry-collector 8889:8889
curl -s http://localhost:8889/metrics | grep -E "ixp_api_atomix_operation_errors.*total"

# 2. Check if Prometheus scrapes OTEL Collector
kubectl port-forward -n observability svc/observability-kube-prometheus-prometheus 9090:9090
# Open http://localhost:9090/targets and look for "otel-collector"

# 3. Query OTEL endpoint directly
kubectl port-forward -n observability svc/otel-collector-opentelemetry-collector 8889:8889
curl http://localhost:8889/metrics | grep ixp

# 4. Verify services are exporting metrics
kubectl logs -l app=api-gateway | grep -i "Fetching flows\|exported"
```

### Traces Not Showing in Jaeger?

```bash
# Check if services point to OTEL Collector
kubectl set env deployment/api-gateway | grep OTEL

# Verify OTEL_EXPORTER_OTLP_ENDPOINT
# Should be: otel-collector-opentelemetry-collector.observability.svc.cluster.local:4317

# Check Jaeger has received traces
kubectl logs -n observability -l app.kubernetes.io/name=jaeger | tail -20
```

### Logs Not in Loki?

```bash
# Verify Loki is running
kubectl get pods -n observability | grep loki

# Check OTEL Collector log exporter
kubectl logs -n observability -l app.kubernetes.io/name=opentelemetry-collector \
  | grep -i "logs\|loki\|exporting"

# Query Loki via Grafana (add Loki datasource: http://loki.observability.svc.cluster.local:3100)
```

### Grafana Datasources Not Working?

```bash
# Restart Grafana after config changes
kubectl rollout restart deployment -n observability observability-grafana

# Check datasource config
kubectl get configmap -n observability grafana-datasources-config \
  -o jsonpath='{.data.prometheus\.yaml}'
```

---

## 8. Health Check

Quick verification that everything is working:

```bash
# All pods should be Running
kubectl get pods -n observability

# OTEL Collector should be exporting
kubectl logs -n observability -l app.kubernetes.io/name=opentelemetry-collector --tail=5 \
  | grep -i "exported\|Exporting"

# Prometheus should have metrics from OTEL
kubectl port-forward -n observability svc/observability-kube-prometheus-prometheus 9090:9090
# Open http://localhost:9090 and run: up{job="otel-collector"}
# Should return 1 (healthy)

# Grafana should be accessible
kubectl port-forward -n observability svc/observability-grafana 3000:80
# Open http://localhost:3000, login admin/admin
```

---

## 9. Tips & Best Practices

1. **Use Grafana for everything** - Metrics, logs, and traces all accessible from one UI
2. **Check OTEL Collector first** - It's the central hub; if metrics aren't flowing, start there
3. **Correlate by trace ID** - Logs from `otelslog` bridge include trace context automatically
4. **Watch resource usage** - OTEL Collector has 512MB memory limit configured
5. **Understand alert grouping** - `group_by: ["alertname", "egress_port"]` means one alert per port
6. **Test alert resolution** - `send_resolved: true` is crucial for the loop to relax back to baseline

---

## References

- **Main README:** [../README.md](../README.md)
- **Makefile Targets:** [../Makefile](../Makefile)
- **Scenario Config:** [../etc/scenario/scenario.yaml](../etc/scenario/scenario.yaml)
- **Observability Values:** [../observability/values.yaml](../observability/values.yaml)

