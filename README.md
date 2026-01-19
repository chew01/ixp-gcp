# ixp-gcp

### Telemetry Log Format

```
Schema v1:

{
  "schema_version": 1,
  "switch_id": "sw-1",
  "window_start_ns": 123,
  "window_end_ns": 456,
  "flows": [
    {
      "ingress_port": 1,
      "egress_port": 5,
      "vlan_id": 100,
      "bytes": 123456
    }
  ]
}
```

### Requirements
- Install Helm
- Install Helm charts for Atomix, Prometheus, Grafana