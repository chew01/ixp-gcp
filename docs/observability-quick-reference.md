# Observability Quick Reference

## 🚀 Quick Start

```bash
# First time setup
make setup

# Deploy everything
make all

# Access Grafana (business metrics)
make grafana-ui
# → http://localhost:3000 (admin/admin)
```

## 📊 Access UIs

| Service | Command | URL | Credentials |
|---------|---------|-----|-------------|
| **Grafana** | `make grafana-ui` | http://localhost:3000 | admin / admin |
| **Prometheus** | `kubectl port-forward -n monitoring svc/monitoring-kube-prometheus-prometheus 9090:9090` | http://localhost:9090 | - |
| **Jaeger** | `kubectl port-forward -n monitoring svc/jaeger-all-in-one-query 16686:16686` | http://localhost:16686 | - |

## 🔍 Business Metrics

### Metric Names
- `ixp_flow_throughput` - Flow throughput in Kbps
- `ixp_flow_drop_rate` - Flow drop rate (planned)

### Labels
- `switch_id` - Switch identifier (e.g., "sw-1")
- `ingress_port` - Ingress port number
- `egress_port` - Egress port number

### Example Queries (Prometheus/Grafana)

```promql
# All flows throughput
ixp_flow_throughput

# Specific switch
ixp_flow_throughput{switch_id="sw-1"}

# Specific flow
ixp_flow_throughput{switch_id="sw-1",ingress_port="1",egress_port="10"}

# Total throughput across all flows
sum(ixp_flow_throughput)

# Average throughput by switch
avg(ixp_flow_throughput) by (switch_id)
```

## 🛠️ Common Commands

### Deployment
```bash
# Deploy monitoring stack only
make deploy-monitoring

# Deploy everything (infra + services + monitoring)
make all

# Redeploy a specific service
make deploy-api
make deploy-auction
make deploy-telemetry
make deploy-dummy
```

### Debugging
```bash
# Check all monitoring pods
kubectl get pods -n monitoring

# View OTEL Collector logs
kubectl logs -n monitoring -l app.kubernetes.io/name=opentelemetry-collector -f

# View API Gateway logs
kubectl logs -l app=api-gateway -f

# View Prometheus logs
kubectl logs -n monitoring -l app.kubernetes.io/name=prometheus

# View Grafana logs
kubectl logs -n monitoring -l app.kubernetes.io/name=grafana
```

### Service Endpoints

```bash
# OTEL Collector endpoint (from services)
OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector.monitoring.svc.cluster.local:4317

# Prometheus OTEL scrape endpoint
http://otel-collector.monitoring.svc.cluster.local:8889/metrics

# Jaeger collector endpoint
http://jaeger-all-in-one-collector.monitoring.svc.cluster.local:4317

# Loki endpoint
http://loki.monitoring.svc.cluster.local:3100/otlp
```

## 🔧 Troubleshooting

### No metrics in Grafana?

```bash
# 1. Check if OTEL Collector is receiving metrics
kubectl logs -n monitoring -l app.kubernetes.io/name=opentelemetry-collector | grep ixp

# 2. Check if Prometheus is scraping OTEL Collector
kubectl port-forward -n monitoring svc/monitoring-kube-prometheus-prometheus 9090:9090
# Visit: http://localhost:9090/targets (look for "otel-collector" job)

# 3. Check OTEL endpoint directly
kubectl port-forward -n monitoring svc/otel-collector 8889:8889
curl http://localhost:8889/metrics | grep ixp_flow_throughput

# 4. Verify API is pushing metrics
kubectl logs -l app=api-gateway | grep -i "Fetching flow"
```

### Grafana datasources not working?

```bash
# Check ConfigMap
kubectl get configmap -n monitoring grafana-datasources-config

# Restart Grafana
kubectl rollout restart deployment -n monitoring monitoring-grafana
```

### Traces not showing in Jaeger?

```bash
# Check if services are configured to send to OTEL Collector
kubectl set env deployment/api-gateway -n default | grep OTEL

# Check Jaeger logs
kubectl logs -n monitoring -l app.kubernetes.io/name=jaeger
```

## 📁 Configuration Files

| File | Purpose |
|------|---------|
| `monitoring/values.yaml` | Prometheus + Grafana configuration |
| `helm/opentelemetry-collector/values.yaml` | OTEL Collector pipelines |
| `helm/loki/values.yaml` | Loki log aggregation config |
| `helm/grafana/values.yaml` | Grafana-specific settings (optional) |
| `monitoring/ixp-flows.json` | IXP Flows Grafana dashboard |
| `Makefile` | Deployment targets |

## 🔗 Documentation

- **Architecture Overview**: [docs/observability-architecture.md](observability-architecture.md)
- **Migration Guide**: [docs/observability-migration.md](observability-migration.md)
- **Main README**: [../README.md](../README.md)

## 📝 Quick Health Check

Run these commands to verify the stack is healthy:

```bash
# All pods should be Running
kubectl get pods -n monitoring

# OTEL Collector should be receiving data
kubectl logs -n monitoring -l app.kubernetes.io/name=opentelemetry-collector --tail=20 | grep -i export

# Prometheus should have metrics
kubectl exec -n monitoring prometheus-monitoring-kube-prometheus-prometheus-0 -- \
  promtool query instant http://localhost:9090 'up{job="otel-collector"}'

# Grafana should be accessible
curl -I http://localhost:3000 (after port-forward)
```

## 💡 Tips

1. **Use Grafana for everything**: Metrics, logs, and traces all in one UI
2. **Check OTEL Collector first**: It's the central hub for all telemetry
3. **Enable debug logs**: OTEL Collector already has debug exporter enabled
4. **Correlate data**: Use trace IDs to link metrics, logs, and traces
5. **Monitor resource usage**: OTEL has memory limiter configured (512MB)

## 🎯 Development Workflow

```bash
# 1. Start fresh cluster
make stop
make all

# 2. Wait for everything to be ready (~2-3 minutes)
kubectl wait --for=condition=ready pod -l app=api-gateway --timeout=300s

# 3. Access Grafana
make grafana-ui

# 4. Generate some traffic (bids, flows, etc.)
# ... your API calls ...

# 5. View metrics in Grafana
# Navigate to: IXP Dashboards → IXP Flows

# 6. View traces in Jaeger
kubectl port-forward -n monitoring svc/jaeger-all-in-one-query 16686:16686

# 7. Check logs if needed
kubectl logs -l app=api-gateway -f
```
