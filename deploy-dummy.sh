echo "Building Dummy docker image..."
docker build -t dummy-producer:local ./dummy
echo "Docker image built."
echo "Loading Dummy image into minikube..."
minikube image load dummy-producer:local
echo "Dummy image loaded."

echo "Applying Dummy kubernetes configs..."
kubectl apply -f ./dummy/deployment.yaml