#!/usr/bin/env bash

# Shared helper for observability test scripts.
# Waits until a matching alert is firing and Alertmanager reports
# that a Telegram notification has been sent.

am_namespace="${ALERTMANAGER_NAMESPACE:-observability}"
am_service="${ALERTMANAGER_SERVICE:-}"
am_url="${ALERTMANAGER_URL:-}"
am_local_port="${ALERTMANAGER_LOCAL_PORT:-19093}"
am_poll_seconds="${ALERT_CHECK_INTERVAL_SECONDS:-5}"

_am_port_forward_pid=""
_am_port_forward_log=""

am_cleanup() {
  if [[ -n "${_am_port_forward_pid}" ]]; then
    kill "${_am_port_forward_pid}" >/dev/null 2>&1 || true
    wait "${_am_port_forward_pid}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${_am_port_forward_log}" && -f "${_am_port_forward_log}" ]]; then
    rm -f "${_am_port_forward_log}" >/dev/null 2>&1 || true
  fi
}

am_try_port_forward() {
  local local_port="$1"
  _am_port_forward_log="${TMPDIR:-/tmp}/am-port-forward-${local_port}-$$.log"

  kubectl -n "${am_namespace}" port-forward "svc/${am_service}" "${local_port}:9093" >"${_am_port_forward_log}" 2>&1 &
  _am_port_forward_pid=$!

  local attempts=0
  while (( attempts < 20 )); do
    if curl -fsS "http://127.0.0.1:${local_port}/-/ready" >/dev/null 2>&1; then
      am_local_port="${local_port}"
      am_url="http://127.0.0.1:${am_local_port}"
      return 0
    fi

    if ! kill -0 "${_am_port_forward_pid}" >/dev/null 2>&1; then
      break
    fi

    attempts=$((attempts + 1))
    sleep 1
  done

  if [[ -n "${_am_port_forward_pid}" ]]; then
    kill "${_am_port_forward_pid}" >/dev/null 2>&1 || true
    wait "${_am_port_forward_pid}" >/dev/null 2>&1 || true
  fi
  _am_port_forward_pid=""
  return 1
}

am_discover_service() {
  if [[ -n "${am_service}" ]]; then
    return 0
  fi

  local candidate
  for candidate in \
    "kube-prometheus-stack-alertmanager" \
    "observability-kube-prometh-alertmanager" \
    "observability-kube-prometheus-alertmanager" \
    "alertmanager-main"; do
    if kubectl -n "${am_namespace}" get svc "${candidate}" >/dev/null 2>&1; then
      am_service="${candidate}"
      return 0
    fi
  done

  am_service="$(kubectl -n "${am_namespace}" get svc -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' \
    | grep -E 'alertmanager|prometh.*alertmanager' \
    | grep -v '^alertmanager-operated$' \
    | head -n1 || true)"

  if [[ -z "${am_service}" ]] && kubectl -n "${am_namespace}" get svc alertmanager-operated >/dev/null 2>&1; then
    am_service="alertmanager-operated"
  fi

  if [[ -z "${am_service}" ]]; then
    echo "Could not auto-discover Alertmanager service in namespace ${am_namespace}." >&2
    echo "Set ALERTMANAGER_SERVICE or ALERTMANAGER_URL explicitly." >&2
    return 1
  fi

  return 0
}

am_prepare_endpoint() {
  if [[ -n "${am_url}" ]]; then
    return 0
  fi

  am_discover_service
  trap am_cleanup EXIT

  local candidate_ports="${am_local_port} 19094 19095 29093"
  local candidate
  for candidate in ${candidate_ports}; do
    if am_try_port_forward "${candidate}"; then
      return 0
    fi
  done

  echo "Failed to connect to Alertmanager at ${am_url}." >&2
  echo "Checked service ${am_service} in namespace ${am_namespace}." >&2
  echo "Set ALERTMANAGER_URL or ALERTMANAGER_SERVICE explicitly if needed." >&2
  if [[ -n "${_am_port_forward_log}" && -f "${_am_port_forward_log}" ]]; then
    echo "Last port-forward error output:" >&2
    cat "${_am_port_forward_log}" >&2 || true
  fi
  return 1
}

am_telegram_notifications_total() {
  curl -fsS "${am_url}/metrics" | awk '
    /^alertmanager_notifications_total\{/ && /telegram/ {
      sum += $NF
    }
    END {
      if (sum == "") {
        print 0
      } else {
        printf "%.0f\n", sum
      }
    }
  '
}

am_has_matching_firing_alert() {
  local alert_regex="$1"
  local alerts
  alerts="$(curl -fsS "${am_url}/api/v2/alerts?active=true&silenced=false&inhibited=false")"

  echo "${alerts}" | tr -d '\n' | grep -E "\"alertname\":\"(${alert_regex})\"" >/dev/null
}

wait_for_telegram_alert() {
  local alert_regex="$1"
  local timeout_seconds="$2"

  if [[ -z "${alert_regex}" ]]; then
    echo "wait_for_telegram_alert requires an alert regex" >&2
    return 1
  fi

  if [[ -z "${timeout_seconds}" ]]; then
    timeout_seconds=240
  fi

  am_prepare_endpoint

  local start_time
  local now
  local baseline
  local elapsed

  start_time="$(date +%s)"
  baseline="$(am_telegram_notifications_total)"

  echo "Waiting up to ${timeout_seconds}s for alert '${alert_regex}' and Telegram delivery..."

  while true; do
    now="$(date +%s)"
    elapsed=$((now - start_time))

    if (( elapsed > timeout_seconds )); then
      echo "Timed out after ${timeout_seconds}s while waiting for Telegram alert delivery." >&2
      return 1
    fi

    if am_has_matching_firing_alert "${alert_regex}"; then
      current="$(am_telegram_notifications_total)"
      if (( current > baseline )); then
        echo "Observed matching firing alert and Telegram notification count increase (${baseline} -> ${current})."
        return 0
      fi
    fi

    sleep "${am_poll_seconds}"
  done
}