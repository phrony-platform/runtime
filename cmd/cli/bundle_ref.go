package main

import (
	"fmt"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
)

func parseBundleRef(s string) (*runtimev1.BundleRef, error) {
	ns, name, version, err := agentref.ParseRef(s)
	if err != nil {
		return nil, err
	}
	return &runtimev1.BundleRef{
		Namespace: ns,
		Name:      name,
		Version:   version,
	}, nil
}

func parseBundleRefVersionRequired(s string) (*runtimev1.BundleRef, error) {
	ref, err := parseBundleRef(s)
	if err != nil {
		return nil, err
	}
	if ref.GetVersion() == "" {
		return nil, fmt.Errorf("bundle reference must include @version (semver or lock hash), got %q", s)
	}
	return ref, nil
}
