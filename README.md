# IXP-GCP — Internet Exchange Point Control Plane

A research control plane for studying bidding strategies in a bandwidth auction market. The system runs on Kubernetes (Minikube for local development) and implements a uniform-price (second-price) auction mechanism for egress bandwidth allocation. Customer agents bid autonomously using configurable strategies; results are exposed via Prometheus and Grafana.

**Scope boundary:** this project implements the control plane (API, auction runner, telemetry processor, bidding agents). Traffic enforcement on the switch is handled by a separate project; this system communicates with the switch via Kafka.

---

## Prerequisites and Setup

- [Go 1.25+](https://go.dev/doc/install)
- [Docker](https://docs.docker.com/engine/install)
- [kubectl](https://kubernetes.io/docs/tasks/tools)
- [Helm](https://helm.sh/docs/intro/install)
- A running Kubernetes cluster (Minikube recommended — see `docs/DEPLOYMENT.md`)

```bash
make setup         # register Helm repos (atomix, prometheus-community)
make deploy-minikube  # start Minikube with 4 CPUs and 8 GiB (first time only)
```

---

## Deploying Against a Real Switch

Use this path when the switch is generating live traffic and no dummy producer is needed. The scenario is configured in `etc/scenario/scenario.yaml`.

The real switch publishes telemetry to an **external Kafka broker**, so Strimzi is not deployed and every `make` command that touches services must receive the broker address.

**First time only — cluster setup:**

```bash
make setup            # register Helm repos
make deploy-minikube  # start Minikube (4 CPUs, 8 GiB)
make all KAFKA_EXTERNAL=true KAFKA_BOOTSTRAP=<host:port>
```

If the broker requires mTLS (e.g. Aiven), create the TLS secret first and pass the certificate paths:

```bash
kubectl create secret generic kafka-tls \
  --from-file=ca.pem --from-file=service.cert --from-file=service.key

make infra KAFKA_EXTERNAL=true KAFKA_BOOTSTRAP=<host:port>
make services \
  KAFKA_BOOTSTRAP=<host:port> \
  KAFKA_TLS_CA_FILE=/etc/kafka-tls/ca.pem \
  KAFKA_TLS_CERT_FILE=/etc/kafka-tls/service.cert \
  KAFKA_TLS_KEY_FILE=/etc/kafka-tls/service.key
```

**Every subsequent run (or after editing `scenario.yaml`):**

```bash
make deploy-real
```

This single target:
1. Pushes `etc/scenario/scenario.yaml` as the `test-scenario` ConfigMap
2. Restarts `auction-runner`, `telemetry-service`, and `api-gateway` and waits for rollout
3. Builds the `customer-agent:local` image
4. Removes any previously running agent pods
5. Generates and applies one agent Deployment per customer in the scenario (no dummy producer)

Kafka connection details are baked into the service Deployments at `make services` time, so `deploy-real` does not need them again.

**Observe:**

```bash
make grafana-ui    # Grafana at http://localhost:3000
make prometheus-ui # Prometheus at http://localhost:9090
```

> Before running `deploy-real`, review `etc/scenario/scenario.yaml` and update `max_capacity` on the switch entry to the measured egress line-rate of the physical port (in kbps).

---

## Running an Experiment

Experiments use a dummy traffic producer to simulate switch telemetry on a local cluster. Each has its own scenario YAML under `etc/scenario/`.

**First time only — cluster setup:**

```bash
make setup            # register Helm repos
make deploy-minikube  # start Minikube (4 CPUs, 8 GiB)
```

**Deploy the full stack for a specific experiment:**

```bash
make all experiment=2a
```

This single command:
1. Deploys Atomix, Kafka, Prometheus/Grafana, and the scenario config (`infra`)
2. Builds and deploys the API gateway, auction runner, and telemetry processor (`services`)
3. Loads the scenario YAML for experiment `2a` as the active ConfigMap
4. Starts the dummy traffic producer
5. Builds the `customer-agent:local` image and deploys one agent pod per customer

**Observe:**

```bash
make grafana-ui    # Grafana at http://localhost:3000
make prometheus-ui # Prometheus at http://localhost:9090
```

**After the run:**

```bash
make export-metrics   # saves data/experiment-<timestamp>.json
```

**Switching experiments on a running cluster** (faster than a full redeploy):

```bash
make all experiment=7
```

Or to swap the scenario without rebuilding everything:

```bash
make load-experiment experiment=7
```

See [docs/AGENTS.md](docs/AGENTS.md) for strategy reference and [docs/EXPERIMENTS.md](docs/EXPERIMENTS.md) for the full experiment guide.

---

## References

- [Atomix](https://atomix.github.io)
- [Vickrey, W. (1961). Counterspeculation, Auctions, and Competitive Sealed Tenders. *Journal of Finance*, 16(1), 8–37.](https://doi.org/10.1111/j.1540-6261.1961.tb02789.x)
