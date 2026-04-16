package scenario

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_WithCustomers(t *testing.T) {
	path := filepath.Join("..", "..", "etc", "scenario", "scenario.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("scenario file not found: %s", path)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.Customers) == 0 {
		t.Fatal("expected customers in scenario")
	}
	cid, ok := CustomerForIngressPort(s, "sw-1", 1)
	if !ok {
		t.Fatal("port 1 on sw-1 should have a customer")
	}
	if cid != "as12345" {
		t.Errorf("port 1 -> customer %q, want as12345", cid)
	}
	cid, ok = CustomerForIngressPort(s, "sw-1", 6)
	if !ok {
		t.Fatal("port 6 on sw-1 should have a customer")
	}
	if cid != "as67890" {
		t.Errorf("port 6 -> customer %q, want as67890", cid)
	}
}

func TestLoad_ValuationPerUnitDefault(t *testing.T) {
	// Write a temporary scenario without valuation_per_unit; confirm default is applied.
	yaml := `version: v1
name: test
reservation_price: 50
auction_interval: 30s
switches:
  - id: sw-1
    ingress_ports: [1, 2]
    egress_ports: [0]
    max_capacity: 100
customers:
  - id: as1
    switch_id: sw-1
    ingress_ports: [1]
    strategy: conservative
  - id: as2
    switch_id: sw-1
    ingress_ports: [2]
    strategy: conservative
    valuation_per_unit: 200
`
	f, err := os.CreateTemp("", "scenario-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(yaml); err != nil {
		t.Fatal(err)
	}
	f.Close()

	s, err := Load(f.Name())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// as1 had no valuation_per_unit — should default to 10 × reservation_price = 500
	for _, c := range s.Customers {
		if c.ID == "as1" {
			want := 10 * s.ReservationPrice
			if c.ValuationPerUnit != want {
				t.Errorf("as1 ValuationPerUnit=%d, want %d (10 × reservation_price)", c.ValuationPerUnit, want)
			}
		}
		// as2 had explicit valuation_per_unit=200 — should be preserved
		if c.ID == "as2" {
			if c.ValuationPerUnit != 200 {
				t.Errorf("as2 ValuationPerUnit=%d, want 200 (explicitly set)", c.ValuationPerUnit)
			}
		}
	}
}

func TestLoad_ValuationBelowReservationWarns(t *testing.T) {
	// valuation_per_unit below reservation_price should not fail — just warn.
	yaml := `version: v1
name: test
reservation_price: 50
auction_interval: 30s
switches:
  - id: sw-1
    ingress_ports: [1]
    egress_ports: [0]
    max_capacity: 100
customers:
  - id: as1
    switch_id: sw-1
    ingress_ports: [1]
    strategy: conservative
    valuation_per_unit: 10
`
	f, err := os.CreateTemp("", "scenario-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(yaml); err != nil {
		t.Fatal(err)
	}
	f.Close()

	s, err := Load(f.Name())
	if err != nil {
		t.Fatalf("Load should succeed even for low valuation: %v", err)
	}
	for _, c := range s.Customers {
		if c.ID == "as1" && c.ValuationPerUnit != 10 {
			t.Errorf("ValuationPerUnit=%d, want 10 (preserved as set)", c.ValuationPerUnit)
		}
	}
}
