echo "Deploying atomix-runtime..."
helm install -n kube-system atomix-runtime atomix/atomix-runtime --wait
echo "Deployed atomix-runtime."

echo "Deploying atomix stores..."
kubectl apply -f ./atomix/storage-profile.yaml
kubectl apply -f ./atomix/store.yaml
echo "Deployed atomix stores."