package manifest

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

func checkForbiddenYAML(doc *yaml.Node) error {
	if doc == nil || len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}
	compiled := mappingHasCompiledPolicies(root)
	spec := mappingValueNode(root, "spec")
	if spec == nil {
		return nil
	}
	if node := mappingValueNode(spec, "hitl"); node != nil && !nodeIsEmpty(node) {
		return fmt.Errorf("parse manifest: spec.hitl is not supported; use kind: Policy documents")
	}
	if !compiled {
		if node := mappingValueNode(spec, "policies"); node != nil && !nodeIsEmpty(node) {
			return fmt.Errorf("parse manifest: spec.policies is not supported on the Agent; use kind: Policy documents referenced from default_policies or binding policies")
		}
	}
	tools := mappingValueNode(spec, "tools")
	if tools == nil || tools.Kind != yaml.SequenceNode {
		return nil
	}
	for i, item := range tools.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		base := fmt.Sprintf("spec.tools[%d]", i)
		for _, key := range []string{"name", "parameters", "policy", "version"} {
			if node := mappingValueNode(item, key); node != nil && !nodeIsEmpty(node) {
				return fmt.Errorf("parse manifest: %s.%s is not supported", base, key)
			}
		}
	}
	return nil
}

func mappingHasCompiledPolicies(root *yaml.Node) bool {
	meta := mappingValueNode(root, "metadata")
	if meta == nil {
		return false
	}
	ann := mappingValueNode(meta, "annotations")
	if ann == nil {
		return false
	}
	val := mappingValueNode(ann, AnnotationPoliciesCompiled)
	return nodeScalar(val) == "true"
}

func mappingValueNode(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if nodeScalar(m.Content[i]) == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func nodeScalar(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	if n.Kind == yaml.ScalarNode {
		return strings.TrimSpace(n.Value)
	}
	return ""
}

func nodeIsEmpty(n *yaml.Node) bool {
	if n == nil {
		return true
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return strings.TrimSpace(n.Value) == ""
	case yaml.SequenceNode:
		return len(n.Content) == 0
	case yaml.MappingNode:
		return len(n.Content) == 0
	default:
		return false
	}
}
