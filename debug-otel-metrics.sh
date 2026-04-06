#!/bin/bash

##############################################################################
# DEBUG OTEL METRICS FLOW 
# 
# Monitors if OTel Collector is receiving metrics from api-gateway
# Specifically watches for: ixp_atomix_operation_errors_total
#
# Usage: ./debug-otel-metrics.sh
#
# This script will:
# 1. Port-forward to OTel Collector Prometheus endpoint (8889)
# 2. Display live metrics every 3 seconds
# 3. Highlight when errors appear in the pipeline
##############################################################################

set -e

COLLECTOR_PORT=8889
COLLECTOR_NS="observability"
COLLECTOR_SVC="otel-collector-opentelemetry-collector"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Cleanup function
cleanup() {
  echo -e "\n${YELLOW}[CLEANUP]${NC} Stopping monitoring..."
  exit 0
}
trap cleanup EXIT INT TERM

echo "═══════════════════════════════════════════════════════════════"
echo "🔍 OTEL METRICS FLOW DEBUG"
echo "═══════════════════════════════════════════════════════════════"
echo ""

# Step 1: Verify OTel Collector is running
echo -e "${BLUE}[1/3]${NC} Checking OTel Collector status..."
COLLECTOR_PODS=$(kubectl get pods -n $COLLECTOR_NS -l app.kubernetes.io/name=opentelemetry-collector --no-headers 2>/dev/null | wc -l)
if [ "$COLLECTOR_PODS" -lt 1 ]; then
  echo -e "${RED}❌ OTel Collector not found!${NC}"
  echo "   Deploy with: make deploy-observability"
  exit 1
fi
echo -e "${GREEN}✅ OTel Collector running (${COLLECTOR_PODS} pods)${NC}"
echo ""

# Step 2: Start port-forward in background
echo -e "${BLUE}[2/3]${NC} Setting up port-forward to OTel Collector metrics endpoint..."
kubectl port-forward -n $COLLECTOR_NS svc/$COLLECTOR_SVC $COLLECTOR_PORT:8889 > /dev/null 2>&1 &
PF_PID=$!

# Wait for port-forward to be ready
RETRY=0
MAX_RETRIES=30
while [ $RETRY -lt $MAX_RETRIES ]; do
  if curl -s "http://localhost:$COLLECTOR_PORT/metrics" > /dev/null 2>&1; then
    echo -e "${GREEN}✅ Port-forward ready on localhost:${COLLECTOR_PORT}${NC}"
    break
  fi
  RETRY=$((RETRY + 1))
  sleep 1
done

if [ $RETRY -eq $MAX_RETRIES ]; then
  echo -e "${RED}❌ Port-forward failed!${NC}"
  kill $PF_PID 2>/dev/null || true
  exit 1
fi
echo ""

# Step 3: Display metric headers
echo -e "${BLUE}[3/3]${NC} Starting live metrics monitor (Ctrl+C to stop)"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "KEY METRICS TO WATCH:"
echo "  • ixp_atomix_operation_errors_total       → Error counter"
echo "  • ixp_atomix_operation_duration_*_bucket  → Latency buckets"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

ITERATION=0
LAST_ERROR_COUNT=0

while true; do
  ITERATION=$((ITERATION + 1))
  TIMESTAMP=$(date '+%H:%M:%S')
  
  # Fetch metrics from OTel Collector
  METRICS=$(curl -s --max-time 3 "http://localhost:$COLLECTOR_PORT/metrics" 2>/dev/null || echo "")
  
  if [ -z "$METRICS" ]; then
    echo -e "${YELLOW}[$TIMESTAMP] ⚠️  Failed to fetch metrics from collector${NC}"
    sleep 3
    continue
  fi
  
  # Extract error counter
  ERROR_METRICS=$(echo "$METRICS" | grep "ixp_atomix_operation_errors_total{" || true)
  ERROR_COUNT=$(echo "$METRICS" | grep "ixp_atomix_operation_errors_total{" | wc -l)
  
  # Extract histogram buckets
  HISTOGRAM_METRICS=$(echo "$METRICS" | grep "ixp_atomix_operation_duration.*_bucket" || true)
  HISTOGRAM_COUNT=$(echo "$METRICS" | grep "ixp_atomix_operation_duration.*_bucket" | wc -l)
  
  # Display result
  echo -e "${BLUE}━━━ Iteration $ITERATION [$TIMESTAMP] ━━━${NC}"
  
  if [ -z "$ERROR_METRICS" ]; then
    echo -e "  ${YELLOW}⏳ NO ERROR COUNTER METRICS FOUND YET${NC}"
  else
    echo -e "  ${GREEN}✅ ERROR COUNTER FOUND (${ERROR_COUNT} time series)${NC}"
    # Show first 3 error metric entries
    echo "$ERROR_METRICS" | head -3 | sed 's/^/     /'
    echo ""
  fi
  
  if [ -z "$HISTOGRAM_METRICS" ]; then
    echo -e "  ${YELLOW}⏳ NO HISTOGRAM METRICS FOUND YET${NC}"
  else
    echo -e "  ${GREEN}✅ HISTOGRAM METRICS FOUND (${HISTOGRAM_COUNT} buckets)${NC}"
    # Show sample histogram entries
    echo "$HISTOGRAM_METRICS" | head -2 | sed 's/^/     /'
  fi
  
  # Summary
  echo ""
  if [ "$ERROR_COUNT" -gt "$LAST_ERROR_COUNT" ]; then
    echo -e "  ${GREEN}📈 COUNTER INCREMENTED!${NC} ($LAST_ERROR_COUNT → $ERROR_COUNT)"
    LAST_ERROR_COUNT=$ERROR_COUNT
  fi
  
  echo ""
  sleep 3
done
