package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	ClosureMemberOriginVendored = "vendored"
	ClosureMemberOriginExternal = "external"
)

// ClosureMemberTarget is resolved delegation metadata for one closure edge.
// AgentVersionID is populated at bundle publish; local closure walks leave it empty.
type ClosureMemberTarget struct {
	ChildName      string
	AgentVersionID string
	Namespace      string
	Name           string
	Version        string
}

// ClosureContext maps parsed spec.agents edge refs to frozen closure members for
// expandSubagentBindings pinning during bundle publish.
type ClosureContext struct {
	byRef map[string]ClosureMemberTarget
}

// NewClosureContext builds a pinning context from a walked closure package.
func NewClosureContext(pkg *ClosurePackage) *ClosureContext {
	if pkg == nil {
		return nil
	}
	ctx := &ClosureContext{byRef: make(map[string]ClosureMemberTarget, len(pkg.Members))}
	for _, m := range pkg.Members {
		key := closureMemberRefKey(m)
		if key == "" {
			continue
		}
		ctx.byRef[key] = ClosureMemberTarget{
			ChildName:      m.ChildName,
			AgentVersionID: m.AgentVersionID,
			Namespace:      m.Namespace,
			Name:           m.Name,
			Version:        m.Version,
		}
	}
	return ctx
}

// Lookup returns the closure target for a parsed edge ref, if present.
func (c *ClosureContext) Lookup(edge AgentEdgeRef) (ClosureMemberTarget, bool) {
	if c == nil || len(c.byRef) == 0 {
		return ClosureMemberTarget{}, false
	}
	target, ok := c.byRef[edgeRefKey(edge)]
	return target, ok
}

// LockfileMember is one entry in a bundle lockfile.
type LockfileMember struct {
	ChildName   string `json:"child_name"`
	Origin      string `json:"origin"`
	ContentHash string `json:"content_hash,omitempty"`
	Ref         string `json:"ref,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	Name        string `json:"name,omitempty"`
	Version     string `json:"version,omitempty"`
}

// Lockfile is the publish-time closure artifact stored on bundle_versions.lock.
type Lockfile struct {
	Version       string           `json:"version"`
	RootChildName string           `json:"root_child_name"`
	Members       []LockfileMember `json:"members"`
}

// ClosureMember is one agent included in a walked bundle closure.
type ClosureMember struct {
	ChildName      string
	Origin         string
	Ref            string
	AgentPath      string
	ContentHash    string
	Resolved       *ResolvedAgent
	Namespace      string
	Name           string
	Version        string
	IsRoot         bool
	AgentVersionID string
}

// ClosurePackage is the ordered closure walk result for bundle publish.
type ClosurePackage struct {
	RootChildName string
	Members       []ClosureMember
	Lockfile      Lockfile
	Version       string
}

// WalkBundle performs a DFS closure walk from bundle.spec.root, validates members,
// compiles vendored agents, and emits a deterministic lockfile whose version is
// sha256(canonical_json(lock body)).
func WalkBundle(bundleRoot string, bundle *BundleManifest) (*ClosurePackage, error) {
	if bundle == nil {
		return nil, fmt.Errorf("bundle manifest is nil")
	}
	absRoot, err := filepath.Abs(strings.TrimSpace(bundleRoot))
	if err != nil {
		return nil, fmt.Errorf("bundle root: %w", err)
	}
	if err := ValidateBundle(bundle, absRoot); err != nil {
		return nil, err
	}

	state := &closureWalkState{
		bundleRoot: absRoot,
		opts: &ValidateOptions{
			BundleRoot:      absRoot,
			InBundleClosure: true,
		},
		visitedChild: make(map[string]struct{}),
		visitedRef:   make(map[string]struct{}),
		childNames:   make(map[string]string),
		onStack:      make(map[string]struct{}),
		stack:        nil,
	}

	rootRef := filepath.ToSlash(strings.TrimSpace(bundle.Spec.Root))
	if err := state.walkLocalRef(rootRef, true); err != nil {
		return nil, err
	}
	if len(state.members) == 0 {
		return nil, fmt.Errorf("bundle closure is empty")
	}

	pkg := &ClosurePackage{
		RootChildName: state.rootChildName,
		Members:       state.members,
	}
	pkg.Lockfile = buildLockfile(pkg)
	version, err := hashLockfileBody(pkg.Lockfile.RootChildName, pkg.Lockfile.Members)
	if err != nil {
		return nil, err
	}
	pkg.Version = version
	pkg.Lockfile.Version = version
	return pkg, nil
}

type closureWalkState struct {
	bundleRoot     string
	opts           *ValidateOptions
	members        []ClosureMember
	rootChildName  string
	visitedChild   map[string]struct{}
	visitedRef     map[string]struct{}
	childNames     map[string]string // child_name -> first ref for duplicate detection
	onStack        map[string]struct{}
	stack          []string
}

func (s *closureWalkState) walkLocalRef(ref string, isRoot bool) error {
	ref = filepath.ToSlash(strings.TrimSpace(ref))
	if ref == "" {
		return fmt.Errorf("agent ref is empty")
	}

	agentPath := filepath.Join(s.bundleRoot, filepath.FromSlash(ref))
	if !isPathWithinRoot(s.bundleRoot, agentPath) {
		return FieldError{Path: ref, Message: "must resolve inside bundle root"}
	}

	data, err := os.ReadFile(agentPath)
	if err != nil {
		return fmt.Errorf("read agent %q: %w", ref, err)
	}
	agent, err := Parse(data)
	if err != nil {
		return fmt.Errorf("parse agent %q: %w", ref, err)
	}
	if err := ValidateAgent(agent, s.opts); err != nil {
		return fmt.Errorf("validate agent %q: %w", ref, err)
	}

	childName := strings.TrimSpace(agent.Metadata.Name)
	if childName == "" {
		return FieldError{Path: ref + ".metadata.name", Message: "is required"}
	}
	if otherRef, dup := s.childNames[childName]; dup && otherRef != ref {
		return fmt.Errorf("duplicate bundle child_name %q from refs %q and %q", childName, otherRef, ref)
	}
	s.childNames[childName] = ref

	if _, onStack := s.onStack[childName]; onStack {
		return fmt.Errorf("bundle closure cycle detected: %s", strings.Join(append(s.stack, childName), " -> "))
	}
	if _, visited := s.visitedChild[childName]; visited {
		s.visitedRef[ref] = struct{}{}
		return nil
	}
	if _, seen := s.visitedRef[ref]; seen {
		return nil
	}

	s.onStack[childName] = struct{}{}
	s.stack = append(s.stack, childName)
	defer func() {
		delete(s.onStack, childName)
		s.stack = s.stack[:len(s.stack)-1]
	}()

	compiled, err := Compile(agentPath, agent)
	if err != nil {
		return fmt.Errorf("compile agent %q: %w", ref, err)
	}
	jsonBytes, err := compiled.JSON()
	if err != nil {
		return fmt.Errorf("encode agent %q: %w", ref, err)
	}

	member := ClosureMember{
		ChildName:   childName,
		Origin:      ClosureMemberOriginVendored,
		Ref:         ref,
		AgentPath:   agentPath,
		ContentHash: contentHash(jsonBytes),
		Resolved:    compiled,
		IsRoot:      isRoot,
	}
	if isRoot {
		s.rootChildName = childName
	}

	s.members = append(s.members, member)

	for i, sub := range agent.Spec.Agents {
		if sub.LateBound {
			continue
		}
		edge, err := ParseAgentEdgeRef(sub.Ref, sub.LateBound)
		if err != nil {
			return FieldError{Path: fmt.Sprintf("%s: spec.agents[%d].ref", ref, i), Message: err.Error()}
		}
		switch edge.Kind {
		case AgentEdgeRefKindLocal:
			if err := s.walkLocalRef(edge.Path, false); err != nil {
				return err
			}
		case AgentEdgeRefKindExternal:
			if err := s.recordExternal(edge, sub.Ref); err != nil {
				return err
			}
		}
	}

	s.visitedChild[childName] = struct{}{}
	s.visitedRef[ref] = struct{}{}
	return nil
}

func (s *closureWalkState) recordExternal(edge AgentEdgeRef, authoredRef string) error {
	key := edgeRefKey(edge)
	if _, seen := s.visitedRef[key]; seen {
		return nil
	}

	namespace := strings.TrimSpace(edge.External.Namespace)
	name := strings.TrimSpace(edge.External.Name)
	version := strings.TrimSpace(edge.External.Constraint)
	childName := name
	if childName == "" {
		return fmt.Errorf("external agent ref %q is missing name", authoredRef)
	}
	if otherRef, dup := s.childNames[childName]; dup && otherRef != key {
		return fmt.Errorf("duplicate bundle child_name %q from refs %q and %q", childName, otherRef, authoredRef)
	}
	s.childNames[childName] = key

	member := ClosureMember{
		ChildName: childName,
		Origin:    ClosureMemberOriginExternal,
		Ref:       strings.TrimSpace(authoredRef),
		Namespace: namespace,
		Name:      name,
		Version:   version,
	}
	s.members = append(s.members, member)
	s.visitedChild[childName] = struct{}{}
	s.visitedRef[key] = struct{}{}
	return nil
}

func buildLockfile(pkg *ClosurePackage) Lockfile {
	members := make([]LockfileMember, 0, len(pkg.Members))
	for _, m := range pkg.Members {
		entry := LockfileMember{
			ChildName: m.ChildName,
			Origin:    m.Origin,
		}
		switch m.Origin {
		case ClosureMemberOriginVendored:
			entry.Ref = m.Ref
			entry.ContentHash = m.ContentHash
		case ClosureMemberOriginExternal:
			entry.Namespace = m.Namespace
			entry.Name = m.Name
			entry.Version = m.Version
			entry.Ref = m.Ref
		}
		members = append(members, entry)
	}
	return Lockfile{
		RootChildName: pkg.RootChildName,
		Members:       members,
	}
}

type lockfileBody struct {
	RootChildName string           `json:"root_child_name"`
	Members       []LockfileMember `json:"members"`
}

func hashLockfileBody(rootChildName string, members []LockfileMember) (string, error) {
	body := lockfileBody{
		RootChildName: rootChildName,
		Members:       members,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("encode lockfile: %w", err)
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// LockfileVersion returns the bundle version label sha256:… for a lockfile body.
func LockfileVersion(rootChildName string, members []LockfileMember) (string, error) {
	return hashLockfileBody(rootChildName, members)
}

// ApplyClosurePinning expands any remaining spec.agents entries and pins compiled
// agent tool bindings from the walked closure (AgentVersionID, ChildName, identity).
func ApplyClosurePinning(agent *Agent, closure *ClosureContext) error {
	if err := expandSubagentBindings(agent, closure); err != nil {
		return err
	}
	return PinCompiledAgentBindings(agent, closure)
}

// PinCompiledAgentBindings sets frozen delegation targets on compiled spec.tools
// agent bindings using a publish-time closure context.
func PinCompiledAgentBindings(agent *Agent, closure *ClosureContext) error {
	if agent == nil || closure == nil {
		return nil
	}
	for i := range agent.Spec.Tools {
		tb := &agent.Spec.Tools[i]
		if !tb.IsAgent() || tb.Agent == nil {
			continue
		}
		if tb.Agent.LateBound {
			continue
		}
		edge, err := ParseAgentEdgeRef(tb.Ref, tb.Agent.LateBound)
		if err != nil {
			return FieldError{Path: fmt.Sprintf("spec.tools[%d].ref", i), Message: err.Error()}
		}
		target, ok := closure.Lookup(edge)
		if !ok {
			return FieldError{
				Path:    fmt.Sprintf("spec.tools[%d]", i),
				Message: "delegation target not in bundle closure",
			}
		}
		if strings.TrimSpace(target.AgentVersionID) == "" {
			return FieldError{
				Path:    fmt.Sprintf("spec.tools[%d].agent", i),
				Message: "closure target missing agent_version_id",
			}
		}
		tb.Agent.AgentVersionID = target.AgentVersionID
		if target.ChildName != "" {
			tb.Agent.ChildName = target.ChildName
		}
		if target.Namespace != "" {
			tb.Agent.Namespace = target.Namespace
		}
		if target.Name != "" {
			tb.Agent.Name = target.Name
		}
		if edge.Kind == AgentEdgeRefKindExternal && target.Version != "" {
			tb.Agent.Version = target.Version
		}
	}
	return nil
}

func contentHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func closureMemberRefKey(m ClosureMember) string {
	switch m.Origin {
	case ClosureMemberOriginVendored:
		return edgeRefKey(AgentEdgeRef{
			Kind: AgentEdgeRefKindLocal,
			Path: filepath.ToSlash(m.Ref),
		})
	case ClosureMemberOriginExternal:
		raw := LogicalID(m.Namespace, m.Name)
		if v := strings.TrimSpace(m.Version); v != "" {
			raw += "@" + v
		}
		return edgeRefKey(AgentEdgeRef{
			Kind: AgentEdgeRefKindExternal,
			External: ParsedLogicalRef{
				Namespace:  m.Namespace,
				Name:       m.Name,
				Constraint: m.Version,
				Raw:        raw,
			},
		})
	default:
		return ""
	}
}
