package manifest

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

const KindTool = "Tool"

// Tool is a v1 Tool contract document (kind Tool).
type Tool struct {
	APIVersion string       `yaml:"apiVersion" json:"apiVersion"`
	Kind       string       `yaml:"kind" json:"kind"`
	Metadata   ToolMetadata `yaml:"metadata" json:"metadata"`
	Spec       ToolSpec     `yaml:"spec" json:"spec"`
}

func (t *Tool) DocumentKind() string {
	if t == nil {
		return ""
	}
	return t.Kind
}

// ToolMetadata holds identity for a Tool document.
type ToolMetadata struct {
	Name      string            `yaml:"name" json:"name"`
	Namespace string            `yaml:"namespace" json:"namespace"`
	Version   string            `yaml:"version" json:"version"`
	Labels    map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

// ToolSpec is the portable tool contract.
type ToolSpec struct {
	Description      string      `yaml:"description,omitempty" json:"description,omitempty"`
	SideEffectClass  string      `yaml:"side_effect_class,omitempty" json:"side_effect_class,omitempty"`
	InputSchema      *SchemaSpec `yaml:"input_schema,omitempty" json:"input_schema,omitempty"`
	OutputSchema     *SchemaSpec `yaml:"output_schema,omitempty" json:"output_schema,omitempty"`
	DefaultPolicies  []string    `yaml:"default_policies,omitempty" json:"default_policies,omitempty"`
}

// LogicalID returns the catalog id namespace.name for this tool.
func (t *Tool) LogicalID() string {
	if t == nil {
		return ""
	}
	return LogicalID(t.Metadata.Namespace, t.Metadata.Name)
}

// ParseTool decodes YAML bytes into a Tool document.
func ParseTool(data []byte) (*Tool, error) {
	var tool Tool
	if err := yaml.Unmarshal(data, &tool); err != nil {
		return nil, fmt.Errorf("parse tool: %w", err)
	}
	return &tool, nil
}
