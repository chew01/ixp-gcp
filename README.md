# ixp-gcp

### Telemetry Log Format

- Key: switch id
- Value: see [shared/structs.go]()

### Requirements
- Install Helm
- Install Helm charts for Atomix, Prometheus, Grafana

```bash
make setup
```

### Design
- Throughput for bids is in kbps
- Throughput for telemetry entries is coerced to nearest kbps

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