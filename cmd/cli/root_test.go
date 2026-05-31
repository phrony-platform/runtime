package main

import (
	"bytes"
	"strings"
	"testing"
)

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
