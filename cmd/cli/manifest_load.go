package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/phrony-platform/runtime/internal/manifest"
)

func loadResolvedManifest(manifestPath string) (*manifest.ResolvedAgent, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	agent, err := manifest.Parse(data)
	if err != nil {
		return nil, err
	}
	if err := manifest.Validate(agent); err != nil {
		// Preserve the full multi-field message for clierr.Format (ValidationErrors only unwraps the first field).
		return nil, errors.New(err.Error())
	}

	resolved, err := manifest.ResolveBundle(manifestPath, agent)
	if err != nil {
		return nil, err
	}
	return resolved, nil
}
