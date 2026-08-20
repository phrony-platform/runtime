package cliupgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	ModulePath = "github.com/phrony-platform/runtime/cmd/cli"
	// ProxyBaseURL is the Go module proxy path for this module.
	ProxyBaseURL = "https://proxy.golang.org/github.com/phrony-platform/runtime"
)

var moduleProxyBaseURL = ProxyBaseURL

type proxyLatestResponse struct {
	Version string `json:"Version"`
}

// ExecRunner runs external commands; inject a fake in tests.
type ExecRunner interface {
	LookPath(file string) (string, error)
	Output(name string, args ...string) ([]byte, error)
	Run(name string, args ...string) error
}

type defaultRunner struct{}

func (defaultRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (defaultRunner) Output(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

func (defaultRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// InstallOptions configures a CLI self-upgrade install.
type InstallOptions struct {
	Version string
	Runner  ExecRunner
}

// LatestVersion fetches the latest release tag from the Go module proxy.
func LatestVersion(ctx context.Context, client *http.Client) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, moduleProxyBaseURL+"/@latest", nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch latest version: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("module proxy returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read module proxy response: %w", err)
	}
	var parsed proxyLatestResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("parse module proxy response: %w", err)
	}
	version := strings.TrimSpace(parsed.Version)
	if version == "" {
		return "", fmt.Errorf("module proxy returned empty version")
	}
	return StripVPrefix(version), nil
}

// NeedsUpgrade reports whether latest is newer than current using semver.
func NeedsUpgrade(current, latest string) bool {
	cur := canonicalSemver(current)
	lat := canonicalSemver(latest)
	if cur == "" || lat == "" {
		return current != latest
	}
	return semver.Compare(cur, lat) < 0
}

// Install builds and copies the phrony CLI to standard locations.
func Install(ctx context.Context, opts InstallOptions) error {
	_ = ctx
	if opts.Runner == nil {
		opts.Runner = defaultRunner{}
	}
	if _, err := opts.Runner.LookPath("go"); err != nil {
		return fmt.Errorf("go toolchain not found on PATH (required for phrony upgrade)")
	}

	gobin, err := resolveGOBIN(opts.Runner)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(gobin, 0o755); err != nil {
		return fmt.Errorf("create install dir %s: %w", gobin, err)
	}

	ref := installRef(opts.Version)
	dest := filepath.Join(gobin, "phrony")
	module := ModulePath + "@" + ref
	if err := opts.Runner.Run("go", "install", "-o", dest, module); err != nil {
		return fmt.Errorf("go install %s: %w", module, err)
	}

	localBin := filepath.Join(os.Getenv("HOME"), ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", localBin, err)
	}
	if err := copyFile(dest, filepath.Join(localBin, "phrony")); err != nil {
		return fmt.Errorf("copy to %s: %w", localBin, err)
	}

	if exe, err := os.Executable(); err == nil {
		exe, err = filepath.EvalSymlinks(exe)
		if err == nil {
			exeDir := filepath.Dir(exe)
			if !samePath(exeDir, gobin) && !samePath(exeDir, localBin) {
				target := filepath.Join(exeDir, "phrony")
				if err := copyFile(dest, target); err != nil {
					return fmt.Errorf("copy to %s: %w", target, err)
				}
			}
		}
	}
	return nil
}

// IsDevBuild reports whether the running binary was built with go run / go test.
func IsDevBuild() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return false
	}
	return strings.Contains(exe, "go-build")
}

func resolveGOBIN(runner ExecRunner) (string, error) {
	if gobin := strings.TrimSpace(os.Getenv("GOBIN")); gobin != "" {
		return gobin, nil
	}
	out, err := runner.Output("go", "env", "GOPATH")
	if err != nil {
		return "", fmt.Errorf("go env GOPATH: %w", err)
	}
	gopath := strings.TrimSpace(string(out))
	if gopath == "" {
		return "", fmt.Errorf("go env GOPATH returned empty path")
	}
	return filepath.Join(gopath, "bin"), nil
}

func installRef(version string) string {
	version = strings.TrimSpace(version)
	if version == "" || version == "latest" {
		return "latest"
	}
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}

func canonicalSemver(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return semver.Canonical(v)
}

// StripVPrefix removes a leading v from a semver tag.
func StripVPrefix(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func samePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	return a == b
}
