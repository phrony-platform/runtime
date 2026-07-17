package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// BundleMeta holds identity fields from a bundle.yaml.
type BundleMeta struct {
	Namespace string
	Name      string
	Version   string
}

// ReadBundleMeta parses metadata from a bundle manifest path.
func ReadBundleMeta(bundlePath string) (BundleMeta, error) {
	data, err := os.ReadFile(bundlePath)
	if err != nil {
		return BundleMeta{}, err
	}
	var doc struct {
		Metadata struct {
			Name      string `yaml:"name"`
			Namespace string `yaml:"namespace"`
			Version   string `yaml:"version"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return BundleMeta{}, fmt.Errorf("parse bundle metadata: %w", err)
	}
	meta := BundleMeta{
		Namespace: strings.TrimSpace(doc.Metadata.Namespace),
		Name:      strings.TrimSpace(doc.Metadata.Name),
		Version:   strings.TrimSpace(doc.Metadata.Version),
	}
	if meta.Namespace == "" || meta.Name == "" {
		return BundleMeta{}, fmt.Errorf("bundle metadata.name and metadata.namespace are required")
	}
	if meta.Version == "" {
		return BundleMeta{}, fmt.Errorf("bundle metadata.version is required")
	}
	return meta, nil
}

// BundleRef formats namespace/name.
func (m BundleMeta) BundleRef() string {
	return fmt.Sprintf("%s/%s", m.Namespace, m.Name)
}

// BundleVersionRef formats namespace/name@semver.
func (m BundleMeta) BundleVersionRef() string {
	return fmt.Sprintf("%s/%s@%s", m.Namespace, m.Name, m.Version)
}

var bundleLockHashRE = regexp.MustCompile(`sha256:[0-9a-f]{64}`)
var bundleSemverRE = regexp.MustCompile(`\b\d+\.\d+\.\d+\b`)

// LockBundle writes bundle.lock.json for a scenario directory.
func LockBundle(t *testing.T, scenario string) {
	t.Helper()
	path := ScenarioBundleYAML(scenario)
	Action(t, "bundle lock %s", scenario)
	res := RunPhronyCLI(t, 2*time.Minute, nil, "bundles", "lock", path)
	if res.ExitCode != 0 {
		t.Fatalf("bundle lock %s: exit=%d stderr=%s", scenario, res.ExitCode, res.Stderr)
	}
	Result(t, "locked %s", scenario)
}

// ValidateBundle runs phrony bundles validate on a scenario bundle.
func ValidateBundle(t *testing.T, scenario string, requireLock bool) PhronyResult {
	t.Helper()
	path := ScenarioBundleYAML(scenario)
	args := []string{"bundles", "validate", path}
	if requireLock {
		args = append(args, "--require-lock")
	}
	Action(t, "bundle validate %s require-lock=%v", scenario, requireLock)
	res := RunPhronyCLI(t, 2*time.Minute, nil, args...)
	return res
}

// ScenarioBundleLockHash reads the committed lock hash from a scenario's bundle.lock.json.
func ScenarioBundleLockHash(scenario string) (string, error) {
	data, err := os.ReadFile(filepath.Join(ScenarioDir(scenario), "bundle.lock.json"))
	if err != nil {
		return "", err
	}
	var lock struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return "", fmt.Errorf("parse bundle.lock.json: %w", err)
	}
	version := strings.TrimSpace(lock.Version)
	if version == "" {
		return "", fmt.Errorf("bundle.lock.json version is empty")
	}
	return version, nil
}

// ParseBundleLockHash extracts the lock hash from bundle publish or lock output.
func ParseBundleLockHash(output string) (string, error) {
	m := bundleLockHashRE.FindString(output)
	if m == "" {
		return "", fmt.Errorf("lock hash not found in output: %q", strings.TrimSpace(output))
	}
	return m, nil
}

// ParseBundlePublishOutput extracts semver and lock hash from bundle publish output.
func ParseBundlePublishOutput(output string) (semver, lockHash string, err error) {
	lockHash, err = ParseBundleLockHash(output)
	if err != nil {
		return "", "", err
	}
	m := bundleSemverRE.FindString(output)
	if m == "" {
		return "", "", fmt.Errorf("semver not found in output: %q", strings.TrimSpace(output))
	}
	return m, lockHash, nil
}
