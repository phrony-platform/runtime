package manifest

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Document is a parsed manifest document of any supported kind.
type Document interface {
	DocumentKind() string
}

// ParseDocument decodes YAML into an Agent, Tool, or Policy document based on kind.
func ParseDocument(data []byte) (Document, error) {
	var header struct {
		Kind string `yaml:"kind"`
	}
	if err := yaml.Unmarshal(data, &header); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	switch header.Kind {
	case KindAgent:
		return Parse(data)
	case KindTool:
		return ParseTool(data)
	case KindPolicy:
		return ParsePolicy(data)
	case "":
		return nil, fmt.Errorf("parse manifest: missing kind")
	default:
		return nil, fmt.Errorf("parse manifest: unsupported kind %q", header.Kind)
	}
}
