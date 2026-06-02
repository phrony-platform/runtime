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

func TestValidateCommand_requiresManifestArg(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"validate"})
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

func TestPublishCommand_requiresManifestArg(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"publish"})
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

func TestDeployCommand_requiresVersionedAgentRef(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"deploy", "demo/echo-agent"})
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
