package cliout

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteTable_alignsColumns(t *testing.T) {
	var buf bytes.Buffer
	err := WriteTable(&buf,
		[]string{"ID", "NAME"},
		[][]string{
			{"abc", "short"},
			{"long-id", "name"},
		},
	)
	if err != nil {
		t.Fatalf("WriteTable: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want header + 2 rows", len(lines))
	}
	if !strings.HasPrefix(lines[1], "abc") {
		t.Fatalf("row1 = %q, want id column aligned", lines[1])
	}
}
