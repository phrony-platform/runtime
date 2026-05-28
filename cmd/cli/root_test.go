package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunCommand_requiresSessionID(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"run"})
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

func TestDeployCommand_requiresFileFlag(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"deploy"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "required flag") {
		t.Fatalf("unexpected error: %v", err)
	}
}
