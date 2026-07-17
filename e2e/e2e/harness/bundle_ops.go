//go:build integration

package harness

import (
	"testing"
	"time"
)

// PublishBundle publishes a locked scenario bundle and returns semver and lock hash.
func PublishBundle(t *testing.T, scenario string) (BundleMeta, string) {
	t.Helper()
	bundlePath := ScenarioBundleYAML(scenario)
	meta, err := ReadBundleMeta(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	Action(t, "bundle publish %s (%s)", scenario, meta.BundleRef())
	res := RunPhronyCLI(t, 2*time.Minute, nil, "bundles", "publish", bundlePath)
	lockHash, err := ScenarioBundleLockHash(scenario)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 {
		if !PhronyAlreadyExists(res) {
			t.Fatalf("bundle publish %s: exit=%d stderr=%s", scenario, res.ExitCode, res.Stderr)
		}
		Note(t, "bundle publish %s: version already published", meta.BundleRef())
	} else if semver, published, perr := ParseBundlePublishOutput(CombinedOutput(res)); perr == nil {
		meta.Version = semver
		lockHash = published
	}
	Result(t, "published %s@%s (%s)", meta.BundleRef(), meta.Version, lockHash)
	return meta, lockHash
}

// DeployBundle activates a published bundle version by semver.
func DeployBundle(t *testing.T, meta BundleMeta, lockHash string) {
	t.Helper()
	_ = lockHash
	ref := meta.BundleVersionRef()
	Action(t, "bundle deploy %s", ref)
	res := RunPhronyCLI(t, 2*time.Minute, nil, "bundles", "deploy", ref)
	if res.ExitCode != 0 {
		t.Fatalf("bundle deploy %s: exit=%d stderr=%s", ref, res.ExitCode, res.Stderr)
	}
	Result(t, "deployed %s", ref)
}

// PublishDeployBundle validates (implicit in publish), publishes, and deploys a scenario bundle.
func PublishDeployBundle(t *testing.T, scenario string) (BundleMeta, string) {
	t.Helper()
	meta, lockHash := PublishBundle(t, scenario)
	DeployBundle(t, meta, lockHash)
	return meta, lockHash
}

// RunBundleDetached starts a detached bundle session and returns the session id.
func RunBundleDetached(t *testing.T, meta BundleMeta, scenario, inputJSON string) string {
	t.Helper()
	ref := meta.BundleRef()
	args := []string{"bundles", "run", ref, "--input", inputJSON}
	Action(t, "detached bundle run %s input=%s", ref, inputJSON)
	res := RunPhronyCLI(t, 3*time.Minute, nil, args...)
	if res.ExitCode != 0 {
		t.Fatalf("bundle run %s: exit=%d stderr=%s stdout=%s", ref, res.ExitCode, res.Stderr, res.Stdout)
	}
	m := sessionStartedRE.FindStringSubmatch(res.Stdout)
	if len(m) < 2 {
		t.Fatalf("parse session id from stdout: %q", res.Stdout)
	}
	Result(t, "bundle session started: %s", m[1])
	return m[1]
}
