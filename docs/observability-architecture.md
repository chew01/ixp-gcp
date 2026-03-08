# Unified Observability Architecture

## Overview

The observability stack has been unified to provide comprehensive monitoring, tracing, and logging for the IXP SDN system while maintaining focus on business metrics (flowThroughput, flowDropRate).

## Architecture Flow

```
┌─────────────────────────────────────────────────────────────────────────┐
│                           Application Services                            │
│                  (API Gateway, Auction, Telemetry, Dummy)                │
└─────────────────┬───────────────────────────────────────────────────────┘
                  │ Push OTLP (metrics/traces/logs)
                  │ via gRPC (4317) or HTTP (4318)
                  ↓
┌─────────────────────────────────────────────────────────────────────────┐
│                     OpenTelemetry Collector                              │
│                        (otel-collector)                                  │
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
│          │    │ All-in-1 │    │          │
│(Storage) │    │ (Traces) │    │  (Logs)  │
└────┬─────┘    └────┬─────┘    └────┬─────┘
     │               │               │
     │ PromQL        │ Jaeger API    │ LogQL
     └───────────────┴───────────────┘
                     ↓
         ┌───────────────────────┐
         │       Grafana         │
         │  (Visualization)      │
         │  - IXP Flows Dashboard│
         │  - System Metrics     │
         │  - Traces Explorer    │
         │  - Logs Explorer      │
         └───────────────────────┘
```

## Components

### 1. **OpenTelemetry Collector** (`otel-collector`)
- **Purpose**: Central telemetry ingestion and routing
- **Receives**: OTLP metrics, traces, and logs from all services
- **Exports**:
  - Metrics via Prometheus exporter (scraped by Prometheus)
  - Traces to Jaeger via OTLP
  - Logs to Loki via OTLP HTTP
- **Endpoint**: `otel-collector.monitoring.svc.cluster.local:4317` (gRPC)

### 2. **Prometheus**
- **Purpose**: Metrics storage and querying (via kube-prometheus-stack)
- **Scrapes**: OTEL Collector's Prometheus endpoint (`:8889`)
- **Stores**: Business metrics (flowThroughput, flowDropRate) and system metrics
- **UI**: `http://localhost:9090` (via port-forward)

### 3. **Grafana**
- **Purpose**: Unified visualization dashboard
- **Datasources**:
  - Prometheus (default) - for metrics
  - Jaeger - for distributed traces
  - Loki - for logs
- **Dashboards**:
  - IXP Flows Dashboard - business metrics (flowThroughput per flow)
- **UI**: `http://localhost:3000` (admin/admin)

### 4. **Jaeger** (All-in-One)
- **Purpose**: Distributed tracing
- **Receives**: Traces from OTEL Collector via OTLP
- **Storage**: In-memory (development mode)
- **UI**: `http://localhost:16686` (via port-forward)

### 5. **Loki**
- **Purpose**: Log aggregation
- **Receives**: Logs from OTEL Collector via OTLP HTTP
- **Storage**: Filesystem (development mode)
- **Query**: Via Grafana's Loki datasource

## Business Metrics

The system tracks the following business metrics:

### `ixp_flow_throughput`
- **Description**: Flow throughput in Kbps
- **Labels**:
  - `switch_id`: Switch identifier (e.g., "sw-1")
  - `ingress_port`: Ingress port number
  - `egress_port`: Egress port number
- **Source**: API Gateway observes Atomix flow store
- **Collection**: Observable Gauge (auto-callback every 10s)

### `ixp_flow_drop_rate` (Planned)
- **Description**: Flow packet drop rate as percentage
- **Labels**: Same as throughput
- **Status**: Not yet implemented

## Deployment

### Initial Setup
```bash
# Add Helm repositories
make setup
```

### Deploy Everything
```bash
# Deploy infrastructure (Minikube, Kafka, Atomix, Monitoring)
# and all services (API, Auction, Dummy, Telemetry)
make all
```

### Deploy Only Observability Stack
```bash
make deploy-monitoring
```

### Access UIs
```bash
# Grafana (business metrics)
kubectl port-forward -n monitoring svc/monitoring-grafana 3000:80
# Visit: http://localhost:3000 (admin/admin)

# Prometheus (raw metrics)
kubectl port-forward -n monitoring svc/monitoring-kube-prometheus-prometheus 9090:9090
# Visit: http://localhost:9090

# Jaeger (traces)
kubectl port-forward -n monitoring svc/jaeger-all-in-one-query 16686:16686
# Visit: http://localhost:16686
```

Or use the shortcut:
```bash
make grafana-ui
```

## Key Changes from Previous Setup

### Before (Separated Stacks)
- **Teammate's Stack**: Prometheus + Grafana with ServiceMonitor scraping `/metrics` endpoint
- **Your Stack**: OTEL Collector + Jaeger + Loki with separate deployment script

### After (Unified Stack)
- **Single deployment** via `make deploy-monitoring`
- **OTEL-first approach**: All telemetry goes through OTEL Collector
- **Prometheus scrapes OTEL Collector** instead of individual services
- **All observability tools in one namespace**: `monitoring`
- **Automatic datasource configuration**: Grafana auto-discovers Prometheus, Jaeger, Loki
- **Preserved business metrics**: flowThroughput dashboard still works

## Application Configuration

Services should send telemetry to OTEL Collector:

```go
// In your otel-init.go or similar
endpoint := "otel-collector.monitoring.svc.cluster.local:4317"
```

The API Gateway uses Observable Gauges to push metrics:
- Metrics are collected via callback function
- OTEL SDK automatically pushes to collector every 10s
- No explicit `/metrics` endpoint needed

## Troubleshooting

### Check OTEL Collector logs
```bash
kubectl logs -n monitoring -l app.kubernetes.io/name=opentelemetry-collector -f
```

### Check if Prometheus is scraping OTEL Collector
```bash
kubectl port-forward -n monitoring svc/monitoring-kube-prometheus-prometheus 9090:9090
# Visit: http://localhost:9090/targets
# Look for "otel-collector" job
```

### Verify metrics are flowing
```bash
# Check OTEL Collector Prometheus endpoint directly
kubectl port-forward -n monitoring svc/otel-collector 8889:8889
curl http://localhost:8889/metrics | grep ixp_flow_throughput
```

### Check Grafana datasources
```bash
# Grafana UI → Configuration → Data Sources
# Should see: Prometheus (default), Jaeger, Loki
```

## Files Changed

- **`monitoring/values.yaml`**: Comprehensive kube-prometheus-stack configuration
- **`helm/opentelemetry-collector/values.yaml`**: Added Prometheus exporter
- **`Makefile`**: Unified `deploy-monitoring` target
- **`api/server.go`**: Already updated to use Observable Gauges (no changes needed)

## Next Steps

To implement `flowDropRate` metric:
1. Add observable gauge in `api/server.go` similar to `flowThroughput`
2. Fetch drop rate data from Atomix or compute from flow statistics
3. Metric will automatically flow through OTEL → Prometheus → Grafana
4. Update IXP Flows Dashboard to display drop rate panel
