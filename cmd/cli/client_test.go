package main

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/version"
	"google.golang.org/grpc"
)

type versionRuntimeClient struct {
	runtimev1.RuntimeClient
	version string
}

func (c *versionRuntimeClient) GetVersion(context.Context, *runtimev1.GetVersionRequest, ...grpc.CallOption) (*runtimev1.GetVersionResponse, error) {
	return &runtimev1.GetVersionResponse{Version: c.version}, nil
}

func TestWarnVersionMismatch_writesUpgradeHint(t *testing.T) {
	var stderr bytes.Buffer
	warnVersionMismatch(context.Background(), &stderr, &versionRuntimeClient{version: "9.9.9"})
	s := stderr.String()
	for _, want := range []string{"warning:", version.CLIVersion, "9.9.9", "phrony upgrade"} {
		if !strings.Contains(s, want) {
			t.Fatalf("stderr missing %q: %q", want, s)
		}
	}
}

func TestWarnVersionMismatch_silentWhenVersionsMatch(t *testing.T) {
	var stderr bytes.Buffer
	warnVersionMismatch(context.Background(), &stderr, &versionRuntimeClient{version: version.CLIVersion})
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
}

func TestUpgradeCommand_checkWhenUpToDate(t *testing.T) {
	testUpgradeIsDevBuild = func() bool { return false }
	testUpgradeLatestVersion = func(context.Context, *http.Client) (string, error) {
		return version.CLIVersion, nil
	}
	t.Cleanup(func() {
		testUpgradeIsDevBuild = nil
		testUpgradeLatestVersion = nil
	})

	var out bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"upgrade", "--check"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "up to date") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestUpgradeCommand_devBuildWarning(t *testing.T) {
	testUpgradeIsDevBuild = func() bool { return true }
	t.Cleanup(func() { testUpgradeIsDevBuild = nil })

	var stderr bytes.Buffer
	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&stderr)
	root.SetArgs([]string{"upgrade", "--check"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for dev build")
	}
	if !strings.Contains(stderr.String(), "make install-cli") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestUpgradeCommand_hasExpectedFlags(t *testing.T) {
	cmd := newUpgradeCommand()
	for _, name := range []string{"check", "version", "yes"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("missing flag %q", name)
		}
	}
}
