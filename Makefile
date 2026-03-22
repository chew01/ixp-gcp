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
	kubectl create configmap test-scenario --from-file=scenario.yaml=./etc/scenario/scenario.yaml

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
	helm repo add atomix https://atomix.github.io/charts.atomix.io
	helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
	helm repo update

grafana-ui:
	kubectl port-forward svc/monitoring-grafana 3000:80 -n monitoring &
	@echo "Grafana at http://localhost:3000"
	@echo "Password: $$(kubectl get secret monitoring-grafana -n monitoring \
		-o jsonpath='{.data.admin-password}' | base64 --decode)"

stop:
	minikube delete

proto:
	cd shared/proto && mkdir -p pb && protoc -I . --go_out=pb --go_opt=paths=source_relative *.proto

test:
	@echo "==> Running unit tests..."
	cd api && go test ./... && cd ..
	cd agent && go test ./... && cd ..