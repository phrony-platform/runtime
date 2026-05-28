package manifest

import (
	"encoding/json"
	"fmt"
)

// ParseJSON decodes canonical resolved manifest JSON from the CLI deploy transport.
func ParseJSON(data []byte) (*Agent, error) {
	var agent Agent
	if err := json.Unmarshal(data, &agent); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &agent, nil
}
