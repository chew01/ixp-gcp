#!/bin/bash
# Master benchmark orchestration script
# Runs both telemetry scenarios and collects comparative metrics
# Usage: ./run-benchmark.sh [experiment-id] [duration-seconds]

set -e

EXPERIMENT=${1:-"1"}   # Default to experiment 1 (baseline)
DURATION=${2:-900}     # Default 15 minutes (900 seconds)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "=========================================="
echo "Telemetry Architecture Benchmark"
echo "=========================================="
echo "Experiment: ${EXPERIMENT}"
echo "Duration per scenario: ${DURATION} seconds ($(($DURATION / 60)) minutes)"
echo "Start time: $(date)"
echo ""

# Ensure cluster is ready
echo "==> Checking cluster status..."
if ! kubectl cluster-info &>/dev/null; then
    echo "ERROR: Cluster not available. Please run: minikube start --cpus=4 --memory=8192"
    exit 1
fi

echo "✅ Cluster accessible"

# Skip deployment - assume infrastructure is already running
echo ""
echo "==> Skipping deployment (using existing infrastructure)..."
echo "⚠️  Make sure you've already run 'make all experiment=${EXPERIMENT}' before running this script"

# Wait for Prometheus to be ready
echo ""
echo "==> Checking Prometheus is ready..."
kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=prometheus -n observability --timeout=60s || {
    echo "ERROR: Prometheus is not ready. Please deploy infrastructure first."
    exit 1
}

echo "✅ Prometheus running"

# Brief stabilization period
echo ""
echo "==> Waiting 10s for any pending changes to stabilize..."
sleep 10

# Check all critical pods are running
echo "==> Verifying pod status..."
EXPECTED_PODS=("api-gateway" "auction-runner" "telemetry-service" "dummy-producer")
for pod in "${EXPECTED_PODS[@]}"; do
    if ! kubectl get pods -n default | grep "$pod" | grep -q Running; then
        echo "WARNING: $pod may not be running properly"
        kubectl get pods -n default | grep "$pod" || echo "  Pod not found"
    else
        echo "  ✅ $pod running"
    fi
done

echo ""
echo "=========================================="
echo "SCENARIO A: Via OTel Collector"
echo "=========================================="

# Deploy Scenario A
echo "==> Configuring services for Scenario A (OTLP → OTel Collector)..."
cd "$PROJECT_ROOT"
make benchmark-scenario-a

echo "==> Waiting for rollout to complete..."
kubectl rollout status deployment/api-gateway --timeout=120s
kubectl rollout status deployment/auction-runner --timeout=120s
kubectl rollout status deployment/telemetry-service --timeout=120s

echo "==> Stabilization period (30s)..."
sleep 30

echo "==> Starting workload collection for ${DURATION} seconds..."
START_A=$(date +%s)
echo "Start time: $(date -r $START_A)" | tee data/benchmark-scenario-a.log
echo "Duration: ${DURATION}s ($(($DURATION / 60)) minutes)" | tee -a data/benchmark-scenario-a.log
echo "Workload: Experiment ${EXPERIMENT}" | tee -a data/benchmark-scenario-a.log
echo ""
echo "Collecting metrics... (this will take $(($DURATION / 60)) minutes)"

# Wait for full duration
sleep $DURATION

END_A=$(date +%s)
echo ""
echo "End time: $(date -r $END_A)" | tee -a data/benchmark-scenario-a.log

# Port-forward Prometheus and export metrics
echo "==> Exporting metrics for Scenario A..."
kubectl port-forward -n observability svc/observability-kube-prometh-prometheus 9090:9090 &
PF_PID=$!
sleep 5  # Wait for port-forward to establish

"$SCRIPT_DIR/04-benchmark-metrics-export.sh" a $START_A $END_A

kill $PF_PID 2>/dev/null || true

echo "✅ Scenario A complete"

# Cool-down period
echo ""
echo "==> Cool-down period (30s)..."
sleep 30

echo ""
echo "=========================================="
echo "SCENARIO B: Direct Native Clients"
echo "=========================================="

# Deploy Scenario B
echo "==> Configuring services for Scenario B (Direct → Backends)..."
cd "$PROJECT_ROOT"
make benchmark-scenario-b

echo "==> Waiting for rollout to complete..."
kubectl rollout status deployment/api-gateway --timeout=120s
kubectl rollout status deployment/auction-runner --timeout=120s
kubectl rollout status deployment/telemetry-service --timeout=120s

echo "==> Stabilization period (30s)..."
sleep 30

echo "==> Starting workload collection for ${DURATION} seconds..."
START_B=$(date +%s)
echo "Start time: $(date -r $START_B)" | tee data/benchmark-scenario-b.log
echo "Duration: ${DURATION}s ($(($DURATION / 60)) minutes)" | tee -a data/benchmark-scenario-b.log
echo "Workload: Experiment ${EXPERIMENT}" | tee -a data/benchmark-scenario-b.log
echo ""
echo "Collecting metrics... (this will take $(($DURATION / 60)) minutes)"

# Wait for full duration
sleep $DURATION

END_B=$(date +%s)
echo ""
echo "End time: $(date -r $END_B)" | tee -a data/benchmark-scenario-b.log

# Export metrics
echo "==> Exporting metrics for Scenario B..."
kubectl port-forward -n observability svc/observability-kube-prometh-prometheus 9090:9090 &
PF_PID=$!
sleep 5

"$SCRIPT_DIR/04-benchmark-metrics-export.sh" b $START_B $END_B

kill $PF_PID 2>/dev/null || true

echo "✅ Scenario B complete"

# Analysis
echo ""
echo "=========================================="
echo "Analysis"
echo "=========================================="

if [ -d "$SCRIPT_DIR/benchmark-analysis" ]; then
    echo "==> Running analysis tool..."
    cd "$SCRIPT_DIR/benchmark-analysis"
    go run . ../../data/benchmark-scenario-a-metrics.json ../../data/benchmark-scenario-b-metrics.json
else
    echo "⚠️  Analysis tool not found (will create in next step)"
    echo "Results available at:"
    echo "  - data/benchmark-scenario-a-metrics.json"
    echo "  - data/benchmark-scenario-b-metrics.json"
fi

echo ""
echo "=========================================="
echo "✅ Benchmark Complete!"
echo "=========================================="
echo "Total runtime: $(($(($(date +%s) - START_A)) / 60)) minutes"
echo ""
echo "Results:"
echo "  - Scenario A log: data/benchmark-scenario-a.log"
echo "  - Scenario B log: data/benchmark-scenario-b.log"
echo "  - Scenario A metrics: data/benchmark-scenario-a-metrics.json"
echo "  - Scenario B metrics: data/benchmark-scenario-b-metrics.json"
if [ -f "data/benchmark-report.md" ]; then
    echo "  - Report: data/benchmark-report.md"
fi
echo ""
