# Deployment Guide

## Local Development with Minikube

### First-Time Setup

```bash
# 1. Install prerequisites (Go 1.25+, Docker, kubectl, Helm, Minikube)

# 2. Register Helm repos
make setup

# 3. Start Minikube
make deploy-minikube
# Starts Minikube with 4 CPUs and 8 GiB RAM; enables ingress addon.

# 4. Deploy the full control plane
make all
```

`make all` runs in this order:
1. `infra` → Atomix, Kafka (Strimzi), Prometheus/Grafana
2. `services` → builds Docker images, loads into Minikube, applies deployments

### Rebuilding After Code Changes

```bash
# Rebuild a specific service image and redeploy
docker build -t api-gateway:local ./api && minikube image load api-gateway:local
kubectl rollout restart deployment/api-gateway

# Or rebuild all
make services
```

### Accessing the API

```bash
kubectl port-forward svc/api-gateway 8080:8080 &
curl -H "X-Customer-ID: as12345" http://localhost:8080/credits
```

---

## External Kafka

### Plaintext

```bash
make infra KAFKA_EXTERNAL=true KAFKA_BOOTSTRAP=192.168.1.50:9092
make services KAFKA_BOOTSTRAP=192.168.1.50:9092
```

### mTLS (e.g. Aiven)

Create topics on Aiven: `switch-telemetry`, `auction-results`.

Place certificates in `certs/`:
```bash
kubectl create secret generic kafka-tls \
  --from-file=ca.pem=certs/ca.pem \
  --from-file=service.cert=certs/service.cert \
  --from-file=service.key=certs/service.key
```

Deploy:
```bash
make all \
  KAFKA_BOOTSTRAP=kafka-xxx.aivencloud.com:12345 \
  KAFKA_TLS_CA_FILE=/etc/kafka-tls/ca.pem \
  KAFKA_TLS_CERT_FILE=/etc/kafka-tls/service.cert \
  KAFKA_TLS_KEY_FILE=/etc/kafka-tls/service.key
```

Add the `kafka-tls` volume/volumeMount to each service's `deployment.yaml` before running.

---

## Atomix

### Inspecting State

```bash
# List all Atomix pods
kubectl get pods -l app=atomix-runtime

# Port-forward the Atomix REST API (if available)
kubectl port-forward svc/atomix-runtime 5678:5678
```

### Resetting State

Atomix state persists across pod restarts. To reset all maps (credits, utility, bids, auction history, telemetry):

```bash
# Scale down services that write to Atomix first
kubectl scale deployment/auction-runner --replicas=0
kubectl scale deployment/api-gateway --replicas=0
kubectl scale deployment/telemetry-service --replicas=0

# Delete the Atomix store (all data is lost)
kubectl delete -f atomix/store.yaml
kubectl apply -f atomix/store.yaml

# Scale services back up
kubectl scale deployment/auction-runner --replicas=1
kubectl scale deployment/api-gateway --replicas=1
kubectl scale deployment/telemetry-service --replicas=1
```

For development, a single-replica Atomix store (`atomix/store.yaml`) is sufficient. For production, configure a multi-replica store for HA.

---

## Scaling Considerations

The current implementation assumes a single switch (`scenario.Switches[0]`). To support multiple switches:

- The telemetry processor and auction runner would need to be sharded by switch or by egress port.
- The bid map key scheme (`bids-<egressPort>`) would need to include a switch ID.
- The API `/flows` endpoint already accepts `switch_id` as a query parameter.

---

## Troubleshooting

### Atomix Not Ready

```
Error: failed to init throughput map: ...
```

Wait for the Atomix runtime pod to be `Running`:
```bash
kubectl get pods | grep atomix
kubectl rollout status deployment/atomix-runtime
```

If it stays `Pending`, check node resources: Atomix needs at least 512 MiB RAM.

### Kafka Topic Missing

```
Error: unknown topic or partition
```

For Strimzi, wait for the Kafka cluster to be `Ready`:
```bash
kubectl wait kafka/ixp-kafka --for=condition=Ready --timeout=300s
```

Topics are auto-created by the producers. If using an external broker, create topics manually.

### Agent Crash Loop

```bash
kubectl logs deployment/customer-agent-as12345
```

Common causes:
- `CUSTOMER_ID not found in scenario`: customer ID in the deployment doesn't match the scenario YAML.
- `unknown strategy "backoff"`: `backoff` was removed; update the scenario to use a supported strategy.
- `API_BASE_URL unreachable`: API gateway not yet running or wrong service name.

### Build Fails After `shared/` Change

Each service has its own `vendor/` directory. After modifying `shared/scenario/types.go` or `shared/scenario/load.go`, sync to all vendor directories:

```bash
for dir in api auction dummy telemetry agent; do
  cp shared/scenario/types.go $dir/vendor/github.com/chew01/ixp-gcp/shared/scenario/types.go
  cp shared/scenario/load.go $dir/vendor/github.com/chew01/ixp-gcp/shared/scenario/load.go
done
```

Or run `make vendor` to re-run `go mod vendor` for all modules (requires network access to the Go module proxy).

### Grafana Shows No Data

1. Confirm Prometheus is scraping: http://localhost:9090/targets (all targets `UP`).
2. Confirm the dashboard uses the correct datasource UID (`prometheus`). Check `observability/ixp-flows.json`.
3. Confirm the `ixp-flows-dashboard` ConfigMap is labelled: `kubectl get configmap ixp-flows-dashboard -n observability --show-labels`.
