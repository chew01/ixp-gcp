echo "Building Telemetry docker image..."
docker build -t telemetry-service:local ./telemetry
echo "Docker image built."
echo "Loading Telemetry image into minikube..."
minikube image load telemetry-service:local
echo "Telemetry image loaded."

echo "Applying Telemetry kubernetes configs..."
kubectl apply -f ./telemetry/deployment.yaml