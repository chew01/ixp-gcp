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
This will set up all necessary infra and services.

#### Grafana
```bash
make grafana-ui
```
This will port forward Grafana to port 3000. 

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