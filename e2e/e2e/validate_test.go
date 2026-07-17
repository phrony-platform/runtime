package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phrony-platform/runtime/e2e/e2e/harness"
)

func TestValidate_InvalidSemver(t *testing.T) {
	harness.BeginTest(t, "A1", "Local validate rejects invalid metadata.version semver", "phrony validate exits non-zero")
	path := harness.ScenarioAgentYAML("10-validate-invalid-semver")
	res := harness.RunPhronyCLI(t, 0, nil, "agents", "validate", path)
	if res.ExitCode == 0 {
		t.Fatalf("expected validate failure, got stdout=%q", res.Stdout)
	}
	harness.Result(t, "validate failed as expected (exit=%d)", res.ExitCode)
}

func TestValidate_DuplicateTools(t *testing.T) {
	harness.BeginTest(t, "A2", "Local validate rejects duplicate tool refs in spec.tools", "phrony validate exits non-zero")
	res := harness.RunPhronyCLI(t, 0, nil, "agents", "validate", harness.ScenarioAgentYAML("11-validate-duplicate-tools"))
	if res.ExitCode == 0 {
		t.Fatalf("expected validate failure, got stdout=%q", res.Stdout)
	}
	harness.Result(t, "validate failed as expected (exit=%d)", res.ExitCode)
}

func TestValidate_MissingPolicyRef(t *testing.T) {
	harness.BeginTest(t, "A3", "Local validate fails when default_policies references a missing policy file", "compile/validate exits non-zero")
	res := harness.RunPhronyCLI(t, 0, nil, "agents", "validate", harness.ScenarioAgentYAML("12-validate-missing-policy-ref"))
	if res.ExitCode == 0 {
		t.Fatalf("expected validate failure, got stdout=%q", res.Stdout)
	}
	harness.Result(t, "validate failed as expected (exit=%d)", res.ExitCode)
}

func TestValidate_InlinePoliciesForbidden(t *testing.T) {
	harness.BeginTest(t, "A4", "Local validate rejects inline spec.policies on Agent manifest", "phrony validate exits non-zero")
	res := harness.RunPhronyCLI(t, 0, nil, "agents", "validate", harness.ScenarioAgentYAML("13-validate-inline-policies-forbidden"))
	if res.ExitCode == 0 {
		t.Fatalf("expected validate failure, got stdout=%q", res.Stdout)
	}
	harness.Result(t, "validate failed as expected (exit=%d)", res.ExitCode)
}

func TestValidate_InvalidSideEffectClass(t *testing.T) {
	harness.BeginTest(t, "A6", "Local validate rejects invalid tool side_effect_class", "phrony validate exits non-zero")
	res := harness.RunPhronyCLI(t, 0, nil, "agents", "validate", harness.ScenarioAgentYAML("17-validate-invalid-side-effect-class"))
	if res.ExitCode == 0 {
		t.Fatalf("expected validate failure, got stdout=%q", res.Stdout)
	}
	combined := harness.CombinedOutput(res)
	if !strings.Contains(combined, "side_effect_class") {
		t.Fatalf("expected side_effect_class in output: %q", combined)
	}
	harness.Result(t, "validate failed as expected (exit=%d)", res.ExitCode)
}

func TestValidate_MissingSecretEnvWarns(t *testing.T) {
	harness.BeginTest(t, "A5", "Validate passes but warns when fromEnv secret is unset locally", "exit 0 with warning about OPENAI_API_KEY")
	t.Setenv("OPENAI_API_KEY", "")
	res := harness.RunPhronyCLI(t, 0, nil, "agents", "validate", harness.ScenarioAgentYAML("14-validate-missing-secret-env"))
	if res.ExitCode != 0 {
		t.Fatalf("expected validate success with warning, exit=%d stderr=%s", res.ExitCode, res.Stderr)
	}
	combined := harness.CombinedOutput(res)
	if !strings.Contains(combined, "OPENAI_API_KEY") && !strings.Contains(combined, "warning") {
		t.Fatalf("expected secret warning in output: %q", combined)
	}
	harness.Result(t, "validate succeeded with secret env warning")
}

func TestValidate_BaselineBundle(t *testing.T) {
	harness.BeginTest(t, "H2", "Baseline stub scenario bundle validates locally", "exit 0, valid: demo/payment-agent-baseline")
	res := harness.RunPhronyCLI(t, 0, nil, "agents", "validate", harness.ScenarioAgentYAML("00-baseline-hitl"))
	if res.ExitCode != 0 {
		t.Fatalf("validate baseline: exit=%d stderr=%s", res.ExitCode, res.Stderr)
	}
	harness.Result(t, "baseline bundle valid")
}

func TestValidate_BundleSecretConflict(t *testing.T) {
	harness.BeginTest(t, "J7", "Bundle lock fails when two members use the same secret name with different fromEnv", "exit non-zero with conflict message")
	res := harness.RunPhronyCLI(t, 0, nil, "bundles", "lock", harness.ScenarioBundleYAML("27-bundle-secrets-conflict"))
	if res.ExitCode == 0 {
		t.Fatalf("expected lock failure, got stdout=%q", res.Stdout)
	}
	combined := harness.CombinedOutput(res)
	if !strings.Contains(combined, `secret "openai"`) || !strings.Contains(combined, "OPENAI_API_KEY_DEV") {
		t.Fatalf("expected secret conflict in output: %q", combined)
	}
	harness.Result(t, "bundle secret conflict rejected at lock")
}

func TestValidate_BundleWithLock(t *testing.T) {
	harness.BeginTest(t, "J1", "Locked payment bundle validates locally", "exit 0, valid: demo/payment-desk")
	res := harness.ValidateBundle(t, "22-bundle-payment-auto", false)
	if res.ExitCode != 0 {
		t.Fatalf("bundle validate: exit=%d stderr=%s", res.ExitCode, res.Stderr)
	}
	combined := harness.CombinedOutput(res)
	if !strings.Contains(combined, "valid: demo/payment-desk") {
		t.Fatalf("expected valid bundle line, got: %q", combined)
	}
	harness.Result(t, "locked bundle valid")
}

func TestValidate_BundleRequireLockMissing(t *testing.T) {
	harness.BeginTest(t, "J2", "bundle validate --require-lock fails without bundle.lock.json", "exit non-zero, mentions bundle lock")
	res := harness.ValidateBundle(t, "25-validate-bundle-no-lock", true)
	if res.ExitCode == 0 {
		t.Fatalf("expected bundle validate failure, got stdout=%q", res.Stdout)
	}
	combined := harness.CombinedOutput(res)
	if !strings.Contains(combined, "phrony bundles lock") {
		t.Fatalf("expected bundle lock hint in output: %q", combined)
	}
	harness.Result(t, "require-lock rejected missing lockfile (exit=%d)", res.ExitCode)
}

func TestInit_ScaffoldAgent(t *testing.T) {
	harness.BeginTest(t, "H1", "phrony init scaffolds a minimal agent.yaml in a new directory", "agent.yaml created")
	target := t.TempDir()
	res := harness.RunPhronyCLI(t, 0, nil, "init", target)
	if res.ExitCode != 0 {
		t.Fatalf("init: exit=%d stderr=%s", res.ExitCode, res.Stderr)
	}
	manifest := filepath.Join(target, "agent.yaml")
	if _, err := os.Stat(manifest); err != nil {
		t.Fatalf("agent.yaml missing: %v", err)
	}
	harness.Result(t, "created %s", manifest)
}
