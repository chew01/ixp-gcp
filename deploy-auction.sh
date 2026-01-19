# API Gateway
echo "Building Auction runner docker image..."
docker build -t auction-runner:local ./auction
echo "Docker image built."
echo "Loading Auction runner image into minikube..."
minikube image load auction-runner:local
echo "Auction runner image loaded."
echo "Applying Auction runner kubernetes configs..."
kubectl apply -f ./auction/deployment.yaml
echo "Auction runner kubernetes configs applied."