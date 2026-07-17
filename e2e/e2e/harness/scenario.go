package harness

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// AgentMeta holds identity fields from an agent.yaml.
type AgentMeta struct {
	Namespace string
	Name      string
	Version   string
}

// ReadAgentMeta parses metadata from an agent manifest path.
func ReadAgentMeta(agentPath string) (AgentMeta, error) {
	data, err := os.ReadFile(agentPath)
	if err != nil {
		return AgentMeta{}, err
	}
	var doc struct {
		Metadata struct {
			Name      string `yaml:"name"`
			Namespace string `yaml:"namespace"`
			Version   string `yaml:"version"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return AgentMeta{}, fmt.Errorf("parse agent metadata: %w", err)
	}
	meta := AgentMeta{
		Namespace: strings.TrimSpace(doc.Metadata.Namespace),
		Name:      strings.TrimSpace(doc.Metadata.Name),
		Version:   strings.TrimSpace(doc.Metadata.Version),
	}
	if meta.Namespace == "" || meta.Name == "" || meta.Version == "" {
		return AgentMeta{}, fmt.Errorf("agent metadata.name, metadata.namespace, and metadata.version are required")
	}
	return meta, nil
}

// AgentRef formats namespace/name.
func (m AgentMeta) AgentRef() string {
	return fmt.Sprintf("%s/%s", m.Namespace, m.Name)
}

// AgentVersionRef formats namespace/name@version.
func (m AgentMeta) AgentVersionRef() string {
	return fmt.Sprintf("%s/%s@%s", m.Namespace, m.Name, m.Version)
}
