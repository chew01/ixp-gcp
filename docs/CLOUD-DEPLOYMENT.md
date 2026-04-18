# Cloud Deployment Guide — Digital Ocean Kubernetes (DOKS)

This guide covers migrating the IXP control plane from a local Minikube cluster to a managed Kubernetes cluster on Digital Ocean (DOKS). The existing Makefile, scenario YAMLs, and experiment workflow are unchanged; only the image build/push path and a small number of one-time cluster setup steps differ.

---

## Overview of Differences from Minikube


| Concern             | Minikube                                                                     | DOKS                                                                                    |
| ------------------- | ---------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| Image delivery      | `eval $(minikube docker-env)` — images loaded directly into the local daemon | Build locally, push to DOCR (Digital Ocean Container Registry), pull in cluster         |
| Ingress             | Minikube ingress addon                                                       | nginx-ingress-controller deployed via Helm                                              |
| Load balancer       | `kubectl port-forward` only                                                  | DO managed load balancer provisioned automatically for `type: LoadBalancer` services    |
| Storage (Atomix PV) | Minikube hostPath                                                            | DOBS (Digital Ocean Block Storage) — provisioned automatically via default StorageClass |
| Kafka               | In-cluster Strimzi (same)                                                    | In-cluster Strimzi (same) — no VPN required for evaluation experiments                  |
| Dashboard           | `make dashboard-ui` → `kubectl port-forward svc/dashboard 8082:8082`         | Routed through nginx ingress at `http://$LB_IP/dashboard` — no port-forward needed      |


---

## Prerequisites

1. **doctl** installed: [https://docs.digitalocean.com/reference/doctl/how-to/install/](https://docs.digitalocean.com/reference/doctl/how-to/install/)
  ```bash
   brew install doctl   # macOS
   doctl auth init      # paste your DO API token when prompted
  ```
2. **DOKS cluster** (3 nodes, minimum `s-4vcpu-8gb` each):
  ```bash
   doctl kubernetes cluster create ixp-cluster \
     --region sgp1 \
     --node-pool "name=default;size=s-4vcpu-8gb;count=3" \
     --wait
  ```
3. **Digital Ocean Container Registry (DOCR)** created and integrated with the cluster:
  ```bash
   doctl registry create ixp-registry --region sgp1
   # Link the registry to the cluster so nodes can pull images without extra secrets
   doctl registry kubernetes-manifest | kubectl apply -f -
  ```
4. **kubectl context** switched to the new cluster:
  ```bash
   doctl kubernetes cluster kubeconfig save ixp-cluster
   kubectl config use-context do-sgp1-ixp-cluster
   kubectl get nodes   # verify 3 nodes Ready
  ```
5. **Docker** authenticated to DOCR:
  ```bash
   doctl registry login
  ```
6. **Helm repos** registered (same as local):
  ```bash
   make setup
  ```

---

## One-Time Cluster Setup

### Install nginx-ingress-controller

The DOKS cluster does not include Minikube's built-in ingress addon. Install the community nginx-ingress-controller, which will provision a DO load balancer automatically:

```bash
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm repo update
helm install ingress-nginx ingress-nginx/ingress-nginx \
  --namespace ingress-nginx --create-namespace \
  --set controller.service.type=LoadBalancer
```

Wait for the load balancer IP to be assigned:

```bash
kubectl get svc -n ingress-nginx ingress-nginx-controller --watch
# → EXTERNAL-IP changes from <pending> to a public IP within ~60s
export LB_IP=$(kubectl get svc -n ingress-nginx ingress-nginx-controller \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
echo "Load balancer IP: $LB_IP"
```

This IP is used as `API_URL` for load tests and as the `hey` target.

---

## Deploying the Full Control Plane

Set the registry and tag once:

```bash
export DOCKER_REGISTRY=registry.digitalocean.com/ixp-registry
export IMAGE_TAG=v1.0.0
```

Then follow this two-step sequence — **order matters**:

```bash
# 1. Deploy infrastructure (Atomix, in-cluster Strimzi Kafka, Prometheus/Grafana)
#    This installs the Prometheus Operator CRDs (ServiceMonitor, PodMonitor, etc.)
#    that the service manifests depend on.
make infra

# 2. Build, push, and deploy all core services
make services DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG
```

`make infra` is identical to the local workflow. Atomix will use the DOBS StorageClass for its PersistentVolumeClaims automatically. Strimzi Kafka is deployed in-cluster — no VPN required.

> **Do not run `make deploy-api`, `make deploy-auction`, or `make deploy-telemetry` before `make infra` completes.** Each `deploy-`* target applies Kubernetes manifests including a `ServiceMonitor` resource (`monitoring.coreos.com/v1`). If the Prometheus Operator CRDs are not yet installed, `kubectl apply` will fail with `no matches for kind "ServiceMonitor"`.

Verify all pods are running:

```bash
kubectl get pods
# Expected: api-gateway, auction-runner, telemetry-service all Running
kubectl wait kafka/ixp-kafka --for=condition=Ready --timeout=300s
```

---

## Rebuilding Individual Services

Use these commands to rebuild and redeploy a single service after a code change. `make infra` must already have been run at least once before using them.

Each `deploy-*` target runs `docker build`, `docker push`, applies the service's Kubernetes manifests (ingress, deployment, service, ServiceMonitor), and calls `kubectl set image` to roll out the new image. The `deploy-*.yaml` files retain `image: <service>:local` as a static default; the `kubectl set image` call overrides this at deploy time.

```bash
make deploy-api DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG
make deploy-auction DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG
make deploy-telemetry DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG
```

The dashboard is rebuilt the same way:

```bash
make deploy-dashboard DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG
```

---

## Deploying the Dashboard

The dashboard is an optional live-view service that visualises the control plane topology, telemetry, auction events, and credit balances in real time. It uses Kafka, Atomix, and the Kubernetes API (via RBAC) and is served at `/dashboard` through the nginx ingress.

Deploy it after the core control plane is running:

```bash
# 1. Apply RBAC (ServiceAccount + Role + RoleBinding) — required for K8s pod/deployment listing
kubectl apply -f dashboard/rbac.yaml

# 2. Build, push, and deploy the dashboard image
make deploy-dashboard \
  DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG \
  KAFKA_BOOTSTRAP=$KAFKA_BOOTSTRAP

# 3. Apply the dashboard ingress rule (routes /dashboard/* to svc/dashboard:8082)
kubectl apply -f dashboard/ingress.yaml
```

> `make deploy-dashboard` applies `dashboard/rbac.yaml` and `dashboard/deployment.yaml` automatically but does **not** apply `dashboard/ingress.yaml` — apply it once manually as shown above.

Verify the pod is running:

```bash
kubectl get pod -l app=dashboard
kubectl logs deployment/dashboard --tail=20
```

### Accessing the Dashboard

On DOKS the dashboard is reachable through the nginx ingress load balancer — no port-forwarding required:

```bash
echo "Dashboard: http://$LB_IP/dashboard"
open "http://$LB_IP/dashboard"
```

On Minikube (local workflow), use:

```bash
make dashboard-ui   # port-forwards svc/dashboard → http://localhost:8082
```

---

## Running Experiments

Load and start an experiment exactly as in the local workflow, passing the registry variables:

```bash
make all experiment=7 DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG
```

This builds and pushes the `customer-agent` image, generates per-customer Deployment manifests with the correct image reference, and applies them.

### Accessing the API and Dashboard

```bash
# API gateway and dashboard both route through the nginx ingress load balancer
export API_URL=http://$LB_IP
curl -H "X-Customer-ID: as12345" $API_URL/credits

# Dashboard live view (no port-forward needed on DOKS)
echo "Dashboard: http://$LB_IP/dashboard"
```

### Accessing Grafana and Prometheus

Port-forward from the cloud cluster to your local machine (same as Minikube):

```bash
make grafana-ui      # Grafana at http://localhost:3000
make prometheus-ui   # Prometheus at http://localhost:9090
```

### Running API Load Tests (Section 5.2.1)

```bash
# Install hey once
go install github.com/rakyll/hey@latest

# 2 concurrent clients (baseline)
make load-test-api API_URL=$API_URL CONCURRENCY=2

# Repeat for each concurrency level in the sweep
make load-test-api API_URL=$API_URL CONCURRENCY=5
make load-test-api API_URL=$API_URL CONCURRENCY=10
make load-test-api API_URL=$API_URL CONCURRENCY=20
```

### Measuring Auction Pipeline Latency (Section 5.2.2)

```bash
# Load the 5-bidder scenario
make all experiment=perf-5bidders DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG

# Wait ≥30 intervals (15 minutes at 30s/interval), then collect timing logs
make measure-pipeline-latency

# Repeat for 10 bidders
make all experiment=perf-10bidders DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG
make measure-pipeline-latency

# The 2-bidder baseline uses experiment-1
make all experiment=1 DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG
make measure-pipeline-latency
```

The output contains `[bids-collected]`, `[cleared]`, and `[published-to-kafka]` lines with `elapsed_ms` fields. Collect 30 rounds per bidder count and report mean and variance.

### Measuring Kafka Consumer Lag (Section 5.2.3)

While an experiment is running:

```bash
make measure-kafka-lag
```

This runs `kafka-consumer-groups.sh --describe --all-groups` inside the Kafka pod. Record the `LAG` column for the `switch-telemetry` and `auction-results` consumer groups. Run during a spike experiment to observe burst behaviour.

### Measuring Control Loop E2E Latency (Section 5.2.4)

```bash
# Start a spike experiment (e.g. experiment 7)
make all experiment=7 DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG

# With prometheus-ui running, collect the spike time and allocation series
make measure-e2e-latency SINCE=30m
```

The target prints the dummy-producer's spike log line (with its timestamp) and the Prometheus `ixp_customer_allocation_kbps` time series. The E2E latency is the gap between the spike log timestamp and the first sample showing a higher allocation value.

---

## Atomix State Reset Between Experiments

```bash
kubectl scale deployment/auction-runner deployment/api-gateway \
  deployment/telemetry-service --replicas=0
kubectl delete -f atomix/store.yaml
kubectl apply -f atomix/store.yaml
kubectl scale deployment/auction-runner deployment/api-gateway \
  deployment/telemetry-service --replicas=1
```

Always reset before experiments that depend on a clean credit baseline (4a, 4b) or a fresh utility/history baseline.

---

## Exporting Metrics

```bash
# Port-forward Prometheus first
make prometheus-ui

# Export the last hour of experiment data
make export-metrics

# Wider window (e.g. for a 50-interval Q-learning run)
make export-metrics SINCE=2h
```

Output is written to `data/experiment-<timestamp>.json`.

---

## Kafka and VPN

All Section 5 evaluation experiments use the **in-cluster Strimzi Kafka** broker (`ixp-kafka-kafka-bootstrap:9092`). The local switch controller, telemetry processor, and auction runner all run inside the DOKS cluster and communicate with Kafka over the cluster-internal network. **No VPN is required for any evaluation experiment.**

VPN-accessible Kafka is only relevant for the real-switch (`deploy-real`) scenario, where the physical switch's telemetry is produced by the local switch controller running on a separate network behind a VPN. To connect a DOKS cluster to VPN-accessible Kafka:

1. Deploy a WireGuard gateway pod (e.g. using the `linuxserver/wireguard` image) as a DaemonSet or single Deployment, configured with your VPN credentials.
2. Add a routing rule in the gateway pod so that traffic to the Kafka broker CIDR routes through the WireGuard interface.
3. Set `KAFKA_BOOTSTRAP` to the VPN-reachable broker address and optionally provide `KAFKA_TLS_`* for mTLS:

```bash
make deploy-real \
  KAFKA_BOOTSTRAP=10.x.x.x:9092 \
  KAFKA_TLS_CA_FILE=/etc/kafka-tls/ca.pem \
  KAFKA_TLS_CERT_FILE=/etc/kafka-tls/service.cert \
  KAFKA_TLS_KEY_FILE=/etc/kafka-tls/service.key \
  DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG
```

The existing `kafka-tls` Secret and volume mount in each `deployment.yaml` already handle the TLS credential path — no source changes are needed.

---

## Tearing Down

```bash
# Delete experiment agents
make delete-agents

# Delete the dashboard (if deployed)
kubectl delete -f dashboard/ingress.yaml --ignore-not-found
kubectl delete -f dashboard/deployment.yaml --ignore-not-found
kubectl delete -f dashboard/rbac.yaml --ignore-not-found

# Delete core services and infra
kubectl delete -f atomix/store.yaml
kubectl delete -f atomix/storage-profile.yaml
helm uninstall atomix-runtime -n kube-system
helm uninstall strimzi-cluster-operator
helm uninstall monitoring -n monitoring
helm uninstall ingress-nginx -n ingress-nginx

# Delete the DOKS cluster (destroys all nodes and load balancers)
doctl kubernetes cluster delete ixp-cluster
```

