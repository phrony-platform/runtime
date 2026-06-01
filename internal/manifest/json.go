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
	if !isCompiledPolicySnapshot(&agent) && len(agent.Spec.Policies) > 0 {
		return nil, fmt.Errorf("parse manifest: spec.policies is only valid on compiled snapshots")
	}
	return &agent, nil
}
