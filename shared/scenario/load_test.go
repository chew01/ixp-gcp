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
