package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestApprovalsCommand_registered(t *testing.T) {
	root := NewRootCommand()
	for _, sub := range []string{"list", "show", "approve", "reject"} {
		if _, _, err := root.Find([]string{"approvals", sub}); err != nil {
			t.Fatalf("approvals %s: %v", sub, err)
		}
	}
}

func TestApprovalsList_outputsTable(t *testing.T) {
	addr := startTestRuntimeAddrForApprovalsList(t)
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"approvals", "list", "--runtime-addr", addr})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	text := out.String()
	for _, want := range []string{"appr-1", "pending", "sess-1", "payments.charge", "ops"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
}

func TestApprovalsShow_outputsDetail(t *testing.T) {
	addr := startTestRuntimeAddrForApprovalsShow(t)
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"approvals", "show", "appr-1", "--runtime-addr", addr})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	text := out.String()
	for _, want := range []string{
		"id: appr-1",
		"status: pending",
		"tool: payments.charge@1.0.0",
		"comprehension_required: true",
		`args: {"amount":100}`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
}
