#!/bin/bash

# =============================================================================
# DEPRECATED: This script has been replaced by the Makefile target
# =============================================================================
# 
# The observability stack has been unified and integrated into the Makefile.
# Please use: make deploy-observability
#
# This provides a complete observability stack with:
# - Prometheus (metrics)
# - Grafana (visualization)
# - OpenTelemetry Collector (telemetry ingestion)
# - Jaeger (distributed tracing)
# - Loki (log aggregation)
#
# See docs/observability-architecture.md for details.
# =============================================================================

echo "⚠️  DEPRECATION NOTICE"
echo ""
echo "This script is deprecated. Please use the Makefile instead:"
echo ""
echo "  make setup            # One-time setup of Helm repos"
echo "  make deploy-observability # Deploy unified observability stack"
echo "  make grafana-ui       # Access Grafana UI"
echo ""
echo "Or deploy everything at once:"
echo ""
echo "  make all              # Deploy infrastructure + services + observability"
echo ""
echo "See: docs/observability-architecture.md for more information."
echo ""

exit 1