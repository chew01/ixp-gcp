# Deploy Strimzi
echo "Deploying Strimzi Kafka operator..."
helm install strimzi-cluster-operator oci://quay.io/strimzi-helm/strimzi-kafka-operator

# Deploy Kafka Cluster
echo "Deploying Kafka cluster..."
kubectl apply -f ./kafka/kafka.yaml
kubectl wait kafka/ixp-kafka --for=condition=Ready --timeout=300s
echo "Kafka cluster deployed."