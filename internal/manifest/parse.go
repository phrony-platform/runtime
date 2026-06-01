package manifest

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Parse decodes YAML bytes into an Agent document.
func Parse(data []byte) (*Agent, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if err := checkForbiddenYAML(&doc); err != nil {
		return nil, err
	}
	var agent Agent
	if err := doc.Decode(&agent); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &agent, nil
}
