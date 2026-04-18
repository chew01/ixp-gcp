# Cloud Deployment Guide (DigitalOcean Kubernetes)

This guide covers deploying the IXP control plane on a managed DOKS cluster. The Makefile workflow is identical to local — only the image build/push path and a one-time cluster setup differ.

---

## How DOKS Differs from Minikube

| Concern        | Minikube                                 | DOKS                                                      |
|----------------|------------------------------------------|-----------------------------------------------------------|
| Image delivery | Built directly into Minikube daemon      | Built locally, pushed to DOCR, pulled by cluster          |
| Ingress        | Minikube ingress addon                   | nginx-ingress-controller (installed via Helm, one-time)   |
| Load balancer  | `kubectl port-forward` only              | DO managed load balancer auto-provisioned                 |
| Storage        | Minikube hostPath                        | DO Block Storage via default StorageClass                 |
| Dashboard      | `make dashboard-ui` → localhost:8082     | `http://<LB-IP>/dashboard` — no port-forward needed       |

---

## Prerequisites

1. **doctl** installed and authenticated:

   ```bash
   brew install doctl        # macOS
   doctl auth init           # paste your DO API token
   ```

2. **DOKS cluster** — create one from the [DigitalOcean dashboard](https://cloud.digitalocean.com/kubernetes/clusters) if you haven't already (recommended: 3 × `s-4vcpu-8gb` nodes, `sgp1` region).

3. **kubeconfig** switched to the new cluster:

   ```bash
   doctl kubernetes cluster kubeconfig save 9b3f3230-9626-467e-a9da-b1eb8b20a2c5
   kubectl get nodes   # verify nodes are Ready
   ```

4. **DOCR** created, integrated with the cluster, and Docker authenticated:

   ```bash
   doctl registry create ixp-registry --region sgp1
   doctl registry kubernetes-manifest | kubectl apply -f -   # lets nodes pull without extra secrets
   doctl registry login
   ```

5. **Helm repos** registered:

   ```bash
   make setup
   ```

---

## One-Time Cluster Setup: nginx Ingress

DOKS does not include Minikube's ingress addon. Install the nginx-ingress-controller — it automatically provisions a DO load balancer:

```bash
helm install ingress-nginx ingress-nginx/ingress-nginx \
  --namespace ingress-nginx --create-namespace \
  --set controller.service.type=LoadBalancer
```

Wait for the load balancer IP (takes ~60 s):

```bash
kubectl get svc -n ingress-nginx ingress-nginx-controller --watch
# EXTERNAL-IP changes from <pending> to a public IP

export LB_IP=$(kubectl get svc -n ingress-nginx ingress-nginx-controller \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
echo "Load balancer: $LB_IP"
```

All services (API, dashboard) route through this single IP.

---

## Deploying the Full Stack

Set registry variables once:

```bash
export DOCKER_REGISTRY=registry.digitalocean.com/ixp-registry
export IMAGE_TAG=v1.0.0
```

Then deploy in order — **infra must finish before services**:

```bash
# 1. Infrastructure (Atomix, Kafka, Prometheus/Grafana, default scenario ConfigMap)
make infra

# 2. Core services (build, push, deploy api-gateway, auction-runner, telemetry-service)
make services DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG

# 3. Dashboard (build, push, deploy with RBAC and Ingress)
make deploy-dashboard DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG
```

`make infra` must complete first because it installs the Prometheus Operator CRDs (`ServiceMonitor`) that the API gateway manifest depends on.

Verify all pods are running:

```bash
kubectl get pods
kubectl wait kafka/ixp-kafka --for=condition=Ready --timeout=300s
```

---

## Accessing Services

```bash
# API gateway
curl -H "X-Customer-ID: as12345" http://$LB_IP/credits

# Dashboard (live view — no port-forward needed)
open "http://$LB_IP/dashboard"

# Grafana and Prometheus (port-forward to local machine, same as Minikube)
make grafana-ui      # http://localhost:3000
make prometheus-ui   # http://localhost:9090
```

---

## Running Experiments

```bash
make all experiment=7 DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG
```

Builds and pushes the `customer-agent` image, loads the experiment scenario, and deploys agent pods.

---

## Rebuilding Individual Services

After a code change, rebuild and redeploy one service without re-running `make all`:

```bash
make deploy-api       DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG
make deploy-auction   DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG
make deploy-telemetry DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG
make deploy-dashboard DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG
```

---

## Resetting Atomix State

Reset before experiments that need a clean credit baseline (4a, 4b):

```bash
kubectl scale deployment/auction-runner deployment/api-gateway deployment/telemetry-service --replicas=0
kubectl delete -f atomix/store.yaml && kubectl apply -f atomix/store.yaml
kubectl scale deployment/auction-runner deployment/api-gateway deployment/telemetry-service --replicas=1
```

---

## Exporting Metrics

```bash
make prometheus-ui          # port-forward first
make export-metrics         # last hour → data/experiment-<timestamp>.json
make export-metrics SINCE=7200   # wider window (2h)
```

---

## Evaluation Measurements (Section 5)

### API Throughput (5.2.1)

```bash
go install github.com/rakyll/hey@latest

export API_URL=http://$LB_IP
make load-test-api API_URL=$API_URL CONCURRENCY=2
make load-test-api API_URL=$API_URL CONCURRENCY=5
make load-test-api API_URL=$API_URL CONCURRENCY=10
make load-test-api API_URL=$API_URL CONCURRENCY=20
```

### Auction Pipeline Latency (5.2.2)

```bash
make all experiment=perf-5bidders DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG
# wait ≥30 intervals (15 min at 30s/interval)
make measure-pipeline-latency

make all experiment=perf-10bidders DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG
make measure-pipeline-latency

# 2-bidder baseline
make all experiment=1 DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG
make measure-pipeline-latency
```

Output contains `[bids-collected]`, `[cleared]`, and `[published-to-kafka]` lines with `elapsed_ms` fields.

### Kafka Consumer Lag (5.2.3)

```bash
make measure-kafka-lag                        # point-in-time snapshot
make measure-kafka-lag-series COUNT=30 INTERVAL=10   # time series → data/kafka-lag.txt
```

### Control Loop E2E Latency (5.2.4)

```bash
make all experiment=7 DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG
# with prometheus-ui running:
make measure-e2e-latency SINCE=1800
```

The spike timestamp from the dummy-producer logs and the Prometheus `ixp_customer_allocation_kbps` series let you compute the E2E latency manually.

---

## VPN / Real-Switch Kafka

All Section 5 experiments use the **in-cluster Strimzi Kafka** — no VPN required.

For the real-switch scenario where a physical switch's telemetry is produced outside the cluster:

1. Deploy a WireGuard gateway pod to connect the cluster to your VPN.
2. Route Kafka broker traffic through the gateway.
3. Deploy with the VPN-reachable bootstrap address:

```bash
make deploy-real \
  KAFKA_BOOTSTRAP=10.x.x.x:9092 \
  KAFKA_TLS_CA_FILE=/etc/kafka-tls/ca.pem \
  KAFKA_TLS_CERT_FILE=/etc/kafka-tls/service.cert \
  KAFKA_TLS_KEY_FILE=/etc/kafka-tls/service.key \
  DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG
```

---

## Teardown

```bash
# Delete experiment agents
make delete-agents

# Delete dashboard
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
