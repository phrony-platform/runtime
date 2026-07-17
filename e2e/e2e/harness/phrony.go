package harness

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// PhronyResult is the outcome of a phrony CLI invocation.
type PhronyResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func phronyCommand(ctx context.Context, args ...string) *exec.Cmd {
	if ctx == nil {
		ctx = context.Background()
	}
	if bin, ok := resolvePhronyBinary(); ok {
		return exec.CommandContext(ctx, bin, args...)
	}
	runtimeRoot := filepath.Join(PlaygroundRoot(), "..")
	cmd := exec.CommandContext(ctx, "go", append([]string{"run", "./cmd/cli"}, args...)...)
	cmd.Dir = runtimeRoot
	return cmd
}

// resolvePhronyBinary returns an on-disk phrony binary when one exists.
// PHRONY_BIN is used only when that path is present; otherwise the default
// ../bin/phrony is tried. Missing paths fall through to go run.
func resolvePhronyBinary() (string, bool) {
	var candidates []string
	if v := strings.TrimSpace(os.Getenv("PHRONY_BIN")); v != "" {
		candidates = append(candidates, v)
	}
	candidates = append(candidates, PhronyBin())
	for _, p := range candidates {
		if phronyBinaryExists(p) {
			return p, true
		}
	}
	return "", false
}

func phronyBinaryExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// RunPhrony executes phrony with args and optional extra environment variables.
func RunPhrony(ctx context.Context, extraEnv []string, args ...string) PhronyResult {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := phronyCommand(ctx, args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.Env = append(cmd.Env, "PHRONY_RUNTIME_ADDR="+RuntimeAddr())

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	res := PhronyResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if err == nil {
		return res
	}
	if exit, ok := err.(*exec.ExitError); ok {
		res.ExitCode = exit.ExitCode()
	} else {
		res.ExitCode = -1
		res.Stderr = strings.TrimSpace(res.Stderr + "\n" + err.Error())
	}
	return res
}

// RunPhronyTimeout runs phrony with a deadline.
func RunPhronyTimeout(timeout time.Duration, extraEnv []string, args ...string) PhronyResult {
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return RunPhrony(ctx, extraEnv, args...)
}

// RunPhronyCLI runs phrony and logs the command plus outcome (use from tests with -v).
func RunPhronyCLI(t *testing.T, timeout time.Duration, extraEnv []string, args ...string) PhronyResult {
	t.Helper()
	Action(t, "phrony %s", strings.Join(args, " "))
	res := RunPhronyTimeout(timeout, extraEnv, args...)
	LogPhronyResult(t, res)
	return res
}

// CombinedOutput returns stdout and stderr together.
func CombinedOutput(res PhronyResult) string {
	return strings.TrimSpace(res.Stdout + "\n" + res.Stderr)
}

// PhronyAlreadyExists reports whether phrony failed because the version already exists.
func PhronyAlreadyExists(res PhronyResult) bool {
	return strings.Contains(CombinedOutput(res), "(AlreadyExists)")
}

// PhronyAgentArchived reports whether publish failed because the agent is archived.
func PhronyAgentArchived(res PhronyResult) bool {
	return strings.Contains(CombinedOutput(res), "is archived and cannot accept new versions")
}

// PhronyVersionRetired reports whether deploy failed because the version is retired.
func PhronyVersionRetired(res PhronyResult) bool {
	return strings.Contains(CombinedOutput(res), "is retired and cannot be deployed")
}
