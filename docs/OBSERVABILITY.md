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

- **observability/ixp-flows.json** - Flow throughput, drop rates, egress utilization
- **observability/ixp-auction.json** - Auction runs, bids, clearing prices, allocation ratios
- **observability/ixp-bids.json** - Bid submissions and trends

---

## 3. Business Metrics Reference

### Flow Metrics (from Telemetry → API)

| Metric | Unit | Labels | Example Query |
|--------|------|--------|---|
| `ixp_flow_throughput_kbps` | kbps | switch_id, ingress_port, egress_port | `sum(ixp_flow_throughput_kbps{switch_id="sw-1"})` |
| `ixp_flow_drop_rate_percent` | % | switch_id, ingress_port, egress_port | `avg(ixp_flow_drop_rate_percent)` |
| `ixp_flow_drop_kbps` | kbps | switch_id, ingress_port, egress_port | `sum(ixp_flow_drop_kbps)` |
| `ixp_flow_egress_kbps` | kbps | switch_id, egress_port | `sum(ixp_flow_egress_kbps) by (egress_port)` |

### Auction Metrics (from Auction Runner)

#### Counters

| Metric | Unit | Labels | Description |
|--------|------|--------|---|
| `ixp_auction_runs_total` | 1 | egress_port | Total auction runs executed |
| `ixp_auction_bids_total` | 1 | egress_port | Total bids observed |
| `ixp_auction_bids_allocated_total` | 1 | egress_port | Bids with allocation > 0 |
| `ixp_auction_units_requested_kbps_total` | kbps | egress_port | Total bandwidth requested |
| `ixp_auction_units_allocated_kbps_total` | kbps | egress_port | Total bandwidth allocated |

#### Histograms

| Metric | Unit | Labels | Description |
|--------|------|--------|---|
| `ixp_auction_clearing_price_SGD` | SGD | egress_port | Distribution of clearing prices per auction |
| `ixp_auction_bid_allocation_ratio` | ratio | egress_port | Ratio of allocated units to requested units |

#### Example Queries

```promql
# Allocation success rate
sum(rate(ixp_auction_bids_allocated_total[1m])) 
/ sum(rate(ixp_auction_bids_total[1m]))

# Allocation ratio (units)
sum(rate(ixp_auction_units_allocated_kbps_total[5m])) 
/ sum(rate(ixp_auction_units_requested_kbps_total[5m]))

# Median clearing price (last 5 minutes)
histogram_quantile(0.5, sum by (le) (rate(ixp_auction_clearing_price_SGD_bucket[5m])))

# Bid volume per egress port
sum by (egress_port) (rate(ixp_auction_bids_total[1m]))
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

