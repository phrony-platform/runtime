package cliupgrade

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLatestVersion_parsesProxyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/@v/latest" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"Version":"v0.3.0"}`)
	}))
	t.Cleanup(srv.Close)

	old := moduleProxyBaseURL
	moduleProxyBaseURL = srv.URL
	t.Cleanup(func() { moduleProxyBaseURL = old })

	got, err := LatestVersion(context.Background(), srv.Client())
	if err != nil {
		t.Fatalf("LatestVersion: %v", err)
	}
	if got != "0.3.0" {
		t.Fatalf("got %q, want 0.3.0", got)
	}
}

func TestNeedsUpgrade_semverCompare(t *testing.T) {
	if NeedsUpgrade("0.2.0", "0.2.0") {
		t.Fatal("same version should not need upgrade")
	}
	if !NeedsUpgrade("0.2.0", "0.3.0") {
		t.Fatal("older version should need upgrade")
	}
	if NeedsUpgrade("0.3.0", "0.2.0") {
		t.Fatal("newer current should not need upgrade")
	}
}

func TestInstallRef_addsVPrefix(t *testing.T) {
	if got := installRef("0.3.0"); got != "v0.3.0" {
		t.Fatalf("installRef(0.3.0) = %q", got)
	}
	if got := installRef("latest"); got != "latest" {
		t.Fatalf("installRef(latest) = %q", got)
	}
}

func TestInstall_runsGoInstallAndCopies(t *testing.T) {
	dir := t.TempDir()
	gobin := filepath.Join(dir, "gobin")
	localBin := filepath.Join(dir, ".local", "bin")
	t.Setenv("GOBIN", gobin)
	t.Setenv("HOME", dir)

	var runs [][]string
	runner := fakeRunner{
		lookPath: map[string]string{"go": "/usr/bin/go"},
		run: func(name string, args ...string) error {
			runs = append(runs, append([]string{name}, args...))
			return os.WriteFile(filepath.Join(gobin, "phrony"), []byte("binary"), 0o755)
		},
	}

	if err := Install(context.Background(), InstallOptions{
		Version: "0.3.0",
		Runner:  runner,
	}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	want := []string{"go", "install", "-o", filepath.Join(gobin, "phrony"), ModulePath + "@v0.3.0"}
	if strings.Join(runs[0], " ") != strings.Join(want, " ") {
		t.Fatalf("run = %v, want %v", runs[0], want)
	}
	if _, err := os.Stat(filepath.Join(localBin, "phrony")); err != nil {
		t.Fatalf("local copy missing: %v", err)
	}
}

type fakeRunner struct {
	lookPath map[string]string
	output   map[string]string
	run      func(name string, args ...string) error
}

func (f fakeRunner) LookPath(file string) (string, error) {
	if p, ok := f.lookPath[file]; ok {
		return p, nil
	}
	return "", fmt.Errorf("not found: %s", file)
}

func (f fakeRunner) Output(name string, args ...string) ([]byte, error) {
	key := strings.Join(append([]string{name}, args...), "\x00")
	if out, ok := f.output[key]; ok {
		return []byte(out), nil
	}
	return nil, fmt.Errorf("unexpected output: %s", key)
}

func (f fakeRunner) Run(name string, args ...string) error {
	if f.run != nil {
		return f.run(name, args...)
	}
	return nil
}
