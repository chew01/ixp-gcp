# IXP-GCP — Internet Exchange Point Control Plane

A research control plane for studying bidding strategies in a bandwidth auction market. Runs on Kubernetes and implements a uniform-price (second-price) auction for egress bandwidth allocation. Customer agents bid autonomously using configurable strategies; results are exposed via Prometheus and Grafana.

---

## Quick Start (Minikube)

**Prerequisites:** Go 1.22+, Docker, kubectl, Helm, Minikube

```bash
make setup            # register Helm repos
make deploy-minikube  # start Minikube (4 CPUs, 8 GiB RAM)
make all              # deploy infra + core services
```

Run an experiment:

```bash
make all experiment=2a   # deploy with experiment scenario loaded
make grafana-ui          # Grafana at http://localhost:3000
make export-metrics      # save data/experiment-<timestamp>.json
```

---

## Docs

| Guide | Description |
|-------|-------------|
| [docs/LOCAL-DEPLOYMENT.md](docs/LOCAL-DEPLOYMENT.md) | Full local (Minikube) setup, dashboard, rebuilds, troubleshooting |
| [docs/CLOUD-DEPLOYMENT.md](docs/CLOUD-DEPLOYMENT.md) | DigitalOcean Kubernetes (DOKS) deployment |
| [docs/EXPERIMENTS.md](docs/EXPERIMENTS.md) | Experiment reference table and measurement commands |
| [docs/AGENTS.md](docs/AGENTS.md) | Bidding strategy reference |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | System architecture |

---

## References

- [Atomix](https://atomix.github.io)
- Vickrey, W. (1961). Counterspeculation, Auctions, and Competitive Sealed Tenders. *Journal of Finance*, 16(1), 8–37. https://doi.org/10.1111/j.1540-6261.1961.tb02789.x
