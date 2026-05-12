VENDOR_MODULES = api auction dummy telemetry agent

# Load .env file if it exists
-include .env
export $(shell sed 's/=.*//' .env 2>/dev/null)

# ============================================================
# Individual deploys
# ============================================================
.PHONY: deploy-minikube deploy-kafka deploy-atomix deploy-api \
		deploy-auction deploy-dummy deploy-telemetry deploy-agent deploy-observability

deploy-api:
	@echo "==> Deploying API Gateway..."
	eval $$(minikube docker-env) && docker build -t api-gateway:local ./api
	kubectl apply -f ./api/ingress.yaml
	kubectl apply -f ./api/deployment.yaml
	kubectl apply -f ./api/service-monitor.yaml

deploy-atomix:
	@echo "==> Deploying Atomix..."
	helm upgrade --install -n kube-system atomix-runtime atomix/atomix-runtime\
		--set image.pullPolicy=IfNotPresent
	kubectl apply -f ./atomix/storage-profile.yaml
	kubectl apply -f ./atomix/store.yaml

deploy-auction:
	@echo "==> Deploying Auction Runner..."
	eval $$(minikube docker-env) && docker build -t auction-runner:local ./auction
	kubectl apply -f ./auction/deployment.yaml

deploy-config:
	@echo "==> Deploying Scenario Config..."
	kubectl create configmap test-scenario --from-file=scenario.yaml=./etc/scenario/scenario.yaml \
		-o yaml --dry-run=client | kubectl apply -f -

deploy-dummy:
	@echo "==> Deploying Dummy Producer..."
	eval $$(minikube docker-env) && docker build -t dummy-producer:local ./dummy
	kubectl apply -f ./dummy/deployment.yaml

deploy-kafka:
	@echo "==> Deploying Kafka..."
	helm upgrade --install strimzi-cluster-operator oci://quay.io/strimzi-helm/strimzi-kafka-operator \
		--version 1.0.0 \
  		--set image.pullPolicy=IfNotPresent \
  		--set kafkaOperator.image.pullPolicy=IfNotPresent
	kubectl apply -f ./kafka/kafka.yaml
	kubectl wait kafka/ixp-kafka --for=condition=Ready --timeout=3000s

deploy-minikube:
	minikube start --cpus=4 --memory=8192
# 		--docker-env HTTP_PROXY=http://docker.internal \
# 		--docker-env HTTPS_PROXY=http://docker.internal \
# 		--docker-env NO_PROXY=localhost,127.0.0.1,10.96.0.0/12,192.168.49.0/24,host.docker.internal;
# 	minikube addons enable ingress

deploy-observability:
	@echo "==> Deploying Unified Observability Stack..."
	@echo "    - Prometheus (metrics storage & scraping)"
	@echo "    - Grafana (visualization)"
	@echo "    - OpenTelemetry Collector (metrics/traces/logs ingestion)"
	@echo "    - Jaeger (distributed tracing)"
	@echo "    - Loki (log aggregation)"
	@echo ""
	
	# Deploy kube-prometheus-stack (Prometheus + Grafana + Operator)
	@echo "==> Installing Prometheus & Grafana..."
	helm upgrade --install observability prometheus-community/kube-prometheus-stack \
		--namespace observability --create-namespace \
		-f observability/values.yaml \
		--set grafana.image.pullPolicy=IfNotPresent \
		--set prometheus.prometheusSpec.image.pullPolicy=IfNotPresent \
		--set prometheusOperator.image.pullPolicy=IfNotPresent \
		--timeout 5m
	@echo "==> Waiting for Prometheus & Grafana to be ready (max 2 min)..."
	kubectl rollout status deployment/observability-grafana -n observability --timeout=120s 2>/dev/null || true
	kubectl rollout status deployment/observability-kube-prometheus-prometheus-operator -n observability --timeout=120s 2>/dev/null || true
	
	# Deploy OpenTelemetry Collector
	@echo "==> Installing OpenTelemetry Collector..."
	helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts || true
	helm repo update
	helm upgrade --install otel-collector open-telemetry/opentelemetry-collector \
		--namespace observability \
		-f helm/opentelemetry-collector/values.yaml \
		--set image.pullPolicy=IfNotPresent \
		--timeout 5m
	@echo "==> Waiting for OpenTelemetry Collector (max 1 min)..."
	kubectl rollout status daemonset/otel-collector-daemonset -n observability --timeout=60s 2>/dev/null || true
	
	# Deploy Jaeger (all-in-one for development)
	@echo "==> Installing Jaeger..."
	helm repo add jaegertracing https://jaegertracing.github.io/helm-charts || true
	helm repo update
	helm upgrade --install jaeger jaegertracing/jaeger \
		--namespace observability \
		--set allInOne.enabled=true \
		--set allInOne.image.pullPolicy=IfNotPresent \
		--set storage.type=memory \
		--set agent.enabled=false \
		--set collector.enabled=false \
		--set query.enabled=false \
		--timeout 5m
	@echo "==> Waiting for Jaeger (max 1 min)..."
	kubectl rollout status deployment/jaeger -n observability --timeout=60s 2>/dev/null || true
	
	# Deploy Loki (log aggregation)
	@echo "==> Installing Loki..."
	helm repo add grafana https://grafana.github.io/helm-charts || true
	helm repo update
	helm upgrade --install loki grafana/loki \
		--namespace observability \
		-f helm/loki/values.yaml \
		--set loki.image.pullPolicy=IfNotPresent \
		--timeout 5m
	@echo "==> Waiting for Loki (max 1 min)..."
	kubectl rollout status deployment/loki -n observability --timeout=60s 2>/dev/null || true
	
	# Apply IXP Flows dashboard
	@echo "==> Deploying IXP Flows dashboard..."
	kubectl create configmap ixp-flows-dashboard \
		--from-file=ixp-flows.json=./observability/ixp-flows.json \
		-n observability -o yaml --dry-run=client | kubectl apply -f -
	kubectl label configmap ixp-flows-dashboard \
		-n observability grafana_dashboard="1" --overwrite

	# Apply IXP Mission Control dashboard
	@echo "==> Deploying IXP Mission Control dashboard..."
	kubectl create configmap ixp-mission-control-dashboard \
		--from-file=ixp-mission-control.json=./observability/ixp-mission-control.json \
		-n observability -o yaml --dry-run=client | kubectl apply -f -
	kubectl label configmap ixp-mission-control-dashboard \
		-n observability grafana_dashboard="1" --overwrite

	# Apply custom IXP alert rules
	@echo "==> Applying IXP alert rules..."
	kubectl apply -f ./observability/alerts-ixp.yaml

	# Configure Telegram secret only if TELEGRAM_BOT_TOKEN is provided.
	# This keeps deploy idempotent and avoids committing secrets to source control.
	@if [ -n "$$TELEGRAM_BOT_TOKEN" ]; then \
		echo "==> Applying Telegram secret from TELEGRAM_BOT_TOKEN..."; \
		kubectl -n observability create secret generic alertmanager-telegram-secret \
			--from-literal=bot-token="$$TELEGRAM_BOT_TOKEN" \
			-o yaml --dry-run=client | kubectl apply -f -; \
	else \
		echo "==> TELEGRAM_BOT_TOKEN not set; keeping existing alertmanager-telegram-secret"; \
	fi

	@echo ""
	@echo "✅ Observability stack deployed successfully!"
	@echo ""
	@echo "Access the UIs:"
	@echo "  Grafana:    kubectl port-forward -n observability svc/observability-grafana 3000:80"
	@echo "  Prometheus: kubectl port-forward -n observability svc/observability-kube-prometheus-prometheus 9090:9090"
	@echo "  Jaeger:     kubectl port-forward -n observability svc/jaeger 16686:16686"
	@echo ""
	@echo "Grafana credentials: admin / admin"


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

infra: deploy-minikube preload-images deploy-kafka deploy-atomix deploy-config deploy-observability

services: vendor deploy-api deploy-auction deploy-telemetry deploy-dummy deploy-agent

# # Core services only — no dummy switch, no agents.
# services: vendor deploy-api deploy-auction deploy-telemetry

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
.PHONY: vendor logs setup grafana-ui stop test preload-images

vendor:
	@for mod in $(VENDOR_MODULES); do \
		echo "==> Vendoring $$mod..."; \
		cd $$mod && go mod vendor && cd ..; \
	done

logs:
	kubectl logs -l app=$(SERVICE) -f --namespace $(NAMESPACE)

setup:
	@echo "==> Setting up Helm repositories..."
	helm repo add atomix https://atomix.github.io/charts.atomix.io
	helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
	helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
	helm repo add jaegertracing https://jaegertracing.github.io/helm-charts
	helm repo add grafana https://grafana.github.io/helm-charts
	helm repo update
	@echo "✅ All Helm repositories configured!"

grafana-ui:
	@echo "==> Opening Grafana UI..."
	kubectl port-forward svc/observability-grafana 3000:80 -n observability &
	@echo "Grafana available at http://localhost:3000"
	@echo "Username: admin"
	@echo "Password: admin"

jaegar-ui:
	@echo "==> Opening Jaegar UI..."
	kubectl port-forward -n observability svc/jaeger 16686:16686 &
	@echo "Jaegar UI availble at http://localhost:16686"
prometheus-ui:
	kubectl port-forward svc/observability-kube-prometh-prometheus 9090:9090 -n observability &
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

stop:
	minikube delete

preload-images:
	@echo "==> Pre-loading container images into Minikube..."
	@echo "    Atomix images..."
	@eval $$(minikube docker-env) && docker images atomix/consensus-controller:v0.7.1 | grep -q v0.7.1 && minikube image load atomix/consensus-controller:v0.7.1 || echo "    ⚠️  atomix/consensus-controller:v0.7.1 not found in Docker"
	@eval $$(minikube docker-env) && docker images atomix/runtime-controller-init:v0.8.0 | grep -q v0.8.0 && minikube image load atomix/runtime-controller-init:v0.8.0 || echo "    ⚠️  atomix/runtime-controller-init:v0.8.0 not found in Docker"
	@eval $$(minikube docker-env) && docker images atomix/runtime-controller:v0.8.0 | grep -q v0.8.0 && minikube image load atomix/runtime-controller:v0.8.0 || echo "    ⚠️  atomix/runtime-controller:v0.8.0 not found in Docker"
	@eval $$(minikube docker-env) && docker images atomix/pod-memory-controller:v0.1.0 | grep -q v0.1.0 && minikube image load atomix/pod-memory-controller:v0.1.0 || echo "    ⚠️  atomix/pod-memory-controller:v0.1.0 not found in Docker"
	@eval $$(minikube docker-env) && docker images atomix/shared-memory-controller:v0.1.0 | grep -q v0.1.0 && minikube image load atomix/shared-memory-controller:v0.1.0 || echo "    ⚠️  atomix/shared-memory-controller:v0.1.0 not found in Docker"
	@echo "    Kafka/Strimzi images..."
	@eval $$(minikube docker-env) && docker images quay.io/strimzi/operator:1.0.0 | grep -q 1.0.0 && minikube image load quay.io/strimzi/operator:1.0.0 || echo "    ⚠️  quay.io/strimzi/operator:1.0.0 not found in Docker"
	@eval $$(minikube docker-env) && docker images quay.io/strimzi/kafka:1.0.0-kafka-4.1.1 | grep -q 1.0.0-kafka-4.1.1 && minikube image load quay.io/strimzi/kafka:1.0.0-kafka-4.1.1 || echo "    ⚠️  quay.io/strimzi/kafka:1.0.0-kafka-4.1.1 not found in Docker"
	@echo "    Prometheus stack images..."
	@eval $$(minikube docker-env) && docker images docker.io/grafana/grafana:13.0.1 | grep -q 13.0.1 && minikube image load docker.io/grafana/grafana:13.0.1 || echo "    ⚠️  docker.io/grafana/grafana:13.0.1 not found in Docker"
	@eval $$(minikube docker-env) && docker images ghcr.io/jkroepke/kube-webhook-certgen:1.8.2 | grep -q 1.8.2 && minikube image load ghcr.io/jkroepke/kube-webhook-certgen:1.8.2 || echo "    ⚠️  ghcr.io/jkroepke/kube-webhook-certgen:1.8.2 not found in Docker"
	@eval $$(minikube docker-env) && docker images quay.io/kiwigrid/k8s-sidecar:2.7.1 | grep -q 2.7.1 && minikube image load quay.io/kiwigrid/k8s-sidecar:2.7.1 || echo "    ⚠️  quay.io/kiwigrid/k8s-sidecar:2.7.1 not found in Docker"
	@eval $$(minikube docker-env) && docker images quay.io/prometheus-operator/prometheus-operator:v0.90.1 | grep -q v0.90.1 && minikube image load quay.io/prometheus-operator/prometheus-operator:v0.90.1 || echo "    ⚠️  quay.io/prometheus-operator/prometheus-operator:v0.90.1 not found in Docker"
	@eval $$(minikube docker-env) && docker images quay.io/prometheus/alertmanager:v0.32.1 | grep -q v0.32.1 && minikube image load quay.io/prometheus/alertmanager:v0.32.1 || echo "    ⚠️  quay.io/prometheus/alertmanager:v0.32.1 not found in Docker"
	@eval $$(minikube docker-env) && docker images quay.io/prometheus/node-exporter:v1.11.1 | grep -q v1.11.1 && minikube image load quay.io/prometheus/node-exporter:v1.11.1 || echo "    ⚠️  quay.io/prometheus/node-exporter:v1.11.1 not found in Docker"
	@eval $$(minikube docker-env) && docker images quay.io/prometheus/prometheus:v3.11.3 | grep -q v3.11.3 && minikube image load quay.io/prometheus/prometheus:v3.11.3 || echo "    ⚠️  quay.io/prometheus/prometheus:v3.11.3 not found in Docker"
	@eval $$(minikube docker-env) && docker images registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0 | grep -q v2.18.0 && minikube image load registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0 || echo "    ⚠️  registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.18.0 not found in Docker"
	@echo "    OpenTelemetry images..."
	@eval $$(minikube docker-env) && docker images otel/opentelemetry-collector-contrib:0.151.0 | grep -q 0.151.0 && minikube image load otel/opentelemetry-collector-contrib:0.151.0 || echo "    ⚠️  otel/opentelemetry-collector-contrib:0.151.0 not found in Docker"
	@echo "    Jaeger images..."
	@eval $$(minikube docker-env) && docker images jaegertracing/jaeger:2.17.0 | grep -q 2.17.0 && minikube image load jaegertracing/jaeger:2.17.0 || echo "    ⚠️  jaegertracing/jaeger:2.17.0 not found in Docker"
	@echo "    Loki images..."
	@eval $$(minikube docker-env) && docker images docker.io/grafana/loki:3.6.7 | grep -q 3.6.7 && minikube image load docker.io/grafana/loki:3.6.7 || echo "    ⚠️  docker.io/grafana/loki:3.6.7 not found in Docker"
	@eval $$(minikube docker-env) && docker images docker.io/kiwigrid/k8s-sidecar:2.5.0 | grep -q 2.5.0 && minikube image load docker.io/kiwigrid/k8s-sidecar:2.5.0 || echo "    ⚠️  docker.io/kiwigrid/k8s-sidecar:2.5.0 not found in Docker"
	@echo "✅ Preload complete (images that don't exist locally were skipped)"

proto:
	cd shared/proto && mkdir -p pb && protoc -I . --go_out=pb --go_opt=paths=source_relative *.proto

test:
	@echo "==> Running unit tests..."
	cd api && go test ./... && cd ..
	cd agent && go test ./... && cd ..
	cd scripts/gen-agent-deployments && go test ./... && cd ../..