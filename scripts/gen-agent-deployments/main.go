// gen-agent-deployments generates a Kubernetes Deployment manifest for each
// customer defined in a scenario YAML file. Output is piped to kubectl apply.
//
// Usage:
//
//	go run ./scripts/gen-agent-deployments <scenario.yaml> [--image <image-ref>]
//
// The optional --image flag overrides the container image used in every generated
// Deployment. Defaults to "customer-agent:local" (Minikube local image).
// For cloud deployments pass the full registry reference, e.g.:
//
//	go run ./scripts/gen-agent-deployments scenario.yaml \
//	  --image registry.digitalocean.com/my-registry/customer-agent:v1.0.0
package main

import (
	"flag"
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
    spec:
      containers:
      - name: customer-agent
        image: %s
        env:
          - name: CUSTOMER_ID
            value: "%s"
          - name: API_BASE_URL
            value: "http://api-gateway"
          - name: SCENARIO_PATH
            value: /etc/scenario/scenario.yaml
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
	imageFlag := flag.String("image", "customer-agent:local", "container image reference for the customer-agent")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: gen-agent-deployments <scenario.yaml> [--image <image-ref>]\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	path := args[0]
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
		fmt.Printf(deploymentTemplate, c.ID, c.ID, c.ID, *imageFlag, c.ID)
	}
}
