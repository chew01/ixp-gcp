#!/usr/bin/env bash
set -euo pipefail

namespace="${NAMESPACE:-default}"
atomix_statefulset="${ATOMIX_STATEFULSET:-consensus-store}"
telemetry_deployment="${TELEMETRY_DEPLOYMENT:-telemetry-service}"
kafka_statefulset="${KAFKA_STATEFULSET:-ixp-kafka-dual-role}"
api_deployment="${API_DEPLOYMENT:-api-gateway}"
atomix_replicas="${ATOMIX_REPLICAS:-3}"
telemetry_replicas="${TELEMETRY_REPLICAS:-1}"
kafka_replicas="${KAFKA_REPLICAS:-1}"
api_replicas="${API_REPLICAS:-1}"
node_to_uncordon="${NODE_TO_UNCORDON:-}"

echo "Restoring baseline replicas in namespace $namespace"
echo "- $atomix_statefulset: $atomix_replicas"
echo "- $telemetry_deployment: $telemetry_replicas"
echo "- $kafka_statefulset: $kafka_replicas"
echo "- $api_deployment: $api_replicas"

kubectl scale statefulset "$atomix_statefulset" -n "$namespace" --replicas="$atomix_replicas"
kubectl scale deployment "$telemetry_deployment" -n "$namespace" --replicas="$telemetry_replicas"
kubectl scale statefulset "$kafka_statefulset" -n "$namespace" --replicas="$kafka_replicas"
kubectl scale deployment "$api_deployment" -n "$namespace" --replicas="$api_replicas"

kubectl rollout status statefulset/"$atomix_statefulset" -n "$namespace" --timeout=120s || true
kubectl rollout status deployment/"$telemetry_deployment" -n "$namespace" --timeout=120s || true
kubectl rollout status statefulset/"$kafka_statefulset" -n "$namespace" --timeout=180s || true
kubectl rollout status deployment/"$api_deployment" -n "$namespace" --timeout=120s || true

if [[ -n "$node_to_uncordon" ]]; then
	echo "Uncordoning specified node: $node_to_uncordon"
	kubectl uncordon "$node_to_uncordon" >/dev/null 2>&1 || true
else
	cordoned_nodes="$(kubectl get nodes -o jsonpath='{range .items[?(@.spec.unschedulable==true)]}{.metadata.name}{"\n"}{end}')"
	if [[ -n "$cordoned_nodes" ]]; then
		echo "Uncordoning detected cordoned nodes"
		while IFS= read -r node; do
			[[ -z "$node" ]] && continue
			echo "- $node"
			kubectl uncordon "$node" >/dev/null 2>&1 || true
		done <<< "$cordoned_nodes"
	fi
fi

echo "Cluster restored"
