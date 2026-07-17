package harness

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// PhronyBin returns the default phrony CLI path (../bin/phrony from this module).
// RunPhrony resolves PHRONY_BIN only when that file exists; otherwise it uses
// this path or falls back to go run ./cmd/cli in the runtime repo.
func PhronyBin() string {
	return filepath.Join(PlaygroundRoot(), "..", "bin", "phrony")
}

// RuntimeAddr returns PHRONY_RUNTIME_ADDR or 127.0.0.1:7777.
func RuntimeAddr() string {
	if v := strings.TrimSpace(os.Getenv("PHRONY_RUNTIME_ADDR")); v != "" {
		return v
	}
	return "127.0.0.1:7777"
}

// PlaygroundRoot is the runtime/e2e module root (sibling of scenarios/, cmd/).
func PlaygroundRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// ScenariosRoot returns scenarios/.
func ScenariosRoot() string {
	return filepath.Join(PlaygroundRoot(), "scenarios")
}

// ScenarioDir returns the absolute path for a scenario directory name.
func ScenarioDir(name string) string {
	return filepath.Join(ScenariosRoot(), name)
}

// ScenarioAgentYAML returns agent.yaml inside a scenario directory.
func ScenarioAgentYAML(scenario string) string {
	return filepath.Join(ScenarioDir(scenario), "agent.yaml")
}

// ScenarioBundleYAML returns bundle.yaml inside a scenario directory.
func ScenarioBundleYAML(scenario string) string {
	return filepath.Join(ScenarioDir(scenario), "bundle.yaml")
}

// ScenarioManifest returns a manifest path relative to a scenario directory.
func ScenarioManifest(scenario, rel string) string {
	return filepath.Join(ScenarioDir(scenario), rel)
}
