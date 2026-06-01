package manifest

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// PolicyAttachment references a Policy by logical id or bundle file path.
type PolicyAttachment struct {
	Logical string // namespace.name
	File    string // bundle-relative policies/ path
}

// UnmarshalYAML accepts a logical ref string or {ref: policies/...}.
func (p *PolicyAttachment) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		return fmt.Errorf("policy attachment is nil")
	}
	switch value.Kind {
	case yaml.ScalarNode:
		logical := strings.TrimSpace(value.Value)
		if logical == "" {
			return fmt.Errorf("policy logical ref must not be empty")
		}
		p.Logical = logical
		p.File = ""
		return nil
	case yaml.MappingNode:
		var raw struct {
			Ref string `yaml:"ref"`
		}
		if err := value.Decode(&raw); err != nil {
			return err
		}
		file := strings.TrimSpace(raw.Ref)
		if file == "" {
			return fmt.Errorf("policy file ref must set ref")
		}
		p.File = file
		p.Logical = ""
		return nil
	default:
		return fmt.Errorf("policy attachment must be a string or object with ref")
	}
}

// IsZero reports whether the attachment is unset.
func (p PolicyAttachment) IsZero() bool {
	return strings.TrimSpace(p.Logical) == "" && strings.TrimSpace(p.File) == ""
}

// Describe returns a stable description for error messages.
func (p PolicyAttachment) Describe() string {
	if file := strings.TrimSpace(p.File); file != "" {
		return "ref:" + file
	}
	return strings.TrimSpace(p.Logical)
}
