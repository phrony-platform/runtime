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
