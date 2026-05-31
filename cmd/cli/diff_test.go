package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDiffLines_lcsAvoidsCascade(t *testing.T) {
	from := []string{"a", "b", "c", "d"}
	to := []string{"a", "x", "b", "c", "d"}

	ops := diffLines(from, to)

	var inserts, deletes, equals int
	for _, op := range ops {
		switch op.kind {
		case opInsert:
			inserts++
			if op.text != "x" {
				t.Fatalf("unexpected inserted line %q", op.text)
			}
		case opDelete:
			deletes++
		case opEqual:
			equals++
		}
	}

	if inserts != 1 || deletes != 0 {
		t.Fatalf("got %d inserts / %d deletes, want a single insertion", inserts, deletes)
	}
	if equals != 4 {
		t.Fatalf("got %d equal lines, want 4 (no cascading changes)", equals)
	}
}

func TestPrettyCanonicalJSON_sortsKeysAndIndents(t *testing.T) {
	out, err := prettyCanonicalJSON([]byte(`{"b":1,"a":2}`))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"a\": 2,\n  \"b\": 1\n}"
	if string(out) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

func TestWriteManifestDiff_unifiedHunkNoColor(t *testing.T) {
	from := strings.Join([]string{"{", `  "name": "old"`, "}"}, "\n")
	to := strings.Join([]string{"{", `  "name": "new"`, "}"}, "\n")

	var buf bytes.Buffer
	writeManifestDiff(&buf, diffOptions{
		localLabel:  "./agent.yaml",
		remoteLabel: "demo/agent@1.0.0",
		from:        from,
		to:          to,
		color:       false,
	})
	got := buf.String()

	for _, want := range []string{
		"--- demo/agent@1.0.0 (published)",
		"+++ ./agent.yaml (local)",
		"@@ -1,3 +1,3 @@",
		`-   "name": "old"`,
		`+   "name": "new"`,
		"Summary: 1 addition, 1 deletion",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q\n---\n%s", want, got)
		}
	}

	if strings.Contains(got, "\x1b[") {
		t.Fatalf("color disabled but ANSI codes present:\n%s", got)
	}
}

func TestWriteManifestDiff_colorEmitsAnsi(t *testing.T) {
	var buf bytes.Buffer
	writeManifestDiff(&buf, diffOptions{
		localLabel:  "./agent.yaml",
		remoteLabel: "demo/agent@1.0.0",
		from:        "a",
		to:          "b",
		color:       true,
	})
	if !strings.Contains(buf.String(), "\x1b[") {
		t.Fatalf("expected ANSI color codes in output:\n%s", buf.String())
	}
}
