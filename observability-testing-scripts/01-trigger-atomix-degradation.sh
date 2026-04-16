#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${script_dir}/lib-alert-wait.sh"

namespace="${NAMESPACE:-default}"
statefulset="${ATOMIX_STATEFULSET:-consensus-store}"
replicas="${ATOMIX_REPLICAS:-0}"
expected_alert_regex="${EXPECTED_ALERT_REGEX:-IXPAtomix(OperationFailuresCritical|NearTotalFailure|ConnectionErrors|VeryHighLatencyCritical|CriticalFailureRate|TotalFailureImminent|P99LatencyCritical)|IXPAuctionNoBidsAcrossIntervals}"
alert_timeout_seconds="${ALERT_TIMEOUT_SECONDS:-240}"

current_replicas="$(kubectl get statefulset "$statefulset" -n "$namespace" -o jsonpath='{.spec.replicas}')"

echo "Scaling $statefulset in namespace $namespace to $replicas replicas"
kubectl scale statefulset "$statefulset" -n "$namespace" --replicas="$replicas"
kubectl rollout status statefulset/"$statefulset" -n "$namespace" --timeout=120s || true

wait_for_telegram_alert "${expected_alert_regex}" "${alert_timeout_seconds}"

echo "Current replicas were $current_replicas. Restore with: NAMESPACE=$namespace ATOMIX_STATEFULSET=$statefulset ATOMIX_REPLICAS=$current_replicas ./03-restore-cluster.sh"
