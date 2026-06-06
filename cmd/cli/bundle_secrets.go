package main

import (
	"fmt"

	"github.com/phrony-platform/runtime/internal/envfile"
	"github.com/phrony-platform/runtime/internal/manifest"
)

func prepareBundleRunSecrets(fromBundle string, envFiles []string) (map[string][]byte, error) {
	if err := envfile.ApplyFiles(envFiles); err != nil {
		return nil, err
	}
	if fromBundle == "" {
		return nil, nil
	}
	resolved, err := loadResolvedBundle(fromBundle)
	if err != nil {
		return nil, err
	}
	var root *manifest.Agent
	for _, member := range resolved.Closure.Members {
		if member.IsRoot && member.Resolved != nil {
			root = member.Resolved.Agent
			break
		}
	}
	if root == nil {
		return nil, fmt.Errorf("bundle %q has no root member for secret resolution", fromBundle)
	}
	return manifest.ResolveSecretsFromEnv(root)
}
