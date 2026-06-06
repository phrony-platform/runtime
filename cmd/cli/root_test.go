package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/phrony-platform/runtime/internal/version"
)

func TestRootCommand_versionFlag(t *testing.T) {
	want := "phrony version " + version.Version + "\n"
	for _, args := range [][]string{{"-v"}, {"--version"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out bytes.Buffer
			root := NewRootCommand()
			root.SetOut(&out)
			root.SetErr(&bytes.Buffer{})
			root.SetArgs(args)

			if err := root.Execute(); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if got := out.String(); got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestRootCommand_flatAgentCommandsRemoved(t *testing.T) {
	removed := []string{
		"validate",
		"publish",
		"versions",
		"deploy",
		"active",
		"history",
		"diff",
		"inspect",
		"deprecate",
		"retire",
	}
	for _, cmd := range removed {
		t.Run(cmd, func(t *testing.T) {
			root := NewRootCommand()
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			root.SetArgs([]string{cmd})

			err := root.Execute()
			if err == nil {
				t.Fatalf("expected error for removed flat command %q", cmd)
			}
			if !strings.Contains(err.Error(), "unknown command") {
				t.Fatalf("error = %v, want unknown command", err)
			}
		})
	}
}

func TestRootCommand_flatBundleCommandRemoved(t *testing.T) {
	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"bundle"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for removed bundle group, got nil")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("error = %v, want unknown command", err)
	}
}

func TestAgentValidateCommand_requiresManifestArg(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"agents", "validate"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "accepts 1 arg") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAgentPublishCommand_requiresManifestArg(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"agents", "publish"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "accepts 1 arg") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBundleValidateCommand_requiresBundleArg(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"bundles", "validate"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "accepts 1 arg") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAgentDeployCommand_requiresVersionedAgentRef(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"agents", "deploy", "demo/echo-agent"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "@version") {
		t.Fatalf("unexpected error: %v", err)
	}
}
