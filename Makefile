VENDOR_MODULES = api auction dummy telemetry agent dashboard

# ============================================================
# External Kafka support
#
# Plaintext (in-cluster Strimzi default):
#   make infra
#
# External cluster, plaintext:
#   make infra KAFKA_EXTERNAL=true KAFKA_BOOTSTRAP=192.168.1.50:9092
#   make services KAFKA_BOOTSTRAP=192.168.1.50:9092
#
# External cluster with mTLS (e.g. Aiven):
#   kubectl create secret generic kafka-tls \
#     --from-file=ca.pem --from-file=service.cert --from-file=service.key
#   (add the volume/volumeMount for the secret to each deployment.yaml)
#   make services \
#     KAFKA_BOOTSTRAP=kafka-xxx.aivencloud.com:12345 \
#     KAFKA_TLS_CA_FILE=/etc/kafka-tls/ca.pem \
#     KAFKA_TLS_CERT_FILE=/etc/kafka-tls/service.cert \
#     KAFKA_TLS_KEY_FILE=/etc/kafka-tls/service.key
# ============================================================
KAFKA_BOOTSTRAP    ?= ixp-kafka-kafka-bootstrap:9092
KAFKA_TLS_CA_FILE  ?=
KAFKA_TLS_CERT_FILE ?=
KAFKA_TLS_KEY_FILE  ?=


# ============================================================
# Individual deploys
# ============================================================
.PHONY: deploy-minikube deploy-kafka deploy-atomix deploy-api \
        deploy-auction deploy-dummy deploy-telemetry deploy-agent deploy-monitoring

deploy-api:
	@echo "==> Deploying API Gateway..."
	eval $$(minikube docker-env) && docker build -t api-gateway:local ./api
	kubectl apply -f ./api/ingress.yaml
	kubectl apply -f ./api/deployment.yaml
	kubectl apply -f ./api/service-monitor.yaml

deploy-atomix:
	@echo "==> Deploying Atomix..."
	helm install -n kube-system atomix-runtime atomix/atomix-runtime --wait
	kubectl apply -f ./atomix/storage-profile.yaml
	kubectl apply -f ./atomix/store.yaml

deploy-auction:
	@echo "==> Deploying Auction Runner..."
	eval $$(minikube docker-env) && docker build -t auction-runner:local ./auction
	kubectl apply -f ./auction/deployment.yaml
	kubectl set env deployment/auction-runner KAFKA_BOOTSTRAP=$(KAFKA_BOOTSTRAP)
ifdef KAFKA_TLS_CA_FILE
	kubectl set env deployment/auction-runner \
		KAFKA_TLS_CA_FILE=$(KAFKA_TLS_CA_FILE) \
		KAFKA_TLS_CERT_FILE=$(KAFKA_TLS_CERT_FILE) \
		KAFKA_TLS_KEY_FILE=$(KAFKA_TLS_KEY_FILE)
endif

deploy-config:
	@echo "==> Deploying Scenario Config..."
	kubectl create configmap test-scenario \
		--from-file=scenario.yaml=./etc/scenario/scenario.yaml \
		-o yaml --dry-run=client | kubectl apply -f -

deploy-dummy:
	@echo "==> Deploying Dummy Producer..."
	eval $$(minikube docker-env) && docker build -t dummy-producer:local ./dummy
	kubectl apply -f ./dummy/deployment.yaml
	kubectl set env deployment/dummy-producer KAFKA_BOOTSTRAP=$(KAFKA_BOOTSTRAP)
ifdef KAFKA_TLS_CA_FILE
	kubectl set env deployment/dummy-producer \
		KAFKA_TLS_CA_FILE=$(KAFKA_TLS_CA_FILE) \
		KAFKA_TLS_CERT_FILE=$(KAFKA_TLS_CERT_FILE) \
		KAFKA_TLS_KEY_FILE=$(KAFKA_TLS_KEY_FILE)
endif

deploy-kafka:
ifndef KAFKA_EXTERNAL
	@echo "==> Deploying Kafka (in-cluster Strimzi)..."
	helm install strimzi-cluster-operator oci://quay.io/strimzi-helm/strimzi-kafka-operator
	kubectl apply -f ./kafka/kafka.yaml
	kubectl wait kafka/ixp-kafka --for=condition=Ready --timeout=300s
else
	@echo "Skipping Strimzi — using external Kafka at $(KAFKA_BOOTSTRAP)"
endif

deploy-minikube:
	@echo "==> Deploying Minikube..."
	minikube start --cpus=4 --memory=8192
	minikube addons enable ingress

deploy-monitoring:
	helm upgrade --install monitoring prometheus-community/kube-prometheus-stack \
		--namespace monitoring --create-namespace \
		-f monitoring/values.yaml
	kubectl create configmap ixp-flows-dashboard \
		--from-file=ixp-flows.json=./monitoring/ixp-flows.json \
		-n monitoring -o yaml --dry-run=client | kubectl apply -f -
	kubectl label configmap ixp-flows-dashboard \
		-n monitoring grafana_dashboard="1" --overwrite


deploy-telemetry:
	@echo "==> Deploying Telemetry Processor..."
	eval $$(minikube docker-env) && docker build -t telemetry-service:local ./telemetry
	kubectl apply -f ./telemetry/deployment.yaml
	kubectl set env deployment/telemetry-service KAFKA_BOOTSTRAP=$(KAFKA_BOOTSTRAP)
ifdef KAFKA_TLS_CA_FILE
	kubectl set env deployment/telemetry-service \
		KAFKA_TLS_CA_FILE=$(KAFKA_TLS_CA_FILE) \
		KAFKA_TLS_CERT_FILE=$(KAFKA_TLS_CERT_FILE) \
		KAFKA_TLS_KEY_FILE=$(KAFKA_TLS_KEY_FILE)
endif

deploy-agent:
	@echo "==> Deploying Customer Agent (manual single-agent testing only)..."
	eval $$(minikube docker-env) && docker build -t customer-agent:local ./agent
	kubectl apply -f ./agent/deployment.yaml

# ============================================================
# Grouped deploys
# ============================================================
.PHONY: infra services all load-experiment delete-agents deploy-real

infra: deploy-atomix deploy-config deploy-monitoring
	$(MAKE) deploy-kafka $(if $(KAFKA_EXTERNAL),KAFKA_EXTERNAL=$(KAFKA_EXTERNAL),) KAFKA_BOOTSTRAP=$(KAFKA_BOOTSTRAP)

# Core services only — no dummy switch, no agents.
services: vendor deploy-api deploy-auction deploy-telemetry

# Default: deploy the core control plane only (no experiment traffic, no agents).
# To also run an experiment: make all experiment=2a
experiment ?=

ifndef experiment
all: infra services
else
EXPERIMENT_SCENARIO = $(firstword $(wildcard etc/scenario/experiment-$(experiment)-*.yaml etc/scenario/experiment-$(experiment).yaml))
all: infra services load-experiment
endif

delete-agents:
	kubectl delete deployment -l app=customer-agent --ignore-not-found

load-experiment:
	@test -f $(EXPERIMENT_SCENARIO) || (echo "Unknown experiment: $(experiment)"; exit 1)
	@echo "==> Loading experiment scenario: $(EXPERIMENT_SCENARIO)"
	kubectl create configmap test-scenario \
		--from-file=scenario.yaml=$(EXPERIMENT_SCENARIO) \
		-o yaml --dry-run=client | kubectl apply -f -
	$(MAKE) deploy-dummy
	@echo "==> Building customer-agent image..."
	eval $$(minikube docker-env) && docker build -t customer-agent:local ./agent
	kubectl rollout restart deployment/auction-runner
	kubectl rollout restart deployment/telemetry-service
	kubectl rollout restart deployment/api-gateway
	kubectl rollout status deployment/auction-runner --timeout=90s
	kubectl rollout status deployment/api-gateway --timeout=90s
	$(MAKE) delete-agents
	cd scripts/gen-agent-deployments && go run . ../../$(EXPERIMENT_SCENARIO) | kubectl apply -f -
	@echo "==> Experiment $(experiment) is live."

# ============================================================
# Experiments — swap scenario, restart pods, redeploy agents
# ============================================================
# Preferred: make all experiment=2a
# Legacy aliases kept for backward compatibility.
#
# Experiment index:
#   1    — Baseline agent correctness (conservative × 2)
#   2a   — Conservative vs conservative, spike traffic
#   2b   — Conservative vs demand_corrected, spike traffic
#   3    — Conservative vs price_insensitive, heterogeneous
#   4a   — Conservative with finite budget
#   4b   — Budget-aware with finite budget
#   4c   — Throughput optimizer vs conservative
#   5    — Auction convergence and stability
#   6a   — Sensitivity: 10s interval
#   6b   — Sensitivity: 30s interval
#   6c   — Sensitivity: 60s interval
#   7    — Valuation-based dominant strategy vs conservative
#   7b   — Q-learning convergence vs valuation_based
#   8    — Mixed valuations (same strategy, different valuations)
#   9    — EMA negative result vs valuation_based

.PHONY: load-scenario \
        deploy-experiment-1 \
        deploy-experiment-2a deploy-experiment-2b \
        deploy-experiment-3 \
        deploy-experiment-4a deploy-experiment-4b deploy-experiment-4c \
        deploy-experiment-5 \
        deploy-experiment-6a deploy-experiment-6b deploy-experiment-6c \
        deploy-experiment-7 deploy-experiment-7b \
        deploy-experiment-8 \
        deploy-experiment-9

# deploy-real: load etc/scenario/scenario.yaml against a real switch.
# Pushes the config, restarts core services, and deploys one agent pod per
# customer. No dummy producer is started.
deploy-real:
	@echo "==> Loading real-switch scenario..."
	$(MAKE) load-scenario
	@echo "==> Building customer-agent image..."
	eval $$(minikube docker-env) && docker build -t customer-agent:local ./agent
	$(MAKE) delete-agents
	cd scripts/gen-agent-deployments && go run . ../../etc/scenario/scenario.yaml | kubectl apply -f -
	@echo "==> Real-switch scenario live."

# load-scenario: bare reload (no agent restart) — for manual scenario swaps.
SCENARIO ?= etc/scenario/scenario.yaml
load-scenario:
	@echo "==> Loading scenario: $(SCENARIO)"
	kubectl create configmap test-scenario \
		--from-file=scenario.yaml=$(SCENARIO) \
		-o yaml --dry-run=client | kubectl apply -f -
	kubectl rollout restart deployment/auction-runner
	kubectl rollout restart deployment/telemetry-service
	kubectl rollout restart deployment/api-gateway
	@echo "==> Waiting for rollout..."
	kubectl rollout status deployment/auction-runner --timeout=90s
	kubectl rollout status deployment/api-gateway --timeout=90s
	@echo "==> Scenario active: $(SCENARIO)"

deploy-experiment-1:
	$(MAKE) load-experiment experiment=1

# Experiment 2 — Drop-Rate Algorithm vs. Fixed-Margin
deploy-experiment-2a:
	$(MAKE) load-experiment experiment=2a

deploy-experiment-2b:
	$(MAKE) load-experiment experiment=2b

# Experiment 3 — Heterogeneous Strategies
deploy-experiment-3:
	$(MAKE) load-experiment experiment=3

# Experiment 4 — Budget Awareness and Credit Exhaustion
deploy-experiment-4a:
	$(MAKE) load-experiment experiment=4a

deploy-experiment-4b:
	$(MAKE) load-experiment experiment=4b

deploy-experiment-4c:
	$(MAKE) load-experiment experiment=4c

# Experiment 5 — Auction Convergence and Stability
deploy-experiment-5:
	$(MAKE) load-experiment experiment=5

# Experiment 6 — Sensitivity to Auction Interval
deploy-experiment-6a:
	$(MAKE) load-experiment experiment=6a

deploy-experiment-6b:
	$(MAKE) load-experiment experiment=6b

deploy-experiment-6c:
	$(MAKE) load-experiment experiment=6c

# Experiment 7 — Dominant strategy validation
deploy-experiment-7:
	$(MAKE) load-experiment experiment=7

deploy-experiment-7b:
	$(MAKE) load-experiment experiment=7b

# Experiment 8 — Mixed valuations
deploy-experiment-8:
	$(MAKE) load-experiment experiment=8

# Experiment 9 — EMA negative result
deploy-experiment-9:
	$(MAKE) load-experiment experiment=9

# ============================================================
# Utilities
# ============================================================
.PHONY: vendor logs setup grafana-ui prometheus-ui stop test export-metrics

vendor:
	@for mod in $(VENDOR_MODULES); do \
		echo "==> Vendoring $$mod..."; \
		cd $$mod && go mod vendor && cd ..; \
	done

logs:
	kubectl logs -l app=$(SERVICE) -f --namespace $(NAMESPACE)

setup:
	helm repo add atomix https://atomix.github.io/charts.atomix.io
	helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
	helm repo update

# Prometheus URL used by export-metrics (override for in-cluster or remote access).
# Run `make prometheus-ui` first to forward the port, then use the default.
PROMETHEUS_URL ?= http://localhost:9090

grafana-ui:
	kubectl port-forward svc/monitoring-grafana 3000:80 -n monitoring &
	@echo "Grafana at http://localhost:3000"
	@echo "Password: $$(kubectl get secret monitoring-grafana -n monitoring \
		-o jsonpath='{.data.admin-password}' | base64 --decode)"

prometheus-ui:
	kubectl port-forward svc/monitoring-kube-prometheus-prometheus 9090:9090 -n monitoring &
	@echo "Prometheus at http://localhost:9090"

# Export key experiment metrics from Prometheus for the last hour.
# Usage: make export-metrics [PROMETHEUS_URL=http://...] [SINCE=2h]
# Output: data/experiment-<timestamp>.json
SINCE ?= 1 hour
export-metrics:
	@mkdir -p data
	@TS=$$(date +%Y%m%d-%H%M%S); FILE=data/experiment-$$TS.json; \
	printf '{"clearing_price":' > $$FILE; \
	curl -sG "$(PROMETHEUS_URL)/api/v1/query_range" \
		--data-urlencode "query=ixp_auction_clearing_price" \
		--data-urlencode "start=$$(date -d '$(SINCE) ago' +%s)" \
		--data-urlencode "end=$$(date +%s)" \
		--data-urlencode "step=30s" >> $$FILE; \
	printf ',"allocation_kbps":' >> $$FILE; \
	curl -sG "$(PROMETHEUS_URL)/api/v1/query_range" \
		--data-urlencode "query=ixp_customer_allocation_kbps" \
		--data-urlencode "start=$$(date -d '$(SINCE) ago' +%s)" \
		--data-urlencode "end=$$(date +%s)" \
		--data-urlencode "step=30s" >> $$FILE; \
	printf ',"flow_drop_rate":' >> $$FILE; \
	curl -sG "$(PROMETHEUS_URL)/api/v1/query_range" \
		--data-urlencode "query=ixp_flow_drop_rate_percent" \
		--data-urlencode "start=$$(date -d '$(SINCE) ago' +%s)" \
		--data-urlencode "end=$$(date +%s)" \
		--data-urlencode "step=30s" >> $$FILE; \
	printf ',"flow_throughput":' >> $$FILE; \
	curl -sG "$(PROMETHEUS_URL)/api/v1/query_range" \
		--data-urlencode "query=ixp_flow_throughput_kbps" \
		--data-urlencode "start=$$(date -d '$(SINCE) ago' +%s)" \
		--data-urlencode "end=$$(date +%s)" \
		--data-urlencode "step=30s" >> $$FILE; \
	printf ',"utility_per_round":' >> $$FILE; \
	curl -sG "$(PROMETHEUS_URL)/api/v1/query_range" \
		--data-urlencode "query=increase(ixp_agent_utility_total[30s])" \
		--data-urlencode "start=$$(date -d '$(SINCE) ago' +%s)" \
		--data-urlencode "end=$$(date +%s)" \
		--data-urlencode "step=30s" >> $$FILE; \
	printf ',"cumulative_utility":' >> $$FILE; \
	curl -sG "$(PROMETHEUS_URL)/api/v1/query_range" \
		--data-urlencode "query=ixp_agent_utility_total" \
		--data-urlencode "start=$$(date -d '$(SINCE) ago' +%s)" \
		--data-urlencode "end=$$(date +%s)" \
		--data-urlencode "step=30s" >> $$FILE; \
	printf '}' >> $$FILE; \
	echo "Saved to $$FILE"

# ============================================================
# Dashboard
# ============================================================
.PHONY: build-dashboard deploy-dashboard dashboard-ui

build-dashboard:
	@echo "==> Vendoring dashboard dependencies..."
	cd dashboard && go mod vendor
	@echo "==> Building dashboard Docker image (includes frontend build)..."
	eval $$(minikube docker-env) && docker build -t dashboard:local ./dashboard

deploy-dashboard:
	@echo "==> Deploying Dashboard..."
	kubectl apply -f dashboard/rbac.yaml
	cd dashboard && go mod vendor
	eval $$(minikube docker-env) && docker build -t dashboard:local ./dashboard
	kubectl apply -f dashboard/deployment.yaml
	kubectl set env deployment/dashboard KAFKA_BOOTSTRAP=$(KAFKA_BOOTSTRAP)
ifdef KAFKA_TLS_CA_FILE
	kubectl set env deployment/dashboard \
		KAFKA_TLS_CA_FILE=$(KAFKA_TLS_CA_FILE) \
		KAFKA_TLS_CERT_FILE=$(KAFKA_TLS_CERT_FILE) \
		KAFKA_TLS_KEY_FILE=$(KAFKA_TLS_KEY_FILE)
endif

dashboard-ui:
	@echo "==> Opening dashboard at http://localhost:8082 ..."
	kubectl port-forward svc/dashboard 8082:8082

stop:
	minikube delete

proto:
	cd shared/proto && mkdir -p pb && protoc -I . --go_out=pb --go_opt=paths=source_relative *.proto

test:
	@echo "==> Running unit tests..."
	cd api && go test ./... && cd ..
	cd agent && go test ./... && cd ..
	cd scripts/gen-agent-deployments && go test ./... && cd ../..