package manifest

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// SecretDefinition declares how a deploy-time credential is resolved (reference only).
type SecretDefinition struct {
	FromEnv string `yaml:"fromEnv" json:"fromEnv"`
}

func (s *SecretDefinition) UnmarshalYAML(node *yaml.Node) error {
	if node == nil || node.Kind == yaml.DocumentNode {
		if len(node.Content) > 0 {
			node = node.Content[0]
		}
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("must be a mapping")
	}
	raw := make(map[string]string)
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		switch key {
		case "fromEnv":
			var v string
			if err := node.Content[i+1].Decode(&v); err != nil {
				return err
			}
			raw[key] = v
		case "value", "plaintext":
			return fmt.Errorf("%q is not allowed in secret definitions", key)
		default:
			return fmt.Errorf("unknown field %q", key)
		}
	}
	s.FromEnv = raw["fromEnv"]
	return nil
}

func (s *SecretDefinition) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for key := range raw {
		switch key {
		case "fromEnv":
		case "value", "plaintext":
			return fmt.Errorf("%q is not allowed in secret definitions", key)
		default:
			return fmt.Errorf("unknown field %q", key)
		}
	}
	if v, ok := raw["fromEnv"]; ok {
		if err := json.Unmarshal(v, &s.FromEnv); err != nil {
			return err
		}
	}
	return nil
}
