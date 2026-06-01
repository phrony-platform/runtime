package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Bundle holds Tool and Policy documents discovered under a bundle root.
type Bundle struct {
	root     string
	tools    map[string]*Tool    // logical id -> tool
	policies map[string]*Policy  // logical id -> policy
}

// LoadBundle indexes tools/*.yaml and policies/*.yaml under bundleRoot.
func LoadBundle(bundleRoot string) (*Bundle, error) {
	absRoot, err := filepath.Abs(bundleRoot)
	if err != nil {
		return nil, fmt.Errorf("bundle root: %w", err)
	}
	b := &Bundle{
		root:     absRoot,
		tools:    make(map[string]*Tool),
		policies: make(map[string]*Policy),
	}
	if err := b.loadDir(filepath.Join(absRoot, "tools"), b.indexTool); err != nil {
		return nil, err
	}
	if err := b.loadDir(filepath.Join(absRoot, "policies"), b.indexPolicy); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *Bundle) loadDir(dir string, index func([]byte, string) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(ent.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		path := filepath.Join(dir, ent.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if err := index(data, path); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bundle) indexTool(data []byte, path string) error {
	doc, err := ParseDocument(data)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	tool, ok := doc.(*Tool)
	if !ok || tool.Kind != KindTool {
		return fmt.Errorf("%s: expected kind %s", path, KindTool)
	}
	if !IsSupportedAPIVersion(tool.APIVersion) {
		return FieldError{Path: path + ".apiVersion", Message: "must be " + APIVersionV1}
	}
	id := tool.LogicalID()
	if id == "" {
		return FieldError{Path: path, Message: "tool metadata.namespace and metadata.name are required"}
	}
	if _, dup := b.tools[id]; dup {
		return FieldError{Path: path, Message: "duplicate tool logical id " + id}
	}
	b.tools[id] = tool
	return nil
}

func (b *Bundle) indexPolicy(data []byte, path string) error {
	doc, err := ParseDocument(data)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	policy, ok := doc.(*Policy)
	if !ok || policy.Kind != KindPolicy {
		return fmt.Errorf("%s: expected kind %s", path, KindPolicy)
	}
	if !IsSupportedAPIVersion(policy.APIVersion) {
		return FieldError{Path: path + ".apiVersion", Message: "must be " + APIVersionV1}
	}
	id := policy.LogicalID()
	if id == "" {
		return FieldError{Path: path, Message: "policy metadata.namespace and metadata.name are required"}
	}
	if _, dup := b.policies[id]; dup {
		return FieldError{Path: path, Message: "duplicate policy logical id " + id}
	}
	b.policies[id] = policy
	return nil
}

// Tool returns the indexed tool for a logical id, if present.
func (b *Bundle) Tool(logicalID string) (*Tool, bool) {
	if b == nil {
		return nil, false
	}
	t, ok := b.tools[strings.TrimSpace(logicalID)]
	return t, ok
}

// Policy returns the indexed policy for a logical id, if present.
func (b *Bundle) Policy(logicalID string) (*Policy, bool) {
	if b == nil {
		return nil, false
	}
	p, ok := b.policies[strings.TrimSpace(logicalID)]
	return p, ok
}
