#!/bin/bash
# Export resource metrics for benchmark comparison
# Usage: ./benchmark-metrics-export.sh <scenario-name> <start-timestamp> <end-timestamp>

set -e

SCENARIO=$1
START_TIME=$2
END_TIME=$3
OUTPUT_DIR="data"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
OUTPUT_FILE="${OUTPUT_DIR}/benchmark-scenario-${SCENARIO}-metrics.json"

if [ -z "$SCENARIO" ] || [ -z "$START_TIME" ] || [ -z "$END_TIME" ]; then
    echo "ERROR: Missing required parameters"
    echo "Usage: $0 <scenario-name> <start-timestamp> <end-timestamp>"
    exit 1
fi

mkdir -p "$OUTPUT_DIR"

DURATION=$((END_TIME - START_TIME))

PROM_URL="http://localhost:9090"

echo "Exporting metrics for scenario: $SCENARIO"
echo "Time range: $(date -r $START_TIME) to $(date -r $END_TIME)"
echo "Duration: ${DURATION}s"

# Function to query Prometheus and format as JSON
query_prom() {
    local query=$1
    local output_key=$2

    printf '"%s":' "$output_key"
    curl -sG "${PROM_URL}/api/v1/query_range" \
        --data-urlencode "query=${query}" \
        --data-urlencode "start=${START_TIME}" \
        --data-urlencode "end=${END_TIME}" \
        --data-urlencode "step=15s" 2>/dev/null
}

# Build JSON output
echo "Querying Prometheus..."
{
    echo "{"
    echo '"metadata":{'
    echo "\"scenario\":\"${SCENARIO}\","
    echo "\"start_time\":${START_TIME},"
    echo "\"end_time\":${END_TIME},"
    echo "\"duration_seconds\":${DURATION},"
    echo "\"timestamp\":\"$(date -Iseconds)\""
    echo "},"

    echo "\"system_metrics\":{"

    # Service-level CPU usage
    echo "\"service_cpu\":{"
    query_prom 'rate(container_cpu_usage_seconds_total{namespace="default",pod=~"api-gateway.*|auction-runner.*|telemetry-service.*|customer-agent.*|dummy-producer.*",id!~".*/kubepods.slice/kubepods.slice"}[1m])' "data"
    echo "},"

    # Service-level memory usage
    echo "\"service_memory\":{"
    query_prom 'container_memory_working_set_bytes{namespace="default",pod=~"api-gateway.*|auction-runner.*|telemetry-service.*|customer-agent.*|dummy-producer.*",id!~".*/kubepods.slice/kubepods.slice"}' "data"
    echo "},"

    # OTel Collector CPU (Scenario A only)
    echo "\"otel_collector_cpu\":{"
    query_prom 'rate(container_cpu_usage_seconds_total{namespace="observability",pod=~"otel-collector.*"}[1m])' "data"
    echo "},"

    # OTel Collector memory
    echo "\"otel_collector_memory\":{"
    query_prom 'container_memory_working_set_bytes{namespace="observability",pod=~"otel-collector.*"}' "data"
    echo "},"

    # Total cluster CPU (all namespaces)
    echo "\"cluster_total_cpu\":{"
    query_prom 'sum(rate(container_cpu_usage_seconds_total{id=~"/kubepods.*"}[1m]))' "data"
    echo "},"

    # Total cluster memory
    echo "\"cluster_total_memory\":{"
    query_prom 'sum(container_memory_working_set_bytes{id=~"/kubepods.*"})' "data"
    echo "}"

    echo "},"

    # Validation metrics (business logic)
    echo "\"validation_metrics\":{"

    # Auction runs (should be same in both scenarios)
    echo "\"auction_runs\":{"
    query_prom 'ixp_auction_runs_total' "data"
    echo "},"

    # Flow throughput (should be same)
    echo "\"flow_throughput\":{"
    query_prom 'ixp_flow_throughput' "data"
    echo "},"

    # Clearing price (should be same)
    echo "\"clearing_price\":{"
    query_prom 'ixp_auction_clearing_price' "data"
    echo "}"

    echo "}"
    echo "}"
} > "$OUTPUT_FILE"

echo "✅ Metrics exported to $OUTPUT_FILE"

# Validate JSON
if command -v jq &> /dev/null; then
    if jq empty "$OUTPUT_FILE" 2>/dev/null; then
        echo "✅ JSON validation passed"
    else
        echo "⚠️  WARNING: JSON validation failed"
    fi
fi
