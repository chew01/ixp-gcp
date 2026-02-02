echo "(Re)starting minikube..."
minikube delete
minikube start
minikube addons enable ingress
echo "Minikube started."

kubectl create configmap test-scenario --from-file=scenario.yaml=./etc/scenario/scenario.yaml


# API Gateway
echo "Building API Gateway docker image..."
docker build -t api-gateway:local ./api-gateway
echo "Docker image built."
echo "Loading API Gateway image into minikube..."
minikube image load api-gateway:local
echo "API Gateway image loaded."
echo "Applying API Gateway kubernetes configs..."
kubectl apply -f ./api-gateway/k8s/ingress.yaml
kubectl apply -f ./api-gateway/k8s/deployment.yaml
echo "API Gateway kubernetes configs applied."

# Telemetry
echo "Building Telemetry docker image..."
docker build -t telemetry-service:local ./telemetry
echo "Docker image built."
echo "Loading Telemetry image into minikube..."
minikube image load telemetry-service:local
echo "Telemetry image loaded."

echo "Applying Telemetry kubernetes configs..."
kubectl create namespace kafka
echo "Installing Strimzi Kafka operator..."
kubectl create -f 'https://strimzi.io/install/latest?namespace=kafka' -n kafka
echo "Strimzi Kafka operator installed."
echo "Applying Kafka cluster, topics, users, and Telemetry deployment..."
kubectl apply -f ./telemetry/k8s/kafka-cluster.yaml -n kafka
kubectl wait kafka/ixp-kafka --for=condition=Ready --timeout=300s -n kafka 
kubectl apply -f ./telemetry/k8s/topics.yaml -n kafka
kubectl apply -f ./telemetry/k8s/users.yaml -n kafka
kubectl apply -f ./telemetry/k8s/deployment.yaml
echo "Kafka cluster, topics, users, and Telemetry deployment applied."

# Can test kafka consumer using the following command:
# kubectl -n kafka run kafka-producer -ti --image=quay.io/strimzi/kafka:0.48.0-kafka-4.1.0 --rm=true --restart=Never -- bin/kafka-console-producer.sh --bootstrap-server ixp-kafka-kafka-bootstrap:9092 --topic telemetry.raw

# State Management
