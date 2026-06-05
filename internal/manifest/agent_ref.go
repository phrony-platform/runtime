package manifest

import (
	"fmt"
	"path/filepath"
	"strings"
)

// AgentEdgeRefKind classifies a spec.agents ref as a bundle-local path or an
// external catalog logical id.
type AgentEdgeRefKind string

const (
	AgentEdgeRefKindLocal    AgentEdgeRefKind = "local"
	AgentEdgeRefKindExternal AgentEdgeRefKind = "external"
)

// AgentEdgeRef is a parsed spec.agents delegation target.
type AgentEdgeRef struct {
	Kind      AgentEdgeRefKind
	Path      string           // bundle-relative path when Kind is local
	External  ParsedLogicalRef // catalog ref when Kind is external
	LateBound bool
}

// IsLocalAgentRef reports whether ref is a bundle-relative agent manifest path.
func IsLocalAgentRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	if strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "../") {
		return true
	}
	if strings.Contains(ref, "/") || strings.Contains(ref, "\\") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(ref))
	return ext == ".yaml" || ext == ".yml"
}

// IsExternalAgentRef reports whether ref is a namespace.name[@version] catalog id.
func IsExternalAgentRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" || IsLocalAgentRef(ref) {
		return false
	}
	_, err := ParseLogicalRef(ref)
	return err == nil
}

// ParseAgentEdgeRef classifies and parses a spec.agents ref. lateBound is the
// authored late_bound flag on the SubagentBinding.
func ParseAgentEdgeRef(ref string, lateBound bool) (AgentEdgeRef, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return AgentEdgeRef{}, fmt.Errorf("ref is empty")
	}
	out := AgentEdgeRef{LateBound: lateBound}
	if IsLocalAgentRef(ref) {
		out.Kind = AgentEdgeRefKindLocal
		out.Path = filepath.FromSlash(ref)
		return out, nil
	}
	parsed, err := ParseLogicalRef(ref)
	if err != nil {
		return AgentEdgeRef{}, fmt.Errorf("ref %q must be a local path (./...) or namespace.name[@version]", ref)
	}
	out.Kind = AgentEdgeRefKindExternal
	out.External = parsed
	return out, nil
}

// edgeRefKey returns a stable deduplication key for a parsed edge ref.
func edgeRefKey(edge AgentEdgeRef) string {
	switch edge.Kind {
	case AgentEdgeRefKindLocal:
		return "local:" + edge.Path
	default:
		return "external:" + edge.External.Raw
	}
}
