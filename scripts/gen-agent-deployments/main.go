// gen-agent-deployments generates a Kubernetes Deployment manifest for each
// customer defined in a scenario YAML file. Output is piped to kubectl apply.
//
// Usage:
//
//	go run ./scripts/gen-agent-deployments /etc/scenario/scenario.yaml | kubectl apply -f -
package main

import (
	"fmt"
	"os"

	"github.com/chew01/ixp-gcp/shared/scenario"
)

const deploymentTemplate = `---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: customer-agent-%s
  labels:
    app: customer-agent
spec:
  replicas: 1
  selector:
    matchLabels:
      app: customer-agent-%s
  template:
    metadata:
      labels:
        app: customer-agent-%s
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9090"
        prometheus.io/path: "/metrics"
    spec:
      containers:
      - name: customer-agent
        image: customer-agent:local
        env:
          - name: CUSTOMER_ID
            value: "%s"
          - name: API_BASE_URL
            value: "http://api-gateway"
          - name: SCENARIO_PATH
            value: /etc/scenario/scenario.yaml
          - name: OTEL_SERVICE_NAME
            value: customer-agent-%s
          - name: OTEL_EXPORTER_OTLP_ENDPOINT
            value: "otel-collector-opentelemetry-collector.observability.svc.cluster.local:4317"
          - name: TELEMETRY_MODE
            value: "collector"
        volumeMounts:
          - name: scenario-volume
            mountPath: /etc/scenario
            readOnly: true
      volumes:
        - name: scenario-volume
          configMap:
            name: test-scenario
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: gen-agent-deployments <scenario.yaml>\n")
		os.Exit(1)
	}

	path := os.Args[1]
	scene, err := scenario.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading scenario: %v\n", err)
		os.Exit(1)
	}

	// Deduplicate: one Deployment per unique customer ID.
	seen := make(map[string]bool)
	for _, c := range scene.Customers {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		fmt.Printf(deploymentTemplate, c.ID, c.ID, c.ID, c.ID, c.ID)
	}
}
