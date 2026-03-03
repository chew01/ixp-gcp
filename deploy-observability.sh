#!/bin/bash

# Deploy Observability Stack: Prometheus, Grafana, Jaeger, Loki, OTEL Collector
# This script sets up a complete observability stack for your SDN project

set -e

echo "Creating monitoring namespace..."
kubectl create namespace monitoring --dry-run=client -o yaml | kubectl apply -f -

# echo "Uninstalling current otel collector for quick restart"
# helm uninstall otel-collector -n monitoring

echo "Installing OpenTelemetry Collector..."
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm upgrade --install otel-collector open-telemetry/opentelemetry-collector \
  --namespace monitoring \
  -f helm/opentelemetry-collector/values.yaml \
  --wait

# echo "Installing Prometheus..."
# helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
# helm repo update
# helm upgrade --install prometheus prometheus-community/prometheus \
#   --namespace monitoring \
#   --set server.persistentVolume.enabled=false \
#   --set alertmanager.enabled=false \
#   --set pushgateway.enabled=false \
#   --set server.extraFlags="{web.enable-otlp-receiver}" \
#   --wait

echo "Installing Grafana with automated data sources..."
helm repo add grafana https://grafana.github.io/helm-charts || true
helm repo update
helm upgrade --install grafana grafana/grafana \
  --namespace monitoring \
  --set persistence.enabled=false \
  --set adminPassword='admin' \
  --set sidecar.datasources.enabled=true \
  --set sidecar.datasources.label=grafana_datasource \
  --set sidecar.datasources.searchNamespace=monitoring \
  --wait

# Apply the Data Source Configuration
kubectl apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: grafana-datasources
  namespace: monitoring
  labels:
    grafana_datasource: "1"
data:
  datasources.yaml: |
    apiVersion: 1
    datasources:
    - name: Prometheus
      type: prometheus
      url: http://prometheus-server.monitoring.svc.cluster.local
      access: proxy
      isDefault: true
    - name: Jaeger
      type: jaeger
      url: http://jaeger.monitoring.svc.cluster.local:16686
      access: proxy
    - name: Loki
      type: loki
      url: http://loki.monitoring.svc.cluster.local:3100
      access: proxy
EOF

echo "Installing Jaeger..."
helm repo add jaegertracing https://jaegertracing.github.io/helm-charts
helm upgrade --install jaeger jaegertracing/jaeger \
  --namespace monitoring \
  --set allInOne.enabled=true \
  --set storage.type=memory \
  --set agent.enabled=false \
  --set collector.enabled=true \
  --set query.enabled=true \
  --wait

echo "Installing Loki for log aggregation..."
helm upgrade --install loki grafana/loki \
  --namespace monitoring \
  -f helm/loki/values.yaml \
  --wait


echo "Setting up port forwarding..."
echo "Setup jaegar port forwarding"
kubectl port-forward -n monitoring svc/jaeger 16686:16686
echo "Grafana: kubectl port-forward -n monitoring svc/grafana 3000:80"
kubectl port-forward -n monitoring svc/grafana 3000:80
# echo "Prometheus: kubectl port-forward -n monitoring svc/prometheus-server 9090:80"
# kubectl port-forward -n monitoring svc/prometheus-server 9090:80
echo "Jaeger: kubectl port-forward -n monitoring svc/jaeger 16686:16686"
echo "Loki: kubectl port-forward -n monitoring svc/loki 3100:3100"

echo "Grafana admin credentials:"
echo "Username: admin"
echo "Password: admin"
echo "Access Grafana at: http://localhost:3000"

echo "To add data sources in Grafana:"
echo "1. Prometheus: http://prometheus-server.monitoring.svc.cluster.local"
echo "2. Jaeger: http://jaeger-query.monitoring.svc.cluster.local:16686"
echo "3. Loki: http://loki.monitoring.svc.cluster.local:3100"

echo "OTEL Collector OTLP endpoint: otel-collector.monitoring.svc.cluster.local:4317 (gRPC) or :4318 (HTTP)"

echo "Deployment complete! 🎉"