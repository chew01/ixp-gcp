# Observability Stack Migration Guide

## Summary

The observability stacks have been successfully merged into a unified deployment. You can now use `make all` to deploy the entire system including monitoring.

## What Changed

### Before
- **Two separate stacks**: One for business metrics (Prometheus + Grafana), one for full observability (OTEL + Jaeger + Loki)
- **Two deployment methods**: `make deploy-monitoring` and `./deploy-observability.sh`
- **Direct scraping**: Prometheus scraped `/metrics` endpoint from API service via ServiceMonitor

### After
- **Single unified stack**: All observability tools deployed together
- **One deployment method**: `make deploy-monitoring` (included in `make all`)
- **OTEL-first approach**: Metrics pushed to OTEL Collector, Prometheus scrapes from OTEL

## Architecture Changes

```
BEFORE:
API Service :8080/metrics ← Prometheus (scrape)
                           ↓
                        Grafana (query)

AFTER:
API Service → OTEL Collector :8889 ← Prometheus (scrape)
              ↓              ↓              ↓
           Jaeger         Loki          Grafana (query all)
```

## Configuration Files Updated

1. **`monitoring/values.yaml`**
   - Expanded to full kube-prometheus-stack configuration
   - Added OTEL Collector scrape config
   - Enabled Grafana dashboard sidecar
   - Configured datasources (Prometheus, Jaeger, Loki)

2. **`helm/opentelemetry-collector/values.yaml`**
   - Changed metrics exporter from `otlphttp` to `prometheus`
   - Added Prometheus endpoint on port 8889
   - Added processors (batch, memory_limiter)
   - Fixed Jaeger endpoints for all-in-one deployment

3. **`Makefile`**
   - Updated `deploy-monitoring` to deploy full stack
   - Added Jaeger, Loki, OTEL Collector deployments
   - Updated `setup` to include all Helm repos
   - Fixed `grafana-ui` port-forward command
   - Added helpful output with access instructions

4. **`deploy-observability.sh`**
   - Deprecated with clear migration message
   - Redirects users to `make deploy-monitoring`

5. **`README.md`**
   - Added observability section
   - Documented business metrics
   - Added link to architecture docs

## Migration Steps (If Redeploying)

If you have an existing deployment and want to migrate:

### Option 1: Clean Deployment (Recommended)
```bash
# Stop current cluster
make stop

# Setup Helm repos
make setup

# Deploy everything fresh
make all
```

### Option 2: Update Only Monitoring
```bash
# Delete existing monitoring namespace
kubectl delete namespace monitoring

# Redeploy with new unified stack
make deploy-monitoring
```

## Verification Steps

After deployment, verify everything works:

### 1. Check All Pods are Running
```bash
kubectl get pods -n monitoring
```

Expected pods:
- `monitoring-kube-prometheus-operator-*` (Running)
- `monitoring-prometheus-*` (Running)
- `monitoring-grafana-*` (Running)
- `otel-collector-*` (Running)
- `jaeger-all-in-one-*` (Running)
- `loki-*` (Running)

### 2. Verify OTEL Collector is Receiving Metrics
```bash
kubectl logs -n monitoring -l app.kubernetes.io/name=opentelemetry-collector | grep "ixp"
```

Should see metrics with "ixp" prefix being processed.

### 3. Verify Prometheus is Scraping OTEL Collector
```bash
# Port-forward Prometheus
kubectl port-forward -n monitoring svc/monitoring-kube-prometheus-prometheus 9090:9090

# Open browser: http://localhost:9090/targets
# Look for "otel-collector" job - should be "UP"
```

### 4. Verify Metrics in Prometheus
```bash
# In Prometheus UI, query:
ixp_flow_throughput

# Or use command line:
kubectl port-forward -n monitoring svc/monitoring-kube-prometheus-prometheus 9090:9090 &
curl -s 'http://localhost:9090/api/v1/query?query=ixp_flow_throughput' | jq
```

### 5. Verify Grafana Datasources
```bash
# Access Grafana
make grafana-ui

# In browser: http://localhost:3000
# Login: admin / admin
# Go to: Configuration → Data Sources
```

Expected datasources:
- ✅ Prometheus (default) - Connected
- ✅ Jaeger - Connected
- ✅ Loki - Connected

### 6. Verify IXP Flows Dashboard
```bash
# In Grafana UI:
# Go to: Dashboards → IXP Dashboards → IXP Flows

# Should see flowThroughput metrics displayed
```

### 7. Verify Traces (if services are running)
```bash
# Port-forward Jaeger
kubectl port-forward -n monitoring svc/jaeger-all-in-one-query 16686:16686

# Open browser: http://localhost:16686
# Select service: api-gateway
# Click "Find Traces"
```

## Troubleshooting

### Metrics not showing in Grafana

**Check OTEL Collector logs:**
```bash
kubectl logs -n monitoring -l app.kubernetes.io/name=opentelemetry-collector -f
```

**Check if API is pushing metrics:**
```bash
kubectl logs -l app=api-gateway | grep -i metric
```

**Verify OTEL Collector endpoint:**
```bash
# Should resolve correctly
kubectl run -it --rm debug --image=busybox --restart=Never -- \
  nslookup otel-collector.monitoring.svc.cluster.local
```

### Prometheus not scraping OTEL Collector

**Check Prometheus targets:**
```bash
kubectl port-forward -n monitoring svc/monitoring-kube-prometheus-prometheus 9090:9090
# Visit: http://localhost:9090/targets
```

**Check OTEL Collector Prometheus endpoint:**
```bash
kubectl port-forward -n monitoring svc/otel-collector 8889:8889
curl http://localhost:8889/metrics | grep ixp_flow_throughput
```

### Grafana datasources not working

**Check ConfigMap is applied:**
```bash
kubectl get configmap -n monitoring grafana-datasources-config -o yaml
```

**Check Grafana logs:**
```bash
kubectl logs -n monitoring -l app.kubernetes.io/name=grafana | grep -i datasource
```

**Restart Grafana pods:**
```bash
kubectl rollout restart deployment -n monitoring monitoring-grafana
```

## Code Changes in Services (Already Done)

The API service has already been updated to use OpenTelemetry Observable Gauges instead of Prometheus direct instrumentation. No additional code changes are needed.

**Key changes in `api/server.go`:**
- Uses `localotel.Meter.Float64ObservableGauge()` for metrics
- Callback function fetches flow data from Atomix every 10s
- Metrics automatically pushed to OTEL Collector
- No explicit `/metrics` endpoint needed

## Benefits of Unified Stack

1. **Single deployment command**: `make deploy-monitoring` or `make all`
2. **Consistent configuration**: All configs in `monitoring/` and `helm/` folders
3. **Complete observability**: Metrics, traces, and logs in one place
4. **Vendor-neutral**: OTEL-first approach allows easy backend swapping
5. **Better resource usage**: Single Prometheus, single Grafana for everything
6. **Easier debugging**: All telemetry flows through OTEL Collector with debug logging

## Next Steps

### For Your Teammate
The business metrics (flowThroughput, flowDropRate) work exactly the same way:
- Same Grafana dashboard (IXP Flows)
- Same metrics names and labels
- Just deployed together with traces and logs now

### For You
You now have a complete observability stack:
- Query business metrics in Prometheus/Grafana
- Trace requests across services in Jaeger
- Search logs in Loki (via Grafana)
- All telemetry centralized through OTEL Collector

## Questions?

See the full architecture documentation:
- [docs/observability-architecture.md](observability-architecture.md)

Or view the configuration files:
- `monitoring/values.yaml` - Prometheus + Grafana config
- `helm/opentelemetry-collector/values.yaml` - OTEL Collector config
- `helm/loki/values.yaml` - Loki config
- `Makefile` - Deployment targets
