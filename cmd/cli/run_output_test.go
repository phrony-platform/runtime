package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompletionWriter_plainTextStreams(t *testing.T) {
	var out bytes.Buffer
	w := newCompletionWriter(&out)
	if err := w.WriteDelta("Hi"); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteDelta("!"); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if out.String() != "Hi!" {
		t.Fatalf("out = %q, want streamed plain text", out.String())
	}
}

func TestCompletionWriter_jsonBufferedAndPrettified(t *testing.T) {
	var out bytes.Buffer
	w := newCompletionWriter(&out)
	compact := `{"reply":"hello","refused":false}`
	if err := w.WriteDelta(compact); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("out = %q, want no output before flush", out.String())
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(out.String())
	if !strings.Contains(got, "\n  \"reply\": \"hello\"") {
		t.Fatalf("out = %q, want indented JSON", got)
	}
}

func TestPrettifySessionOutput_nestedMessageJSON(t *testing.T) {
	raw := []byte(`{"message":"{\"reply\":\"hi\"}","stop_reason":"end_turn"}`)
	pretty, err := prettifySessionOutput(raw)
	if err != nil {
		t.Fatal(err)
	}
	got := string(pretty)
	if !strings.Contains(got, "\n  \"message\": {") {
		t.Fatalf("out = %q, want nested message object", got)
	}
	if !strings.Contains(got, "\n    \"reply\": \"hi\"") {
		t.Fatalf("out = %q, want indented reply field", got)
	}
}
