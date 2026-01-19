kubectl create namespace monitoring

# Prometheus
helm install prometheus prometheus-community/prometheus -n monitoring
kubectl edit configmap prometheus-server -n monitoring

# Grafana
helm install grafana grafana/grafana -n monitoring
export POD_NAME=$(kubectl get pods --namespace default -l "app.kubernetes.io/name=grafana,app.kubernetes.io/instance=grafana" -o jsonpath="{.items[0].metadata.name}")
kubectl --namespace default port-forward $POD_NAME 3000 &

echo "Grafana admin password:"
kubectl get secret -n monitoring grafana -o jsonpath="{.data.admin-password}" | base64 --decode ; echo
echo "Login at http://localhost:3000 with username 'admin'"