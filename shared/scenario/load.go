package scenario

import (
	"fmt"
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

	// Minimal validation
	if s.Version != "v1" {
		return nil, fmt.Errorf("unsupported scenario version: %s", s.Version)
	}

	return &s, nil
}
