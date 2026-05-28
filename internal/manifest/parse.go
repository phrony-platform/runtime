package manifest

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Parse decodes YAML bytes into an Agent document.
func Parse(data []byte) (*Agent, error) {
	var agent Agent
	if err := yaml.Unmarshal(data, &agent); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &agent, nil
}
