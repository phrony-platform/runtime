package cliout

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteStatus_containsLogoAndTable(t *testing.T) {
	var out bytes.Buffer
	err := WriteStatus(&out, StatusPanel{
		RuntimeAddr:    "127.0.0.1:7777",
		CLIVersion:     "1.2.3",
		RuntimeVersion: "0.0.1",
		SchemaVersion:  "1",
		Health:         "SERVING",
	})
	if err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}
	s := out.String()
	for _, want := range []string{
		"ÆÆÆÆÆ",
		"CLI", "1.2.3", "Schema meta", "● SERVING",
		"127.0.0.1:7777", "0.0.1",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("output missing %q:\n%s", want, s)
		}
	}
	for _, border := range []string{"╭", "╰", "├", "│", "·"} {
		if strings.Contains(s, border) {
			t.Fatalf("output contains border %q:\n%s", border, s)
		}
	}
}

func TestWriteStatus_tableAlignment(t *testing.T) {
	var out bytes.Buffer
	if err := WriteStatus(&out, StatusPanel{
		RuntimeAddr:    "127.0.0.1:7777",
		CLIVersion:     "1.2.3",
		RuntimeVersion: "0.0.1",
		SchemaVersion:  "1",
		Health:         "SERVING",
	}); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}
	want := "Runtime  Schema meta  1\n"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("output missing aligned row %q:\n%s", want, out.String())
	}
}

func TestWriteStatus_missingSchemaShowsDash(t *testing.T) {
	var out bytes.Buffer
	if err := WriteStatus(&out, StatusPanel{Health: "SERVING"}); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}
	if !strings.Contains(out.String(), "Schema meta") || !strings.Contains(out.String(), "—") {
		t.Fatalf("output = %q, want em dash for missing schema", out.String())
	}
}
