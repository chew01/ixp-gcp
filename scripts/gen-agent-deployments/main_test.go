package main

import (
	"os"
	"strings"
	"testing"
)

const testScenario = `version: v1
name: test
reservation_price: 50
auction_interval: 30s
switches:
  - id: sw-1
    ingress_ports: [1, 2, 3, 4]
    egress_ports: [0]
    max_capacity: 100
customers:
  - id: as11111
    switch_id: sw-1
    ingress_ports: [1, 2]
    strategy: conservative
  - id: as22222
    switch_id: sw-1
    ingress_ports: [3, 4]
    strategy: conservative
`

func TestGeneratesOneDeploymentPerCustomer(t *testing.T) {
	f, err := os.CreateTemp("", "scenario-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(testScenario); err != nil {
		t.Fatal(err)
	}
	f.Close()

	// Capture stdout by temporarily redirecting via a pipe.
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	os.Args = []string{"gen-agent-deployments", f.Name()}
	main()

	w.Close()
	os.Stdout = old

	var buf strings.Builder
	tmp := make([]byte, 4096)
	for {
		n, err := r.Read(tmp)
		buf.Write(tmp[:n])
		if err != nil {
			break
		}
	}
	output := buf.String()

	// Should contain one Deployment per unique customer ID.
	if !strings.Contains(output, "customer-agent-as11111") {
		t.Error("expected deployment for as11111")
	}
	if !strings.Contains(output, "customer-agent-as22222") {
		t.Error("expected deployment for as22222")
	}

	// Each deployment should set CUSTOMER_ID env var correctly.
	if !strings.Contains(output, `value: "as11111"`) {
		t.Error("expected CUSTOMER_ID=as11111 in deployment")
	}
	if !strings.Contains(output, `value: "as22222"`) {
		t.Error("expected CUSTOMER_ID=as22222 in deployment")
	}

	// Count deployments (each starts with "---\napiVersion")
	count := strings.Count(output, "kind: Deployment")
	if count != 2 {
		t.Errorf("expected 2 Deployment manifests, got %d", count)
	}
}
