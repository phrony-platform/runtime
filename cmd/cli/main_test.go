package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRun_printsFormattedError(t *testing.T) {
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	os.Stderr = w

	outCh := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		outCh <- buf.String()
	}()

	code := run([]string{"status", "--runtime-addr", "127.0.0.1:1"})
	_ = w.Close()
	os.Stderr = oldStderr
	out := <-outCh

	if code != 1 {
		t.Fatalf("run() = %d, want 1", code)
	}
	if !strings.HasPrefix(out, "Error: ") {
		t.Fatalf("stderr = %q, want Error: prefix", out)
	}
	if strings.Contains(out, "level=ERROR") {
		t.Fatalf("stderr = %q, want plain text not slog", out)
	}
}
