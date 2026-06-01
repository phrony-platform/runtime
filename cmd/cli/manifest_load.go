package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/phrony-platform/runtime/internal/manifest"
)

func loadResolvedManifest(manifestPath string) (*manifest.ResolvedAgent, bool, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, false, fmt.Errorf("read manifest: %w", err)
	}

	agent, err := manifest.Parse(data)
	if err != nil {
		return nil, false, err
	}
	deprecated := manifest.NormalizeAPIVersion(agent)
	if err := manifest.Validate(agent); err != nil {
		// Preserve the full multi-field message for clierr.Format (ValidationErrors only unwraps the first field).
		return nil, deprecated, errors.New(err.Error())
	}

	resolved, err := manifest.Compile(manifestPath, agent)
	if err != nil {
		return nil, deprecated, err
	}
	return resolved, deprecated, nil
}
