package cliout

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestWriteVersionMismatchWarning_plainWhenNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var out bytes.Buffer
	if err := WriteVersionMismatchWarning(&out, "0.2.0", "0.3.0"); err != nil {
		t.Fatalf("WriteVersionMismatchWarning: %v", err)
	}
	s := out.String()
	if strings.Contains(s, "\x1b[") {
		t.Fatalf("expected plain output, got %q", s)
	}
	for _, want := range []string{"warning:", "0.2.0", "0.3.0", "phrony upgrade"} {
		if !strings.Contains(s, want) {
			t.Fatalf("output missing %q: %q", want, s)
		}
	}
}

func TestWriteVersionMismatchWarning_ansiWhenColorEnabled(t *testing.T) {
	s := formatVersionMismatchWarning(true, "0.2.0", "0.3.0")
	if !strings.Contains(s, "\x1b[1;33mwarning:") {
		t.Fatalf("expected bold yellow prefix, got %q", s)
	}
	if !strings.Contains(s, "\x1b[33m") {
		t.Fatalf("expected yellow body, got %q", s)
	}
}

func TestUseColor_respectsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if UseColor(os.Stderr) {
		t.Fatal("expected UseColor false when NO_COLOR is set")
	}
}
