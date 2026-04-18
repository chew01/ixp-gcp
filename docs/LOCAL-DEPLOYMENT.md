# Local Deployment Guide (Minikube)

This guide covers running the full IXP control plane on your local machine using Minikube.

---

## Prerequisites

- Go 1.22+, Docker, kubectl, Helm, Minikube

---

## First-Time Setup

```bash
# 1. Register Helm repos
make setup

# 2. Start Minikube (4 CPUs, 8 GiB RAM, ingress addon enabled)
make deploy-minikube

# 3. Deploy infra + core services
make all
```

`make all` runs in order:
1. **infra** — Atomix, Strimzi Kafka, Prometheus/Grafana, default scenario ConfigMap
2. **services** — vendors Go modules, builds Docker images into Minikube's daemon, applies deployments

Verify everything is up:

```bash
kubectl get pods
kubectl wait kafka/ixp-kafka --for=condition=Ready --timeout=300s
```

---

## Running an Experiment

```bash
make all experiment=7
```

Loads the experiment scenario, deploys the dummy traffic producer, restarts core services, and creates one agent pod per customer. Switch experiments by re-running with a different ID.

See `docs/EXPERIMENTS.md` for the full reference table.

---

## Deploying the Dashboard

```bash
make deploy-dashboard
```

Builds the dashboard image (including the React frontend), applies RBAC, Deployment, and Ingress, and sets the Kafka env var.

Access it via Minikube's ingress:

```bash
open "http://$(minikube ip)/dashboard"
```

Or port-forward directly:

```bash
make dashboard-ui   # → http://localhost:8082
```

---

## Accessing Services

| Service     | Command                                           | URL                               |
|-------------|---------------------------------------------------|-----------------------------------|
| API gateway | `kubectl port-forward svc/api-gateway 8080:8080`  | http://localhost:8080             |
| Grafana     | `make grafana-ui`                                 | http://localhost:3000             |
| Prometheus  | `make prometheus-ui`                              | http://localhost:9090             |
| Dashboard   | `make dashboard-ui`                               | http://localhost:8082             |

---

## Rebuilding a Single Service

After a code change, rebuild and redeploy without re-running `make all`:

```bash
make deploy-api
make deploy-auction
make deploy-telemetry
make deploy-dashboard
```

Each target builds the image into Minikube's daemon, applies manifests, and rolls out the new image.

---

## Resetting Atomix State

Atomix state (credits, utility, bids, auction history) persists across pod restarts. Reset before experiments that need a clean baseline (4a, 4b):

```bash
kubectl scale deployment/auction-runner deployment/api-gateway deployment/telemetry-service --replicas=0
kubectl delete -f atomix/store.yaml && kubectl apply -f atomix/store.yaml
kubectl scale deployment/auction-runner deployment/api-gateway deployment/telemetry-service --replicas=1
```

---

## External Kafka

**Plaintext:**

```bash
make infra KAFKA_EXTERNAL=true KAFKA_BOOTSTRAP=192.168.1.50:9092
make services KAFKA_BOOTSTRAP=192.168.1.50:9092
```

**mTLS (e.g. Aiven):** create topics `switch-telemetry` and `auction-results`, then:

```bash
kubectl create secret generic kafka-tls \
  --from-file=ca.pem=certs/ca.pem \
  --from-file=service.cert=certs/service.cert \
  --from-file=service.key=certs/service.key

make all \
  KAFKA_BOOTSTRAP=kafka-xxx.aivencloud.com:12345 \
  KAFKA_TLS_CA_FILE=/etc/kafka-tls/ca.pem \
  KAFKA_TLS_CERT_FILE=/etc/kafka-tls/service.cert \
  KAFKA_TLS_KEY_FILE=/etc/kafka-tls/service.key
```

---

## Troubleshooting

**Atomix not ready** (`Error: failed to init throughput map`):

```bash
kubectl rollout status deployment/atomix-runtime
# If Pending: check node resources — Atomix needs ≥512 MiB RAM
```

**Kafka topic missing** (`Error: unknown topic or partition`):

```bash
kubectl wait kafka/ixp-kafka --for=condition=Ready --timeout=300s
# Topics are auto-created by producers once Kafka is Ready
```

**Agent crash loop:**

```bash
kubectl logs deployment/customer-agent-as12345
# Common: CUSTOMER_ID not in scenario, or API_BASE_URL unreachable
```

**Build fails after `shared/` change** — each service has its own `vendor/` directory; sync after editing `shared/scenario/*.go`:

```bash
for dir in api auction dummy telemetry agent; do
  cp shared/scenario/types.go $dir/vendor/github.com/chew01/ixp-gcp/shared/scenario/types.go
  cp shared/scenario/load.go  $dir/vendor/github.com/chew01/ixp-gcp/shared/scenario/load.go
done
# Or: make vendor  (requires network)
```

**Grafana shows no data:**

1. Prometheus targets all `UP`: http://localhost:9090/targets
2. Dashboard datasource UID is `prometheus` (check `monitoring/ixp-flows.json`)
3. ConfigMap is labelled: `kubectl get configmap ixp-flows-dashboard -n monitoring --show-labels`

---

## Teardown

```bash
make stop   # minikube delete
```
