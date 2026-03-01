VENDOR_MODULES = api auction dummy telemetry

# ============================================================
# Individual deploys
# ============================================================
.PHONY: deploy-minikube deploy-kafka deploy-atomix deploy-api \
        deploy-auction deploy-dummy deploy-telemetry deploy-monitoring

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
	minikube start
	minikube addons enable ingress

deploy-telemetry:
	@echo "==> Deploying Telemetry Processor..."
	docker build -t telemetry-service:local ./telemetry
	minikube image load telemetry-service:local
	kubectl apply -f ./telemetry/deployment.yaml

# ============================================================
# Grouped deploys
# ============================================================
.PHONY: infra services all

infra: deploy-minikube deploy-kafka deploy-atomix deploy-config

services: vendor deploy-api deploy-auction deploy-dummy deploy-telemetry

all: infra services

# ============================================================
# Utilities
# ============================================================
.PHONY: vendor logs

vendor:
	@for mod in $(VENDOR_MODULES); do \
		echo "==> Vendoring $$mod..."; \
		cd $$mod && go mod vendor && cd ..; \
	done

logs:
	kubectl logs -l app=$(SERVICE) -f --namespace $(NAMESPACE)