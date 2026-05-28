package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDeployCommand_requiresManifestArg(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"deploy"})
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
