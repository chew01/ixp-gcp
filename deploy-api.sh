# API Gateway
echo "Building API Gateway docker image..."
docker build -t api-gateway:local ./api
echo "Docker image built."
echo "Loading API Gateway image into minikube..."
minikube image load api-gateway:local
echo "API Gateway image loaded."
echo "Applying API Gateway kubernetes configs..."
kubectl apply -f ./api/ingress.yaml
kubectl apply -f ./api/deployment.yaml
echo "API Gateway kubernetes configs applied."