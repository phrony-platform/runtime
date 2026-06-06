package main

import (
	"fmt"
	"os"

	"github.com/phrony-platform/runtime/internal/manifest"
	"gopkg.in/yaml.v3"
)

func readManifestKind(manifestPath string) (string, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", fmt.Errorf("read manifest: %w", err)
	}
	var header struct {
		Kind string `yaml:"kind"`
	}
	if err := yaml.Unmarshal(data, &header); err != nil {
		return "", fmt.Errorf("parse manifest: %w", err)
	}
	return header.Kind, nil
}

func isBundleManifestPath(manifestPath string) (bool, error) {
	kind, err := readManifestKind(manifestPath)
	if err != nil {
		return false, err
	}
	return kind == manifest.KindBundle, nil
}
