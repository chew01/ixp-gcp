package main

import (
	"testing"

	"github.com/chew01/ixp-gcp/agent/strategy"
	"github.com/chew01/ixp-gcp/shared/scenario"
)

func TestDeriveCustomerPorts(t *testing.T) {
	scene := &scenario.Scenario{
		Switches: []scenario.Switch{
			{ID: "sw-1", IngressPorts: []uint32{1, 2, 3}, EgressPorts: []uint32{0}},
		},
		Customers: []scenario.Customer{
			{ID: "as12345", SwitchID: "sw-1", IngressPorts: []uint32{1, 2}},
			{ID: "as67890", SwitchID: "sw-1", IngressPorts: []uint32{3}},
		},
	}

	ports := deriveCustomerPorts(scene, "as12345")
	if len(ports) != 1 {
		t.Fatalf("expected 1 switch entry, got %d", len(ports))
	}
	cp := ports[0]
	if cp.SwitchID != "sw-1" {
		t.Fatalf("expected switch sw-1, got %s", cp.SwitchID)
	}
	if len(cp.Ingress) != 2 {
		t.Fatalf("expected 2 ingress ports, got %d", len(cp.Ingress))
	}
	if cp.Ingress[0] != 1 || cp.Ingress[1] != 2 {
		t.Fatalf("unexpected ingress ports: %+v", cp.Ingress)
	}
	if len(cp.Egress) != 1 || cp.Egress[0] != 0 {
		t.Fatalf("unexpected egress ports: %+v", cp.Egress)
	}
}

func TestSelectStrategy(t *testing.T) {
	t.Run("conservative returns Conservative", func(t *testing.T) {
		s, err := selectStrategy("conservative")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := s.(strategy.Conservative); !ok {
			t.Errorf("expected strategy.Conservative, got %T", s)
		}
	})

	t.Run("unknown strategy returns error", func(t *testing.T) {
		_, err := selectStrategy("unknown")
		if err == nil {
			t.Fatal("expected error for unknown strategy, got nil")
		}
	})
}
