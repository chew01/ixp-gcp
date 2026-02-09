bash deploy-minikube.sh
bash deploy-atomix.sh
kubectl create configmap test-scenario --from-file=scenario.yaml=./etc/scenario/scenario.yaml
bash deploy-kafka.sh
bash deploy-dummy.sh
bash deploy-telemetry.sh
bash deploy-api.sh
bash deploy-auction.sh
# bash deploy-monitoring.sh

minikube tunnel