package scenario

// CustomerForIngressPort returns the customer ID that owns the given (switchID, ingressPort).
// If the scenario has been validated, every ingress port is assigned to exactly one customer.
// Returns ("", false) if not found (e.g. invalid scenario or port not assigned).
func CustomerForIngressPort(s *Scenario, switchID string, ingressPort uint32) (customerID string, ok bool) {
	for _, c := range s.Customers {
		if c.SwitchID != switchID {
			continue
		}
		for _, p := range c.IngressPorts {
			if p == ingressPort {
				return c.ID, true
			}
		}
	}
	return "", false
}
