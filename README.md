# ixp-gcp

### Prerequisites
- Go 1.25+
- Docker
- Kubectl
- Working Kubernetes cluster
- Helm

### Setup
```bash
make setup
```
This will register necessary helm repos.

### Quick Start
```bash
make all
make grafana-ui
```
This will set up all necessary infra and services.


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