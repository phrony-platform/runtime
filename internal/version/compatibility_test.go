package version

import (
	"strings"
	"testing"
)

func TestSameRelease_normalizesVersionPrefix(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.2.0", "v0.2.0", true},
		{"v0.2.0", "0.2.0", true},
		{"0.2.0", "0.3.0", false},
		{"", "", true},
		{"", "0.1.0", false},
	}
	for _, tc := range cases {
		if got := SameRelease(tc.a, tc.b); got != tc.want {
			t.Fatalf("SameRelease(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestVersionMismatchLines_containsUpgradeHint(t *testing.T) {
	lines := VersionMismatchLines("0.2.0", "0.3.0")
	if len(lines) != 4 {
		t.Fatalf("len(lines) = %d, want 4", len(lines))
	}
	if lines[0] != "warning: CLI version (0.2.0) does not match runtime version (0.3.0)." {
		t.Fatalf("first line = %q", lines[0])
	}
	for _, want := range []string{"phrony upgrade", "phrony-runtime@latest"} {
		found := false
		for _, line := range lines {
			if strings.Contains(line, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("lines missing %q: %v", want, lines)
		}
	}
}
