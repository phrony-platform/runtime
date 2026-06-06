package manifest

import (
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const KindBundle = "Bundle"

// BundleManifest is the v1 Bundle packaging document (kind: Bundle). It declares
// the root agent for a multi-agent closure; bundle.lock.json is a committed sidecar.
type BundleManifest struct {
	APIVersion string              `yaml:"apiVersion" json:"apiVersion"`
	Kind       string              `yaml:"kind" json:"kind"`
	Metadata   BundleMetadata      `yaml:"metadata" json:"metadata"`
	Spec       BundleManifestSpec  `yaml:"spec" json:"spec"`
}

// DocumentKind returns the manifest kind.
func (b *BundleManifest) DocumentKind() string {
	if b == nil {
		return ""
	}
	return b.Kind
}

// BundleMetadata holds identity for a Bundle document.
type BundleMetadata struct {
	Name      string            `yaml:"name" json:"name"`
	Namespace string            `yaml:"namespace" json:"namespace"`
	Labels    map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

// BundleManifestSpec is the packaging envelope for a multi-agent bundle.
type BundleManifestSpec struct {
	// Root is a bundle-relative path to the root kind: Agent manifest.
	Root string `yaml:"root" json:"root"`
}

// ParseBundle decodes YAML bytes into a Bundle document.
func ParseBundle(data []byte) (*BundleManifest, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse bundle: %w", err)
	}
	if err := checkForbiddenYAML(&doc); err != nil {
		return nil, err
	}
	var bundle BundleManifest
	if err := doc.Decode(&bundle); err != nil {
		return nil, fmt.Errorf("parse bundle: %w", err)
	}
	return &bundle, nil
}

// ValidateBundle checks structural and semantic rules for a parsed Bundle document.
func ValidateBundle(bundle *BundleManifest, bundleRoot string) error {
	if bundle == nil {
		return ValidationErrors{{Path: "", Message: "manifest is nil"}}
	}
	var errs ValidationErrors

	if !IsSupportedAPIVersion(bundle.APIVersion) {
		errs = append(errs, FieldError{
			Path:    "apiVersion",
			Message: "must be " + APIVersionV1,
		})
	}
	if bundle.Kind != KindBundle {
		errs = append(errs, FieldError{
			Path:    "kind",
			Message: "must be " + KindBundle,
		})
	}

	if strings.TrimSpace(bundle.Metadata.Name) == "" {
		errs = append(errs, FieldError{Path: "metadata.name", Message: "is required"})
	}
	if strings.TrimSpace(bundle.Metadata.Namespace) == "" {
		errs = append(errs, FieldError{Path: "metadata.namespace", Message: "is required"})
	}

	root := strings.TrimSpace(bundle.Spec.Root)
	if root == "" {
		errs = append(errs, FieldError{Path: "spec.root", Message: "is required"})
	} else {
		errs = append(errs, validateBundleRelativePath(bundleRoot, root, "spec.root")...)
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

func validateBundleRelativePath(bundleRoot, ref, fieldPath string) []FieldError {
	ref = filepath.FromSlash(strings.TrimSpace(ref))
	if ref == "" {
		return []FieldError{{Path: fieldPath, Message: "is required"}}
	}
	if filepath.IsAbs(ref) {
		return []FieldError{{Path: fieldPath, Message: "must be bundle-relative"}}
	}
	if bundleRoot == "" {
		return nil
	}
	abs := filepath.Join(bundleRoot, ref)
	if !isPathWithinRoot(bundleRoot, abs) {
		return []FieldError{{Path: fieldPath, Message: "must resolve inside bundle root"}}
	}
	return nil
}
