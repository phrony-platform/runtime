package main

import (
	"fmt"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
)

func parseAgentRef(s string) (*runtimev1.AgentRef, error) {
	ns, name, version, err := agentref.ParseRef(s)
	if err != nil {
		return nil, err
	}
	return &runtimev1.AgentRef{
		Namespace: ns,
		Name:      name,
		Version:   version,
	}, nil
}

func parseAgentRefVersionRequired(s string) (*runtimev1.AgentRef, error) {
	ref, err := parseAgentRef(s)
	if err != nil {
		return nil, err
	}
	if ref.GetVersion() == "" {
		return nil, fmt.Errorf("agent reference must include @version, got %q", s)
	}
	return ref, nil
}
