// Package agentref parses and formats agent namespace/name references.
//
// Canonical string form is "namespace/name" when both are non-empty. If namespace
// is empty, Format returns name only (manifests may omit namespace).
package agentref

import (
	"fmt"
	"strings"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Parse splits a CLI-style agent reference "namespace/name".
func Parse(s string) (namespace, name string, err error) {
	namespace, name, ok := strings.Cut(s, "/")
	if !ok || namespace == "" || name == "" {
		return "", "", fmt.Errorf("agent must be namespace/name, got %q", s)
	}
	return namespace, name, nil
}

// ParseRef splits "namespace/name" or "namespace/name@version".
func ParseRef(s string) (namespace, name, version string, err error) {
	agentPart, version, hasVersion := strings.Cut(s, "@")
	if hasVersion && version == "" {
		return "", "", "", fmt.Errorf("agent version must not be empty after @ in %q", s)
	}
	namespace, name, err = Parse(agentPart)
	if err != nil {
		return "", "", "", err
	}
	return namespace, name, version, nil
}

// FormatVersioned returns "namespace/name@version" when version is set.
func FormatVersioned(namespace, name, version string) string {
	base := Format(namespace, name)
	if version == "" {
		return base
	}
	return base + "@" + version
}

// IsLockHashVersion reports whether label is a bundle lock hash reference (sha256:…).
func IsLockHashVersion(label string) bool {
	return strings.HasPrefix(label, "sha256:")
}

// Format returns the canonical string form of an agent reference.
func Format(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}

// FromProto extracts namespace and name from a gRPC AgentRef.
func FromProto(ref *runtimev1.AgentRef) (namespace, name string, err error) {
	if ref == nil || ref.GetNamespace() == "" || ref.GetName() == "" {
		return "", "", status.Error(codes.InvalidArgument, "agent_ref requires namespace and name")
	}
	return ref.GetNamespace(), ref.GetName(), nil
}
