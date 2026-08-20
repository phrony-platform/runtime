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
	ModuleRoot = "github.com/phrony-platform/runtime"
	// ProxyBaseURL is the Go module proxy path for this module.
	ProxyBaseURL = "https://proxy.golang.org/github.com/phrony-platform/runtime"
)

var (
	moduleProxyBaseURL     = ProxyBaseURL
	githubLatestReleaseURL = "https://api.github.com/repos/phrony-platform/runtime/releases/latest"
)

type proxyLatestResponse struct {
	Version string `json:"Version"`
}

type goListModuleResponse struct {
	Version string `json:"Version"`
}

type githubReleaseResponse struct {
	TagName string `json:"tag_name"`
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
	// InstallRef overrides Version for go install (for example "latest").
	InstallRef string
	Runner     ExecRunner
}

// LatestVersion fetches the latest release label for upgrade checks.
// It prefers the GitHub release tag (matches CLIVersion), then go list @latest,
// then the public module proxy.
func LatestVersion(ctx context.Context, client *http.Client) (string, error) {
	return latestVersion(ctx, client, defaultRunner{})
}

func latestVersion(ctx context.Context, client *http.Client, runner ExecRunner) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if runner == nil {
		runner = defaultRunner{}
	}

	if version, err := latestVersionFromGitHub(ctx, client); err == nil {
		return version, nil
	}
	if _, err := runner.LookPath("go"); err == nil {
		if version, err := latestVersionFromGoList(runner); err == nil {
			return version, nil
		}
	}
	return latestVersionFromModuleProxy(ctx, client)
}

func latestVersionFromGitHub(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubLatestReleaseURL, nil)
	if err != nil {
		return "", fmt.Errorf("build GitHub request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "phrony-cli")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch GitHub release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub releases API returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read GitHub release response: %w", err)
	}
	var parsed githubReleaseResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("parse GitHub release response: %w", err)
	}
	version := strings.TrimSpace(parsed.TagName)
	if version == "" {
		return "", fmt.Errorf("GitHub release returned empty tag")
	}
	return StripVPrefix(version), nil
}

func latestVersionFromGoList(runner ExecRunner) (string, error) {
	out, err := runner.Output("go", "list", "-m", "-json", ModuleRoot+"@latest")
	if err != nil {
		return "", fmt.Errorf("go list @latest: %w", err)
	}
	var parsed goListModuleResponse
	if err := json.Unmarshal(out, &parsed); err != nil {
		return "", fmt.Errorf("parse go list response: %w", err)
	}
	version := strings.TrimSpace(parsed.Version)
	if version == "" {
		return "", fmt.Errorf("go list returned empty version")
	}
	return StripVPrefix(version), nil
}

func latestVersionFromModuleProxy(ctx context.Context, client *http.Client) (string, error) {
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
		return "", fmt.Errorf(
			"module proxy returned HTTP %d (reinstall manually: go install %s@latest)",
			resp.StatusCode,
			ModulePath,
		)
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

	ref := strings.TrimSpace(opts.InstallRef)
	if ref == "" {
		ref = installRef(opts.Version)
	}
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
