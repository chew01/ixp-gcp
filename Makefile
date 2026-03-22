VENDOR_MODULES = api auction dummy telemetry agent

# ============================================================
# Individual deploys
# ============================================================
.PHONY: deploy-minikube deploy-kafka deploy-atomix deploy-api \
		deploy-auction deploy-dummy deploy-telemetry deploy-agent deploy-observability

deploy-api:
	@echo "==> Deploying API Gateway..."
	docker build -t api-gateway:local ./api
	minikube image load api-gateway:local
	kubectl apply -f ./api/ingress.yaml
	kubectl apply -f ./api/deployment.yaml

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

deploy-config:
	@echo "==> Deploying Scenario Config..."
	kubectl create configmap test-scenario --from-file=scenario.yaml=./etc/scenario/scenario.yaml

deploy-dummy:
	@echo "==> Deploying Dummy Producer..."
	docker build -t dummy-producer:local ./dummy
	minikube image load dummy-producer:local
	kubectl apply -f ./dummy/deployment.yaml

deploy-kafka:
	@echo "==> Deploying Kafka..."
	helm install strimzi-cluster-operator oci://quay.io/strimzi-helm/strimzi-kafka-operator
	kubectl apply -f ./kafka/kafka.yaml
	kubectl wait kafka/ixp-kafka --for=condition=Ready --timeout=300s

deploy-minikube:
	@echo "==> Deploying Minikube..."
	minikube delete
	minikube start --memory=6144 --cpus=4
	minikube addons enable ingress

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
		--wait
	
	# Deploy OpenTelemetry Collector
	@echo "==> Installing OpenTelemetry Collector..."
	helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts || true
	helm repo update
	helm upgrade --install otel-collector open-telemetry/opentelemetry-collector \
		--namespace observability \
		-f helm/opentelemetry-collector/values.yaml \
		--wait
	
	# Deploy Jaeger (all-in-one for development)
	@echo "==> Installing Jaeger..."
	helm repo add jaegertracing https://jaegertracing.github.io/helm-charts || true
	helm repo update
	helm upgrade --install jaeger jaegertracing/jaeger \
		--namespace observability \
		--set allInOne.enabled=true \
		--set storage.type=memory \
		--set agent.enabled=false \
		--set collector.enabled=false \
		--set query.enabled=false \
		--wait
	
	# Deploy Loki (log aggregation)
	@echo "==> Installing Loki..."
	helm repo add grafana https://grafana.github.io/helm-charts || true
	helm repo update
	helm upgrade --install loki grafana/loki \
		--namespace observability \
		-f helm/loki/values.yaml \
		--wait
	
	# Apply IXP Flows dashboard
	@echo "==> Deploying IXP Flows dashboard..."
	kubectl create configmap ixp-flows-dashboard \
		--from-file=ixp-flows.json=./observability/ixp-flows.json \
		-n observability -o yaml --dry-run=client | kubectl apply -f -
	kubectl label configmap ixp-flows-dashboard \
		-n observability grafana_dashboard="1" --overwrite
	
	# Apply IXP Bids dashboard
	@echo "==> Deploying IXP Bids dashboard..."
	kubectl create configmap ixp-bids-dashboard \
		--from-file=ixp-bids.json=./observability/ixp-bids.json \
		-n observability -o yaml --dry-run=client | kubectl apply -f -
	kubectl label configmap ixp-bids-dashboard \
		-n observability grafana_dashboard="1" --overwrite
	
	# Apply IXP Auctions dashboard
	@echo "==> Deploying IXP Auctions dashboard..."
	kubectl create configmap ixp-auctions-dashboard \
		--from-file=ixp-auction.json=./observability/ixp-auction.json \
		-n observability -o yaml --dry-run=client | kubectl apply -f -
	kubectl label configmap ixp-auctions-dashboard \
		-n observability grafana_dashboard="1" --overwrite

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
	docker build -t telemetry-service:local ./telemetry
	minikube image load telemetry-service:local
	kubectl apply -f ./telemetry/deployment.yaml

deploy-agent:
	@echo "==> Deploying Customer Agent..."
	docker build -t customer-agent:local ./agent
	minikube image load customer-agent:local
	kubectl apply -f ./agent/deployment.yaml

# ============================================================
# Grouped deploys
# ============================================================
.PHONY: infra services all

infra: deploy-minikube deploy-kafka deploy-atomix deploy-config deploy-observability

services: vendor deploy-api deploy-auction deploy-telemetry deploy-dummy deploy-agent

all: infra services

# ============================================================
# Utilities
# ============================================================
.PHONY: vendor logs setup grafana-ui stop test

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

stop:
	minikube delete

proto:
	cd shared/proto && mkdir -p pb && protoc -I . --go_out=pb --go_opt=paths=source_relative *.proto

test:
	@echo "==> Running unit tests..."
	cd api && go test ./... && cd ..
	cd agent && go test ./... && cd ..
