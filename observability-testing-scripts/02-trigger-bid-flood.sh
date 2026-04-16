#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

api_base_url="${API_BASE_URL:-http://api-gateway.default.svc.cluster.local:80}"
api_namespace="${API_NAMESPACE:-default}"
api_service="${API_SERVICE:-api-gateway}"
api_local_port="${API_LOCAL_PORT:-18080}"
api_service_port="${API_SERVICE_PORT:-80}"
customer_id="${CUSTOMER_ID:-flood-test}"
ingress_ports="${INGRESS_PORTS:-100}"
egress_port="${EGRESS_PORT:-132}"
requests="${REQUESTS:-12000}"
parallelism="${PARALLELISM:-20}"
rate_limit_delay="${RATE_LIMIT_DELAY:-0.05}"  # seconds between batches (20 batches/sec = 400 req/sec with parallelism 20)
unit_price="${UNIT_PRICE:-100}"
units="${UNITS:-10}"

port_list=($ingress_ports)

_api_port_forward_pid=""
_api_port_forward_log=""

api_cleanup() {
  if [[ -n "${_api_port_forward_pid}" ]]; then
    kill "${_api_port_forward_pid}" >/dev/null 2>&1 || true
    wait "${_api_port_forward_pid}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${_api_port_forward_log}" && -f "${_api_port_forward_log}" ]]; then
    rm -f "${_api_port_forward_log}" >/dev/null 2>&1 || true
  fi
}

api_reachable() {
  local url="$1"
  curl -sS --max-time 2 -o /dev/null "$url"
}

api_prepare_endpoint() {
  local svc_port
  svc_port="$(kubectl -n "${api_namespace}" get svc "${api_service}" -o jsonpath='{.spec.ports[?(@.name=="http")].port}' 2>/dev/null || true)"
  if [[ -z "${svc_port}" ]]; then
    svc_port="$(kubectl -n "${api_namespace}" get svc "${api_service}" -o jsonpath='{.spec.ports[0].port}' 2>/dev/null || true)"
  fi
  if [[ -n "${svc_port}" ]]; then
    api_service_port="${svc_port}"
  fi

  echo "api-gateway service port: ${api_service_port}"

  # Skip DNS check - go directly to port-forward if needed
  echo "Setting up API access via port-forward..."

  _api_port_forward_log="${TMPDIR:-/tmp}/api-port-forward-${api_local_port}-$$.log"
  kubectl -n "${api_namespace}" port-forward "svc/${api_service}" "${api_local_port}:${api_service_port}" >"${_api_port_forward_log}" 2>&1 &
  _api_port_forward_pid=$!
  disown $_api_port_forward_pid  # Remove from job control so wait doesn't block on it

  local attempts=0
  local local_url="http://127.0.0.1:${api_local_port}"
  
  # Wait for port-forward to establish
  sleep 2
  
  while (( attempts < 30 )); do
    if api_reachable "$local_url"; then
      api_base_url="$local_url"
      echo "Using API endpoint via port-forward: $api_base_url"
      return 0
    fi

    if ! kill -0 "${_api_port_forward_pid}" >/dev/null 2>&1; then
      echo "Port-forward process died, retrying..." >&2
      kubectl -n "${api_namespace}" port-forward "svc/${api_service}" "${api_local_port}:${api_service_port}" >"${_api_port_forward_log}" 2>&1 &
      _api_port_forward_pid=$!
    fi

    attempts=$((attempts + 1))
    sleep 2
  done

  echo "Failed to reach API service via port-forward." >&2
  if [[ -n "${_api_port_forward_log}" && -f "${_api_port_forward_log}" ]]; then
    echo "Port-forward output:" >&2
    cat "${_api_port_forward_log}" >&2 || true
  fi
  return 1
}

trap api_cleanup EXIT

submit_bid() {
  local ingress_port="$1"
  curl -sS --max-time 5 --connect-timeout 2 -w "%{http_code}" -o /dev/null \
    -X POST "$api_base_url/bids" \
    -H "Content-Type: application/json" \
    -H "X-Customer-ID: $customer_id" \
    -d "{\"ingress_port\":$ingress_port,\"egress_port\":$egress_port,\"units\":$units,\"unit_price\":$unit_price}" 2>/dev/null || echo "000"
}

api_prepare_endpoint

sleep 2
echo "Starting bid flood to $api_base_url/bids with ${requests} requests (parallelism ${parallelism}, rate ${rate_limit_delay}s delay)..."
echo "Estimated duration: $(awk "BEGIN {print int(${requests}/${parallelism}*${rate_limit_delay})}") seconds"

request_count=0
batch_count=0
for i in $(seq 1 "$requests"); do
  ingress_port="${port_list[$(( (i - 1) % ${#port_list[@]} ))]}"
  submit_bid "$ingress_port" >/dev/null 2>&1 &
  request_count=$((request_count + 1))
  if (( request_count % 100 == 0 )); then
    echo "[Progress] Sent $request_count/$requests requests..." >&2
  fi
  if (( i % parallelism == 0 )); then
    batch_count=$((batch_count + 1))
    echo "[Batch $batch_count] Waiting for $parallelism requests to complete..." >&2
    wait
    echo "[Batch $batch_count] Complete. Sleeping ${rate_limit_delay}s..." >&2
    sleep "$rate_limit_delay"
  fi
done

wait

echo "Bid flood completed: sent ${request_count} requests with parallelism ${parallelism} to ${api_base_url}/bids"
