package manifest

import (
	"fmt"
	"sort"
	"strings"
)

// SecretMember is one closure member contributing secrets to a bundle union.
type SecretMember struct {
	ChildName string
	Agent     *Agent
}

// UnionBundleSecrets merges secrets across closure members.
// Returns an error if two members use the same secret name with different fromEnv.
func UnionBundleSecrets(members []ClosureMember) (map[string]SecretDefinition, error) {
	return UnionAgentSecrets(closureMembersToSecretMembers(members))
}

// UnionAgentSecrets merges secrets across bundle members with parsed agent manifests.
func UnionAgentSecrets(members []SecretMember) (map[string]SecretDefinition, error) {
	union := make(map[string]SecretDefinition)
	firstMember := make(map[string]string)
	firstFromEnv := make(map[string]string)

	for _, m := range members {
		if m.Agent == nil || len(m.Agent.Secrets) == 0 {
			continue
		}
		childName := strings.TrimSpace(m.ChildName)
		if childName == "" {
			childName = strings.TrimSpace(m.Agent.Metadata.Name)
		}
		for name, def := range m.Agent.Secrets {
			fromEnv := strings.TrimSpace(def.FromEnv)
			if existing, ok := union[name]; ok {
				if strings.TrimSpace(existing.FromEnv) != fromEnv {
					return nil, fmt.Errorf(
						`secret %q: member %q uses %s, member %q uses %s`,
						name, firstMember[name], firstFromEnv[name], childName, fromEnv,
					)
				}
				continue
			}
			union[name] = def
			firstMember[name] = childName
			firstFromEnv[name] = fromEnv
		}
	}
	return union, nil
}

// BundleSecretDeclaredBy maps each secret name to the member child_names that declare it.
func BundleSecretDeclaredBy(members []SecretMember) map[string][]string {
	declared := make(map[string]map[string]struct{})
	for _, m := range members {
		if m.Agent == nil || len(m.Agent.Secrets) == 0 {
			continue
		}
		childName := strings.TrimSpace(m.ChildName)
		if childName == "" {
			childName = strings.TrimSpace(m.Agent.Metadata.Name)
		}
		for name := range m.Agent.Secrets {
			if declared[name] == nil {
				declared[name] = make(map[string]struct{})
			}
			declared[name][childName] = struct{}{}
		}
	}
	out := make(map[string][]string, len(declared))
	for name, children := range declared {
		names := make([]string, 0, len(children))
		for child := range children {
			names = append(names, child)
		}
		sort.Strings(names)
		out[name] = names
	}
	return out
}

func closureMembersToSecretMembers(members []ClosureMember) []SecretMember {
	out := make([]SecretMember, 0, len(members))
	for _, m := range members {
		if m.Resolved == nil || m.Resolved.Agent == nil {
			continue
		}
		out = append(out, SecretMember{
			ChildName: m.ChildName,
			Agent:     m.Resolved.Agent,
		})
	}
	return out
}
