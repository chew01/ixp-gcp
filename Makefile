VENDOR_MODULES = api auction dummy telemetry agent dashboard

# Container registry — leave unset for Minikube (images built directly into daemon).
# Set for cloud (DOKS): export DOCKER_REGISTRY=registry.digitalocean.com/ixp-registry
DOCKER_REGISTRY ?=
IMAGE_TAG       ?= latest
image_ref = $(if $(DOCKER_REGISTRY),$(DOCKER_REGISTRY)/$(1):$(IMAGE_TAG),$(1):local)

# Kafka — defaults to in-cluster Strimzi. Override for external brokers.
KAFKA_BOOTSTRAP     ?= ixp-kafka-kafka-bootstrap:9092
KAFKA_TLS_CA_FILE   ?=
KAFKA_TLS_CERT_FILE ?=
KAFKA_TLS_KEY_FILE  ?=

# Observability / measurement
PROMETHEUS_URL ?= http://localhost:9090
API_URL        ?= http://localhost:8080
CONCURRENCY    ?= 2
SINCE          ?= 3600

# Scenario loaded on `make infra` and `make load-scenario`
SCENARIO ?= etc/scenario/scenario.yaml

# ============================================================
# Setup
# ============================================================
.PHONY: setup vendor

setup:
	helm repo add atomix https://atomix.github.io/charts.atomix.io
	helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
	helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
	helm repo update

vendor:
	@for mod in $(VENDOR_MODULES); do \
		if [ -d "$$mod/vendor" ]; then \
			echo "==> $$mod vendor already present — skipping."; \
		else \
			echo "==> Vendoring $$mod..."; \
			(cd $$mod && go mod vendor); \
		fi; \
	done

# ============================================================
# Local (Minikube) cluster bootstrap
# ============================================================
.PHONY: deploy-minikube

deploy-minikube:
	minikube start --cpus=4 --memory=8192
	minikube addons enable ingress

# ============================================================
# Infrastructure
# ============================================================
.PHONY: deploy-atomix deploy-kafka deploy-monitoring infra

deploy-atomix:
	@if helm status atomix-runtime -n kube-system >/dev/null 2>&1; then \
		echo "Atomix already installed — skipping."; \
	else \
		helm install -n kube-system atomix-runtime atomix/atomix-runtime --wait; \
	fi
	kubectl apply -f atomix/storage-profile.yaml
	kubectl apply -f atomix/store.yaml
	@until kubectl get statefulset consensus-store >/dev/null 2>&1; do sleep 2; done
	@if kubectl rollout status statefulset/consensus-store --timeout=10s >/dev/null 2>&1; then \
		echo "consensus-store already healthy — skipping patch."; \
	else \
		echo "==> Patching StatefulSet (fix DO block storage permissions)..."; \
		kubectl patch statefulset consensus-store --type='json' \
			-p='[{"op":"add","path":"/spec/template/spec/initContainers","value":[{"name":"fix-permissions","image":"busybox","command":["sh","-c","chown -R 1000:1000 /var/lib/atomix"],"volumeMounts":[{"name":"data","mountPath":"/var/lib/atomix"}]}]}]'; \
		kubectl delete pod -l atomix.io/store=consensus-store --ignore-not-found=true; \
		kubectl rollout status statefulset/consensus-store --timeout=300s; \
	fi

deploy-kafka:
ifndef KAFKA_EXTERNAL
	@if helm status strimzi-cluster-operator >/dev/null 2>&1; then \
		echo "Strimzi already installed — skipping."; \
	else \
		helm install strimzi-cluster-operator oci://quay.io/strimzi-helm/strimzi-kafka-operator; \
	fi
	kubectl apply -f kafka/kafka.yaml
	kubectl wait kafka/ixp-kafka --for=condition=Ready --timeout=300s
else
	@echo "Skipping Strimzi — using external Kafka at $(KAFKA_BOOTSTRAP)"
endif

deploy-monitoring:
	helm upgrade --install monitoring prometheus-community/kube-prometheus-stack \
		--namespace monitoring --create-namespace \
		-f monitoring/values.yaml
	kubectl create configmap ixp-flows-dashboard \
		--from-file=ixp-flows.json=monitoring/ixp-flows.json \
		-n monitoring -o yaml --dry-run=client | kubectl apply -f -
	kubectl label configmap ixp-flows-dashboard -n monitoring grafana_dashboard="1" --overwrite

infra: deploy-atomix deploy-monitoring
	$(MAKE) deploy-kafka $(if $(KAFKA_EXTERNAL),KAFKA_EXTERNAL=$(KAFKA_EXTERNAL),) KAFKA_BOOTSTRAP=$(KAFKA_BOOTSTRAP)
	kubectl create configmap test-scenario \
		--from-file=scenario.yaml=$(SCENARIO) \
		-o yaml --dry-run=client | kubectl apply -f -

# ============================================================
# Core services
# ============================================================
.PHONY: deploy-api deploy-auction deploy-telemetry deploy-dummy deploy-agent services

deploy-api:
	@echo "==> Deploying API Gateway..."
	$(if $(DOCKER_REGISTRY),\
		docker build --platform linux/amd64 -t $(call image_ref,api-gateway) ./api && docker push $(call image_ref,api-gateway),\
		eval $$(minikube docker-env) && docker build -t api-gateway:local ./api)
	kubectl apply -f api/ingress.yaml -f api/deployment.yaml
	kubectl set image deployment/api-gateway api-gateway=$(call image_ref,api-gateway)
	kubectl apply -f api/service-monitor.yaml

deploy-auction:
	@echo "==> Deploying Auction Runner..."
	$(if $(DOCKER_REGISTRY),\
		docker build --platform linux/amd64 -t $(call image_ref,auction-runner) ./auction && docker push $(call image_ref,auction-runner),\
		eval $$(minikube docker-env) && docker build -t auction-runner:local ./auction)
	kubectl apply -f auction/deployment.yaml
	kubectl set image deployment/auction-runner auction-runner=$(call image_ref,auction-runner)
	kubectl set env deployment/auction-runner KAFKA_BOOTSTRAP=$(KAFKA_BOOTSTRAP)
	$(if $(KAFKA_TLS_CA_FILE),kubectl set env deployment/auction-runner \
		KAFKA_TLS_CA_FILE=$(KAFKA_TLS_CA_FILE) KAFKA_TLS_CERT_FILE=$(KAFKA_TLS_CERT_FILE) KAFKA_TLS_KEY_FILE=$(KAFKA_TLS_KEY_FILE),true)

deploy-telemetry:
	@echo "==> Deploying Telemetry Processor..."
	$(if $(DOCKER_REGISTRY),\
		docker build --platform linux/amd64 -t $(call image_ref,telemetry-service) ./telemetry && docker push $(call image_ref,telemetry-service),\
		eval $$(minikube docker-env) && docker build -t telemetry-service:local ./telemetry)
	kubectl apply -f telemetry/deployment.yaml
	kubectl set image deployment/telemetry-service telemetry-service=$(call image_ref,telemetry-service)
	kubectl set env deployment/telemetry-service KAFKA_BOOTSTRAP=$(KAFKA_BOOTSTRAP)
	$(if $(KAFKA_TLS_CA_FILE),kubectl set env deployment/telemetry-service \
		KAFKA_TLS_CA_FILE=$(KAFKA_TLS_CA_FILE) KAFKA_TLS_CERT_FILE=$(KAFKA_TLS_CERT_FILE) KAFKA_TLS_KEY_FILE=$(KAFKA_TLS_KEY_FILE),true)

deploy-dummy:
	@echo "==> Deploying Dummy Producer..."
	$(if $(DOCKER_REGISTRY),\
		docker build --platform linux/amd64 -t $(call image_ref,dummy-producer) ./dummy && docker push $(call image_ref,dummy-producer),\
		eval $$(minikube docker-env) && docker build -t dummy-producer:local ./dummy)
	kubectl apply -f dummy/deployment.yaml
	kubectl set image deployment/dummy-producer dummy-producer=$(call image_ref,dummy-producer)
	kubectl set env deployment/dummy-producer KAFKA_BOOTSTRAP=$(KAFKA_BOOTSTRAP)
	@[ -z "$(KAFKA_TLS_CA_FILE)" ] || kubectl set env deployment/dummy-producer \
		KAFKA_TLS_CA_FILE=$(KAFKA_TLS_CA_FILE) KAFKA_TLS_CERT_FILE=$(KAFKA_TLS_CERT_FILE) KAFKA_TLS_KEY_FILE=$(KAFKA_TLS_KEY_FILE)

deploy-agent:
	@echo "==> Deploying Customer Agent (single-agent manual testing)..."
	$(if $(DOCKER_REGISTRY),\
		docker build --platform linux/amd64 -t $(call image_ref,customer-agent) ./agent && docker push $(call image_ref,customer-agent),\
		eval $$(minikube docker-env) && docker build -t customer-agent:local ./agent)
	kubectl apply -f agent/deployment.yaml
	kubectl set image deployment/customer-agent customer-agent=$(call image_ref,customer-agent)

services: vendor deploy-api deploy-auction deploy-telemetry

# ============================================================
# Experiments
# ============================================================
# Experiment index:
#   1              Baseline (conservative × 2); 2-bidder latency baseline
#   2a / 2b        Conservative vs conservative / demand_corrected, spike traffic
#   3              Conservative vs price_insensitive, heterogeneous
#   4a / 4b / 4c   Budget-awareness variants
#   5              Auction convergence (valuation_based × 2)
#   6a / 6b / 6c   Sensitivity: 10s / 30s / 60s auction interval
#   7 / 7b         Dominant-strategy validation / Q-learning convergence
#   8              Mixed valuations
#   9              EMA negative result
#   perf-5bidders / perf-10bidders   Pipeline latency (Section 5.2.2)

.PHONY: all load-experiment load-scenario delete-agents deploy-real

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
	@test -f "$(EXPERIMENT_SCENARIO)" || (echo "Unknown experiment: $(experiment)"; exit 1)
	@echo "==> Loading experiment: $(EXPERIMENT_SCENARIO)"
	kubectl create configmap test-scenario \
		--from-file=scenario.yaml=$(EXPERIMENT_SCENARIO) \
		-o yaml --dry-run=client | kubectl apply -f -
	$(MAKE) deploy-dummy KAFKA_BOOTSTRAP=$(KAFKA_BOOTSTRAP) \
		$(if $(DOCKER_REGISTRY),DOCKER_REGISTRY=$(DOCKER_REGISTRY) IMAGE_TAG=$(IMAGE_TAG),)
	@echo "==> Building customer-agent image..."
	$(if $(DOCKER_REGISTRY),\
		docker build --platform linux/amd64 -t $(call image_ref,customer-agent) ./agent && docker push $(call image_ref,customer-agent),\
		eval $$(minikube docker-env) && docker build -t customer-agent:local ./agent)
	kubectl rollout restart deployment/auction-runner deployment/telemetry-service deployment/api-gateway
	kubectl rollout status deployment/auction-runner --timeout=90s
	kubectl rollout status deployment/api-gateway --timeout=90s
	$(MAKE) delete-agents
	cd scripts/gen-agent-deployments && go run . \
		$(if $(DOCKER_REGISTRY),--image $(call image_ref,customer-agent),) \
		../../$(EXPERIMENT_SCENARIO) | kubectl apply -f -
	@echo "==> Experiment $(experiment) live."

load-scenario:
	@echo "==> Loading scenario: $(SCENARIO)"
	kubectl create configmap test-scenario \
		--from-file=scenario.yaml=$(SCENARIO) \
		-o yaml --dry-run=client | kubectl apply -f -
	kubectl rollout restart deployment/auction-runner deployment/telemetry-service deployment/api-gateway
	kubectl rollout status deployment/auction-runner --timeout=90s
	kubectl rollout status deployment/api-gateway --timeout=90s
	@echo "==> Scenario active."

# Load etc/scenario/scenario.yaml against a real switch (no dummy producer).
deploy-real:
	$(MAKE) load-scenario SCENARIO=etc/scenario/scenario.yaml
	$(if $(DOCKER_REGISTRY),\
		docker build --platform linux/amd64 -t $(call image_ref,customer-agent) ./agent && docker push $(call image_ref,customer-agent),\
		eval $$(minikube docker-env) && docker build -t customer-agent:local ./agent)
	$(MAKE) delete-agents
	cd scripts/gen-agent-deployments && go run . \
		$(if $(DOCKER_REGISTRY),--image $(call image_ref,customer-agent),) \
		../../etc/scenario/scenario.yaml | kubectl apply -f -
	@echo "==> Real-switch scenario live."

# ============================================================
# Dashboard
# ============================================================
.PHONY: deploy-dashboard dashboard-ui

deploy-dashboard:
	@echo "==> Deploying Dashboard..."
	cd dashboard && go mod vendor
	$(if $(DOCKER_REGISTRY),\
		docker build --platform linux/amd64 --build-arg VITE_BASE_PATH=/dashboard/ -t $(call image_ref,dashboard) ./dashboard && docker push $(call image_ref,dashboard),\
		eval $$(minikube docker-env) && docker build -t dashboard:local ./dashboard)
	kubectl apply -f dashboard/rbac.yaml
	kubectl apply -f dashboard/deployment.yaml
	kubectl apply -f dashboard/ingress.yaml
	kubectl set image deployment/dashboard dashboard=$(call image_ref,dashboard)
	kubectl set env deployment/dashboard KAFKA_BOOTSTRAP=$(KAFKA_BOOTSTRAP)
	@[ -z "$(KAFKA_TLS_CA_FILE)" ] || kubectl set env deployment/dashboard \
		KAFKA_TLS_CA_FILE=$(KAFKA_TLS_CA_FILE) KAFKA_TLS_CERT_FILE=$(KAFKA_TLS_CERT_FILE) KAFKA_TLS_KEY_FILE=$(KAFKA_TLS_KEY_FILE)

# Port-forward the dashboard for local access (Minikube or cluster without ingress).
dashboard-ui:
	kubectl port-forward svc/dashboard 8082:8082 &
	@echo "Dashboard at http://localhost:8082"

# ============================================================
# Observability
# ============================================================
.PHONY: grafana-ui prometheus-ui export-metrics logs

grafana-ui:
	kubectl port-forward svc/monitoring-grafana 3000:80 -n monitoring &
	@echo "Grafana at http://localhost:3000"
	@echo "Password: $$(kubectl get secret monitoring-grafana -n monitoring \
		-o jsonpath='{.data.admin-password}' | base64 --decode)"

prometheus-ui:
	kubectl port-forward svc/monitoring-kube-prometheus-prometheus 9090:9090 -n monitoring &
	@echo "Prometheus at http://localhost:9090"

# Export key experiment metrics from Prometheus.
# Usage: make export-metrics [SINCE=3600] [PROMETHEUS_URL=http://...]
# SINCE is in seconds. Output: data/experiment-<timestamp>.json
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

logs:
	kubectl logs -l app=$(SERVICE) -f

# ============================================================
# Measurement (Section 5 evaluation)
# ============================================================
.PHONY: load-test-api measure-kafka-lag measure-kafka-lag-series \
        measure-pipeline-latency measure-e2e-latency

# 5.2.1 — API throughput and latency
# Requires: hey (go install github.com/rakyll/hey@latest)
# Local: port-forward api-gateway first (kubectl port-forward svc/api-gateway 8080:8080)
# Cloud: export API_URL=http://<LB-IP>
load-test-api:
	@command -v hey >/dev/null 2>&1 || \
	  (echo "ERROR: 'hey' not found — install: go install github.com/rakyll/hey@latest"; exit 1)
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

# 5.2.3 — Kafka consumer lag (point-in-time snapshot)
KAFKA_POD ?= $(shell kubectl get pods -l strimzi.io/name=ixp-kafka-kafka -o name 2>/dev/null | head -1 | sed 's|pod/||')
measure-kafka-lag:
	@test -n "$(KAFKA_POD)" || (echo "ERROR: no Kafka pod found — is Strimzi running?"; exit 1)
	kubectl exec -it $(KAFKA_POD) -- \
	  bin/kafka-consumer-groups.sh \
	    --bootstrap-server localhost:9092 \
	    --describe --all-groups

# 5.2.3 — Kafka consumer lag time series
# Usage: make measure-kafka-lag-series [LAG_COUNT=30] [LAG_INTERVAL=10]
LAG_COUNT    ?= 30
LAG_INTERVAL ?= 10
measure-kafka-lag-series:
	@test -n "$(KAFKA_POD)" || (echo "ERROR: no Kafka pod found — is Strimzi running?"; exit 1)
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
# Run after ≥30 intervals. Grep for timing markers in auction-runner logs.
measure-pipeline-latency:
	kubectl logs deployment/auction-runner \
	  | grep -E '\[(bids-collected|cleared|published-to-kafka)\]'

# 5.2.4 — Control loop end-to-end latency
# Run after a spike experiment with prometheus-ui active.
measure-e2e-latency:
	@echo "==> Spike timestamp from dummy-producer:"
	kubectl logs deployment/dummy-producer | grep -iE 'spike|traffic.*increased|spike_after'
	@echo ""
	@echo "==> Allocation time series from Prometheus (step=5s):"
	curl -sG "$(PROMETHEUS_URL)/api/v1/query_range" \
		--data-urlencode "query=ixp_customer_allocation_kbps" \
		--data-urlencode "start=$(PROM_START)" \
		--data-urlencode "end=$$(date +%s)" \
		--data-urlencode "step=5s" | python3 -m json.tool

# ============================================================
# Misc
# ============================================================
.PHONY: test proto stop

test:
	cd api && go test ./...
	cd agent && go test ./...
	cd scripts/gen-agent-deployments && go test ./...

proto:
	cd shared/proto && mkdir -p pb && protoc -I . --go_out=pb --go_opt=paths=source_relative *.proto

stop:
	minikube delete
