VENDOR_MODULES = api auction dummy telemetry agent dashboard

# ============================================================
# Container registry (cloud deployment)
#
# Leave DOCKER_REGISTRY unset (default) to build images directly into the
# Minikube daemon — the original local workflow is unchanged.
#
# Set DOCKER_REGISTRY to push images to a remote registry instead:
#   export DOCKER_REGISTRY=registry.digitalocean.com/my-registry
#   export IMAGE_TAG=v1.0.0
#   make services DOCKER_REGISTRY=$DOCKER_REGISTRY IMAGE_TAG=$IMAGE_TAG
# ============================================================
DOCKER_REGISTRY ?=
IMAGE_TAG       ?= latest

# image_ref(name) — returns the full image reference.
#   With DOCKER_REGISTRY: registry.digitalocean.com/my-registry/name:v1.0.0
#   Without:              name:local  (Minikube local image)
image_ref = $(if $(DOCKER_REGISTRY),$(DOCKER_REGISTRY)/$(1):$(IMAGE_TAG),$(1):local)

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
	$(if $(DOCKER_REGISTRY),\
		docker build --platform linux/amd64 -t $(call image_ref,api-gateway) ./api && docker push $(call image_ref,api-gateway),\
		eval $$(minikube docker-env) && docker build -t api-gateway:local ./api)
	kubectl apply -f ./api/ingress.yaml
	kubectl apply -f ./api/deployment.yaml
	kubectl set image deployment/api-gateway api-gateway=$(call image_ref,api-gateway)
	kubectl apply -f ./api/service-monitor.yaml

deploy-atomix:
	@echo "==> Deploying Atomix..."
	@if helm status atomix-runtime -n kube-system >/dev/null 2>&1; then \
		echo "    Atomix Helm release already installed — skipping."; \
	else \
		helm install -n kube-system atomix-runtime atomix/atomix-runtime --wait; \
	fi
	kubectl apply -f ./atomix/storage-profile.yaml
	kubectl apply -f ./atomix/store.yaml
	@echo "==> Patching StatefulSet with init container (DO block-storage ignores fsGroup)..."
	kubectl rollout status statefulset/consensus-store --timeout=30s || true
	kubectl patch statefulset consensus-store --type='json' \
		-p='[{"op":"add","path":"/spec/template/spec/initContainers","value":[{"name":"fix-permissions","image":"busybox","command":["sh","-c","chown -R 1000:1000 /var/lib/atomix"],"volumeMounts":[{"name":"data","mountPath":"/var/lib/atomix"}]}]}]'

deploy-auction:
	@echo "==> Deploying Auction Runner..."
	$(if $(DOCKER_REGISTRY),\
		docker build --platform linux/amd64 -t $(call image_ref,auction-runner) ./auction && docker push $(call image_ref,auction-runner),\
		eval $$(minikube docker-env) && docker build -t auction-runner:local ./auction)
	kubectl apply -f ./auction/deployment.yaml
	kubectl set image deployment/auction-runner auction-runner=$(call image_ref,auction-runner)
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
	$(if $(DOCKER_REGISTRY),\
		docker build --platform linux/amd64 -t $(call image_ref,dummy-producer) ./dummy && docker push $(call image_ref,dummy-producer),\
		eval $$(minikube docker-env) && docker build -t dummy-producer:local ./dummy)
	kubectl apply -f ./dummy/deployment.yaml
	kubectl set image deployment/dummy-producer dummy-producer=$(call image_ref,dummy-producer)
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
	@if helm status strimzi-cluster-operator >/dev/null 2>&1; then \
		echo "    Strimzi Helm release already installed — skipping."; \
	else \
		helm install strimzi-cluster-operator oci://quay.io/strimzi-helm/strimzi-kafka-operator; \
	fi
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
	$(if $(DOCKER_REGISTRY),\
		docker build --platform linux/amd64 -t $(call image_ref,telemetry-service) ./telemetry && docker push $(call image_ref,telemetry-service),\
		eval $$(minikube docker-env) && docker build -t telemetry-service:local ./telemetry)
	kubectl apply -f ./telemetry/deployment.yaml
	kubectl set image deployment/telemetry-service telemetry-service=$(call image_ref,telemetry-service)
	kubectl set env deployment/telemetry-service KAFKA_BOOTSTRAP=$(KAFKA_BOOTSTRAP)
ifdef KAFKA_TLS_CA_FILE
	kubectl set env deployment/telemetry-service \
		KAFKA_TLS_CA_FILE=$(KAFKA_TLS_CA_FILE) \
		KAFKA_TLS_CERT_FILE=$(KAFKA_TLS_CERT_FILE) \
		KAFKA_TLS_KEY_FILE=$(KAFKA_TLS_KEY_FILE)
endif

deploy-agent:
	@echo "==> Deploying Customer Agent (manual single-agent testing only)..."
	$(if $(DOCKER_REGISTRY),\
		docker build --platform linux/amd64 -t $(call image_ref,customer-agent) ./agent && docker push $(call image_ref,customer-agent),\
		eval $$(minikube docker-env) && docker build -t customer-agent:local ./agent)
	kubectl apply -f ./agent/deployment.yaml
	kubectl set image deployment/customer-agent customer-agent=$(call image_ref,customer-agent)

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
	$(MAKE) deploy-dummy KAFKA_BOOTSTRAP=$(KAFKA_BOOTSTRAP) $(if $(DOCKER_REGISTRY),DOCKER_REGISTRY=$(DOCKER_REGISTRY) IMAGE_TAG=$(IMAGE_TAG),)
	@echo "==> Building customer-agent image..."
	$(if $(DOCKER_REGISTRY),\
		docker build --platform linux/amd64 -t $(call image_ref,customer-agent) ./agent && docker push $(call image_ref,customer-agent),\
		eval $$(minikube docker-env) && docker build -t customer-agent:local ./agent)
	kubectl rollout restart deployment/auction-runner
	kubectl rollout restart deployment/telemetry-service
	kubectl rollout restart deployment/api-gateway
	kubectl rollout status deployment/auction-runner --timeout=90s
	kubectl rollout status deployment/api-gateway --timeout=90s
	$(MAKE) delete-agents
	cd scripts/gen-agent-deployments && go run . \
		$(if $(DOCKER_REGISTRY),--image $(call image_ref,customer-agent),) \
		../../$(EXPERIMENT_SCENARIO) | kubectl apply -f -
	@echo "==> Experiment $(experiment) is live."

# ============================================================
# Experiments — swap scenario, restart pods, redeploy agents
# ============================================================
# Preferred: make all experiment=2a
# Legacy aliases kept for backward compatibility.
#
# Experiment index:
#   1          — Baseline agent correctness (conservative × 2); also 2-bidder pipeline-latency baseline
#   2a         — Conservative vs conservative, spike traffic
#   2b         — Conservative vs demand_corrected, spike traffic
#   3          — Conservative vs price_insensitive, heterogeneous
#   4a         — Conservative with finite budget
#   4b         — Budget-aware with finite budget
#   4c         — Throughput optimizer vs conservative
#   5          — Auction convergence and stability (valuation_based × 2, Section 5.3.2)
#   6a         — Sensitivity: 10s interval
#   6b         — Sensitivity: 30s interval
#   6c         — Sensitivity: 60s interval
#   perf-5bidders  — Pipeline latency measurement: 5 bidders (Section 5.2.2)
#   perf-10bidders — Pipeline latency measurement: 10 bidders (Section 5.2.2)
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
        deploy-experiment-9 \
        deploy-experiment-perf-5bidders deploy-experiment-perf-10bidders

# deploy-real: load etc/scenario/scenario.yaml against a real switch.
# Pushes the config, restarts core services, and deploys one agent pod per
# customer. No dummy producer is started.
deploy-real:
	@echo "==> Loading real-switch scenario..."
	$(MAKE) load-scenario
	@echo "==> Building customer-agent image..."
	$(if $(DOCKER_REGISTRY),\
		docker build --platform linux/amd64 -t $(call image_ref,customer-agent) ./agent && docker push $(call image_ref,customer-agent),\
		eval $$(minikube docker-env) && docker build -t customer-agent:local ./agent)
	$(MAKE) delete-agents
	cd scripts/gen-agent-deployments && go run . \
		$(if $(DOCKER_REGISTRY),--image $(call image_ref,customer-agent),) \
		../../etc/scenario/scenario.yaml | kubectl apply -f -
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

# Performance experiments — auction pipeline latency (Section 5.2.2)
deploy-experiment-perf-5bidders:
	$(MAKE) load-experiment experiment=perf-5bidders

deploy-experiment-perf-10bidders:
	$(MAKE) load-experiment experiment=perf-10bidders

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
# Usage: make export-metrics [PROMETHEUS_URL=http://...] [SINCE=1800]
# SINCE is in seconds (default 3600 = 1 hour). Examples: 1800 (30m), 7200 (2h).
# Output: data/experiment-<timestamp>.json
SINCE ?= 3600
PROM_START = $$(( $$(date +%s) - $(SINCE) ))
export-metrics:
	@mkdir -p data
	@TS=$$(date +%Y%m%d-%H%M%S); FILE=data/experiment-$$TS.json; \
	printf '{"clearing_price":' > $$FILE; \
	curl -sG "$(PROMETHEUS_URL)/api/v1/query_range" \
		--data-urlencode "query=ixp_auction_clearing_price" \
		--data-urlencode "start=$(PROM_START)" \
		--data-urlencode "end=$$(date +%s)" \
		--data-urlencode "step=30s" >> $$FILE; \
	printf ',"allocation_kbps":' >> $$FILE; \
	curl -sG "$(PROMETHEUS_URL)/api/v1/query_range" \
		--data-urlencode "query=ixp_customer_allocation_kbps" \
		--data-urlencode "start=$(PROM_START)" \
		--data-urlencode "end=$$(date +%s)" \
		--data-urlencode "step=30s" >> $$FILE; \
	printf ',"flow_drop_rate":' >> $$FILE; \
	curl -sG "$(PROMETHEUS_URL)/api/v1/query_range" \
		--data-urlencode "query=ixp_flow_drop_rate_percent" \
		--data-urlencode "start=$(PROM_START)" \
		--data-urlencode "end=$$(date +%s)" \
		--data-urlencode "step=30s" >> $$FILE; \
	printf ',"flow_throughput":' >> $$FILE; \
	curl -sG "$(PROMETHEUS_URL)/api/v1/query_range" \
		--data-urlencode "query=ixp_flow_throughput_kbps" \
		--data-urlencode "start=$(PROM_START)" \
		--data-urlencode "end=$$(date +%s)" \
		--data-urlencode "step=30s" >> $$FILE; \
	printf ',"utility_per_round":' >> $$FILE; \
	curl -sG "$(PROMETHEUS_URL)/api/v1/query_range" \
		--data-urlencode "query=increase(ixp_agent_utility_total[30s])" \
		--data-urlencode "start=$(PROM_START)" \
		--data-urlencode "end=$$(date +%s)" \
		--data-urlencode "step=30s" >> $$FILE; \
	printf ',"cumulative_utility":' >> $$FILE; \
	curl -sG "$(PROMETHEUS_URL)/api/v1/query_range" \
		--data-urlencode "query=ixp_agent_utility_total" \
		--data-urlencode "start=$(PROM_START)" \
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
	$(if $(DOCKER_REGISTRY),\
		docker build --platform linux/amd64 -t $(call image_ref,dashboard) ./dashboard && docker push $(call image_ref,dashboard),\
		eval $$(minikube docker-env) && docker build -t dashboard:local ./dashboard)

deploy-dashboard:
	@echo "==> Deploying Dashboard..."
	kubectl apply -f dashboard/rbac.yaml
	cd dashboard && go mod vendor
	$(if $(DOCKER_REGISTRY),\
		docker build --platform linux/amd64 -t $(call image_ref,dashboard) ./dashboard && docker push $(call image_ref,dashboard),\
		eval $$(minikube docker-env) && docker build -t dashboard:local ./dashboard)
	kubectl apply -f dashboard/deployment.yaml
	kubectl set image deployment/dashboard dashboard=$(call image_ref,dashboard)
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

# ============================================================
# Measurement targets for Section 5 evaluation
# ============================================================
.PHONY: load-test-api measure-kafka-lag measure-pipeline-latency measure-e2e-latency

# 5.2.1 — API throughput and latency
# Requires: hey  (go install github.com/rakyll/hey@latest)
# Requires: API gateway reachable at API_URL.
#   Local:  make prometheus-ui first, then use default API_URL=http://localhost:8080
#   Cloud:  export API_URL=http://<DO-loadbalancer-IP>
#
# Usage examples:
#   make load-test-api                            # 2 concurrent clients (default)
#   make load-test-api CONCURRENCY=5 API_URL=http://...
#   make load-test-api CONCURRENCY=10
#   make load-test-api CONCURRENCY=20
API_URL     ?= http://localhost:8080
CONCURRENCY ?= 2
load-test-api:
	@command -v hey >/dev/null 2>&1 || \
	  (echo "ERROR: 'hey' not found. Install with: go install github.com/rakyll/hey@latest"; exit 1)
	@echo "==> POST /bids  concurrency=$(CONCURRENCY)  60s"
	hey -c $(CONCURRENCY) -z 60s -m POST \
		-H "X-Customer-ID: as12345" \
		-H "Content-Type: application/json" \
		-d '{"ingress_port":1,"egress_port":0,"units":50,"unit_price":60}' \
		$(API_URL)/bids
	@echo "==> GET /flows  concurrency=$(CONCURRENCY)  60s"
	hey -c $(CONCURRENCY) -z 60s \
		-H "X-Customer-ID: as12345" \
		"$(API_URL)/flows?switch_id=sw-1&ingress_port=1&egress_port=0"

# 5.2.3 — Kafka consumer lag
# Describes all consumer groups (telemetry-consumer, auction-consumer).
# Run while an experiment is active to capture steady-state lag.
KAFKA_POD ?= $(shell kubectl get pods -l strimzi.io/name=ixp-kafka-kafka -o name 2>/dev/null | head -1 | sed 's|pod/||')
measure-kafka-lag:
	@test -n "$(KAFKA_POD)" || (echo "ERROR: no Kafka pod found (is Strimzi running?)"; exit 1)
	kubectl exec -it $(KAFKA_POD) -- \
	  bin/kafka-consumer-groups.sh \
	    --bootstrap-server localhost:9092 \
	    --describe --all-groups

# 5.2.3 — Kafka consumer lag time series
# Samples consumer-group lag every INTERVAL seconds for COUNT iterations.
# Run while an experiment is active; output goes to data/kafka-lag.txt.
# Usage:  make measure-kafka-lag-series              (30 samples, 10s apart)
#         make measure-kafka-lag-series COUNT=60 INTERVAL=5
LAG_COUNT    ?= 30
LAG_INTERVAL ?= 10
measure-kafka-lag-series:
	@test -n "$(KAFKA_POD)" || (echo "ERROR: no Kafka pod found (is Strimzi running?)"; exit 1)
	@for i in $$(seq 1 $(LAG_COUNT)); do \
	  ts=$$(date -u +%H:%M:%S); \
	  lag_line=$$(kubectl exec $(KAFKA_POD) -- \
	    bin/kafka-consumer-groups.sh \
	    --bootstrap-server localhost:9092 \
	    --describe --all-groups 2>/dev/null \
	    | grep -v "^$$" | grep -v "^GROUP"); \
	  current=$$(echo "$$lag_line" | awk '{print $$4}'); \
	  end=$$(echo "$$lag_line" | awk '{print $$5}'); \
	  lag=$$(echo "$$lag_line" | awk '{print $$6}'); \
	  echo "$$ts  current=$$current  end=$$end  lag=$$lag"; \
	  sleep $(LAG_INTERVAL); \
	done | tee data/kafka-lag.txt

# 5.2.2 — Auction pipeline latency
# Greps the auction-runner logs for the three timing markers added to runner.go.
# Run after an experiment has completed ≥30 intervals; pipe through a script or
# spreadsheet to compute per-round elapsed_ms mean and variance.
measure-pipeline-latency:
	kubectl logs deployment/auction-runner \
	  | grep -E '\[(bids-collected|cleared|published-to-kafka)\]'

# 5.2.4 — Control loop end-to-end latency
# Prints the dummy-producer's spike log lines alongside a Prometheus range query
# for ixp_customer_allocation_kbps so you can compare timestamps manually.
# Run after a spike experiment (e.g. experiment=7) with prometheus-ui active.
# Override SINCE to widen the look-back window (e.g. SINCE=2h).
measure-e2e-latency:
	@echo "==> Spike timestamp from dummy-producer logs:"
	kubectl logs deployment/dummy-producer | grep -iE 'spike|traffic.*increased|spike_after'
	@echo ""
	@echo "==> Allocation time series from Prometheus (step=5s):"
	curl -sG "$(PROMETHEUS_URL)/api/v1/query_range" \
		--data-urlencode "query=ixp_customer_allocation_kbps" \
		--data-urlencode "start=$(PROM_START)" \
		--data-urlencode "end=$$(date +%s)" \
		--data-urlencode "step=5s" | python3 -m json.tool

stop:
	minikube delete

proto:
	cd shared/proto && mkdir -p pb && protoc -I . --go_out=pb --go_opt=paths=source_relative *.proto

test:
	@echo "==> Running unit tests..."
	cd api && go test ./... && cd ..
	cd agent && go test ./... && cd ..
	cd scripts/gen-agent-deployments && go test ./... && cd ../..