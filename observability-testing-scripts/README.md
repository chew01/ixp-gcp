# Observability Testing Scripts

This folder contains the small scripts used to reproduce the evaluation scenarios in Chapter 6.

## What the scripts do

- `02-trigger-atomix-degradation.sh`: scales down the Atomix consensus store to trigger dependency alerts.
- `03-trigger-telemetry-stop.sh`: stops the telemetry service to trigger no-flow and low-consumption alerts.
- `04-trigger-bid-flood.sh`: sends repeated bid requests to the API to create load and surface congestion or error alerts.
- `06-trigger-kafka-outage.sh`: scales Kafka down to trigger pipeline and service-degradation alerts.
- `07-trigger-api-replica-mismatch.sh`: creates `available < desired` for API Gateway to trigger replica-availability alert.
- `03-restore-cluster.sh`: restores Atomix and telemetry replicas after a fault test.

## Typical flow

1. Ensure your target scenario/environment is already running.
2. Open Grafana, Prometheus, and Alertmanager.
3. Run one trigger script.
4. Wait for the matching alert.
5. Capture the screenshot or Telegram notification.
6. Restore the cluster before the next test.

## Alert-gated execution (new)

The trigger scripts now block until they observe both:

- a matching firing alert in Alertmanager, and
- a Telegram notification send event from Alertmanager metrics.

This prevents moving to the next experiment step before alert delivery is confirmed.

Scripts with gating:

- `02-trigger-atomix-degradation.sh`
- `03-trigger-telemetry-stop.sh`
- `04-trigger-bid-flood.sh`
- `06-trigger-kafka-outage.sh`
- `07-trigger-api-replica-mismatch.sh`

Recommended Chapter 6 scenario taxonomy:

- Dependency outage: `02-trigger-atomix-degradation.sh`
- Pipeline outage: `06-trigger-kafka-outage.sh`
- Congestion: `04-trigger-bid-flood.sh`
- Replica availability mismatch: `07-trigger-api-replica-mismatch.sh`

Default Alertmanager connection behavior:

- Use `ALERTMANAGER_URL` if provided.
- Otherwise, auto port-forward `svc/kube-prometheus-stack-alertmanager` in namespace `observability` on local port `19093`.

Useful overrides:

- `EXPECTED_ALERT_REGEX`: override expected alert name pattern.
- `ALERT_TIMEOUT_SECONDS`: max wait time before failing.
- `ALERT_CHECK_INTERVAL_SECONDS`: polling interval (default `5`).
- `ALERTMANAGER_NAMESPACE`, `ALERTMANAGER_SERVICE`, `ALERTMANAGER_LOCAL_PORT`: Alertmanager discovery tuning.

## Notes

- The scripts assume the services run in the `default` namespace unless overridden.
- Adjust `API_BASE_URL`, `PROMETHEUS_URL`, and `NAMESPACE` if your cluster differs.
- `07-trigger-api-replica-mismatch.sh` uses node cordon by default (`CORDON_NODE=true`) to keep one replica pending long enough for the alert to fire.
- `03-restore-cluster.sh` restores baseline replicas by default (Atomix=3, Telemetry=1, Kafka=1, API=1) and also accepts `KAFKA_REPLICAS`, `API_REPLICAS`, and `NODE_TO_UNCORDON` overrides for custom cleanup.
- The goal is to keep the evaluation reproducible without adding new application code.
