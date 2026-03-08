# ixp-gcp

### Prerequisites
- [Go 1.25+](https://go.dev/doc/install)
- [Docker](https://docs.docker.com/engine/install)
- [Kubectl](https://kubernetes.io/docs/tasks/tools)
- Working Kubernetes cluster
- [Helm](helm.sh/docs/intro/install)

### Setup
```bash
make setup
```
This will register necessary helm repos.

### Quick Start
```bash
make all
```
This will set up all necessary infra and services, including:
- **Infrastructure**: Minikube, Kafka, Atomix
- **Observability**: Prometheus, Grafana, OTEL Collector, Jaeger, Loki
- **Services**: API Gateway, Auction Runner, Telemetry Processor, Dummy Producer

#### Observability & Monitoring

The system includes a complete observability stack for metrics, traces, and logs:

```bash
# Access Grafana (business metrics dashboard)
make grafana-ui
# Then visit: http://localhost:3000 (admin/admin)

# Access Prometheus (raw metrics)
kubectl port-forward -n monitoring svc/monitoring-kube-prometheus-prometheus 9090:9090

# Access Jaeger (distributed tracing)
kubectl port-forward -n monitoring svc/jaeger-all-in-one-query 16686:16686
```

**Business Metrics Tracked:**
- `ixp_flow_throughput` - Flow throughput in Kbps per switch/port
- `ixp_flow_drop_rate` - Flow packet drop rate (planned)

See [docs/observability-architecture.md](docs/observability-architecture.md) for detailed architecture information.

### References
- [Atomix](https://atomix.github.io)

### Consuming Auction Results
```bash
kubectl exec -it ixp-kafka-dual-role-0 -- \
bin/kafka-console-consumer.sh \
--bootstrap-server localhost:9092 \
--topic auction-results \       
--from-beginning
```
This prints all the records since the beginning.

### Telemetry Log Format

- Key: switch id
- Value: see [shared/structs.go]()