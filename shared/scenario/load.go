package scenario

import (
	"fmt"
	"log"
	"os"

	"github.com/goccy/go-yaml"
)

func Load(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read scenario: %w", err)
	}

	var s Scenario
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse scenario: %w", err)
	}

	if s.Version != "v1" {
		return nil, fmt.Errorf("unsupported scenario version: %s", s.Version)
	}

	if err := validateCustomers(&s); err != nil {
		return nil, err
	}

	applyValuationDefaults(&s)

	return &s, nil
}

// applyValuationDefaults sets ValuationPerUnit to 10 × ReservationPrice for any
// customer that omits the field, preserving backward compatibility. It also warns
// when the configured value is below ReservationPrice, which would guarantee
// negative utility on every round.
func applyValuationDefaults(s *Scenario) {
	for i := range s.Customers {
		if s.Customers[i].ValuationPerUnit <= 0 {
			s.Customers[i].ValuationPerUnit = 10 * s.ReservationPrice
		}
		if s.Customers[i].ValuationPerUnit < s.ReservationPrice {
			log.Printf("warning: customer %s valuation_per_unit (%d) is below reservation_price (%d); utility will be negative every round",
				s.Customers[i].ID, s.Customers[i].ValuationPerUnit, s.ReservationPrice)
		}
	}
}

// validateCustomers ensures every ingress port (across all switches) is assigned to
// exactly one customer, and that all customer-referenced ports exist in that switch's IngressPorts.
func validateCustomers(s *Scenario) error {
	// Build set of (switchID, port) that exist in topology
	allowedPorts := make(map[string]map[uint32]bool)
	for _, sw := range s.Switches {
		if allowedPorts[sw.ID] == nil {
			allowedPorts[sw.ID] = make(map[uint32]bool)
		}
		for _, p := range sw.IngressPorts {
			allowedPorts[sw.ID][p] = true
		}
	}

	// Track which (switchID, port) are assigned (must be exactly once)
	assigned := make(map[string]map[uint32]string) // switchID -> port -> customerID

	for _, c := range s.Customers {
		if c.ID == "" {
			return fmt.Errorf("customer with empty id")
		}
		if c.SwitchID == "" {
			return fmt.Errorf("customer %q has empty switch_id", c.ID)
		}
		allowed, ok := allowedPorts[c.SwitchID]
		if !ok {
			return fmt.Errorf("customer %q references switch %q which does not exist", c.ID, c.SwitchID)
		}
		if assigned[c.SwitchID] == nil {
			assigned[c.SwitchID] = make(map[uint32]string)
		}
		for _, p := range c.IngressPorts {
			if !allowed[p] {
				return fmt.Errorf("customer %q references port %d on switch %q which is not in that switch's ingress_ports", c.ID, p, c.SwitchID)
			}
			if prev, ok := assigned[c.SwitchID][p]; ok {
				return fmt.Errorf("ingress port %d on switch %q assigned to both %q and %q", p, c.SwitchID, prev, c.ID)
			}
			assigned[c.SwitchID][p] = c.ID
		}
	}

	// Every allowed (switch, port) must be assigned to exactly one customer
	for _, sw := range s.Switches {
		for _, p := range sw.IngressPorts {
			cid, ok := assigned[sw.ID][p]
			if !ok || cid == "" {
				return fmt.Errorf("ingress port %d on switch %q is not assigned to any customer", p, sw.ID)
			}
		}
	}

	return nil
}
