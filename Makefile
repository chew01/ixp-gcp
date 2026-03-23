VENDOR_MODULES = api auction dummy telemetry agent

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
	docker build -t api-gateway:local ./api
	minikube image load api-gateway:local
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
	docker build -t auction-runner:local ./auction
	minikube image load auction-runner:local
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
	docker build -t dummy-producer:local ./dummy
	minikube image load dummy-producer:local
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
	docker build -t telemetry-service:local ./telemetry
	minikube image load telemetry-service:local
	kubectl apply -f ./telemetry/deployment.yaml
	kubectl set env deployment/telemetry-service KAFKA_BOOTSTRAP=$(KAFKA_BOOTSTRAP)
ifdef KAFKA_TLS_CA_FILE
	kubectl set env deployment/telemetry-service \
		KAFKA_TLS_CA_FILE=$(KAFKA_TLS_CA_FILE) \
		KAFKA_TLS_CERT_FILE=$(KAFKA_TLS_CERT_FILE) \
		KAFKA_TLS_KEY_FILE=$(KAFKA_TLS_KEY_FILE)
endif

deploy-agent:
	@echo "==> Deploying Customer Agent..."
	docker build -t customer-agent:local ./agent
	minikube image load customer-agent:local
	kubectl apply -f ./agent/deployment.yaml

# ============================================================
# Grouped deploys
# ============================================================
.PHONY: infra services all

infra: deploy-atomix deploy-config deploy-monitoring
	$(MAKE) deploy-kafka $(if $(KAFKA_EXTERNAL),KAFKA_EXTERNAL=$(KAFKA_EXTERNAL),) KAFKA_BOOTSTRAP=$(KAFKA_BOOTSTRAP)

services: vendor deploy-api deploy-auction deploy-dummy deploy-telemetry deploy-agent

all: infra services

# ============================================================
# Experiments — swap scenario and restart all pods
# ============================================================
# Active scenario file loaded into the test-scenario ConfigMap.
# Override on the command line: make load-scenario SCENARIO=etc/scenario/experiment-1-baseline.yaml
SCENARIO ?= etc/scenario/scenario.yaml

.PHONY: load-scenario \
        deploy-experiment-1 \
        deploy-experiment-2a deploy-experiment-2b \
        deploy-experiment-3 \
        deploy-experiment-4a deploy-experiment-4b \
        deploy-experiment-5 \
        deploy-experiment-6a deploy-experiment-6b deploy-experiment-6c

load-scenario:
	@echo "==> Loading scenario: $(SCENARIO)"
	kubectl create configmap test-scenario \
		--from-file=scenario.yaml=$(SCENARIO) \
		-o yaml --dry-run=client | kubectl apply -f -
	kubectl rollout restart deployment/dummy-producer
	kubectl rollout restart deployment/auction-runner
	kubectl rollout restart deployment/telemetry-service
	kubectl rollout restart deployment/api-gateway
	kubectl rollout restart deployment/customer-agent-as12345
	kubectl rollout restart deployment/customer-agent-as67890
	@echo "==> Waiting for rollout..."
	kubectl rollout status deployment/dummy-producer --timeout=90s
	kubectl rollout status deployment/auction-runner --timeout=90s
	kubectl rollout status deployment/api-gateway --timeout=90s
	@echo "==> Scenario active: $(SCENARIO)"

deploy-experiment-1:
	$(MAKE) load-scenario SCENARIO=etc/scenario/experiment-1-baseline.yaml

# Experiment 2 — Drop-Rate Algorithm vs. Fixed-Margin
# Run A first, export metrics, reset, then Run B.
deploy-experiment-2a:
	$(MAKE) load-scenario SCENARIO=etc/scenario/experiment-2a-conservative-spike.yaml

deploy-experiment-2b:
	$(MAKE) load-scenario SCENARIO=etc/scenario/experiment-2b-demand-corrected-spike.yaml

# Experiment 3 — Heterogeneous Strategies: Market Dynamics
deploy-experiment-3:
	$(MAKE) load-scenario SCENARIO=etc/scenario/experiment-3-heterogeneous.yaml

# Experiment 4 — Budget Awareness and Credit Exhaustion
# Run A first (conservative), export metrics, then Run B (budget_aware).
deploy-experiment-4a:
	$(MAKE) load-scenario SCENARIO=etc/scenario/experiment-4a-conservative-budget.yaml

deploy-experiment-4b:
	$(MAKE) load-scenario SCENARIO=etc/scenario/experiment-4b-budget-aware.yaml

# Experiment 5 — Auction Convergence and Stability
deploy-experiment-5:
	$(MAKE) load-scenario SCENARIO=etc/scenario/experiment-5-convergence.yaml

# Experiment 6 — Sensitivity to Auction Interval
# Run each for 10 minutes, export metrics between runs.
deploy-experiment-6a:
	$(MAKE) load-scenario SCENARIO=etc/scenario/experiment-6a-interval-10s.yaml

deploy-experiment-6b:
	$(MAKE) load-scenario SCENARIO=etc/scenario/experiment-6b-interval-30s.yaml

deploy-experiment-6c:
	$(MAKE) load-scenario SCENARIO=etc/scenario/experiment-6c-interval-60s.yaml

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
	printf '}' >> $$FILE; \
	echo "Saved to $$FILE"

stop:
	minikube delete

proto:
	cd shared/proto && mkdir -p pb && protoc -I . --go_out=pb --go_opt=paths=source_relative *.proto

test:
	@echo "==> Running unit tests..."
	cd api && go test ./... && cd ..
	cd agent && go test ./... && cd ..