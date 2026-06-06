package agentref

import (
	"fmt"
	"strings"
	"testing"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestParse_valid(t *testing.T) {
	ns, name, err := Parse("demo/echo-agent")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if ns != "demo" || name != "echo-agent" {
		t.Fatalf("got %q/%q, want demo/echo-agent", ns, name)
	}
}

func TestParse_errors(t *testing.T) {
	tests := []string{
		"",
		"echo-agent",
		"/echo",
		"demo/",
	}
	for _, in := range tests {
		t.Run(in, func(t *testing.T) {
			_, _, err := Parse(in)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), "namespace/name") {
				t.Fatalf("error %q, want namespace/name", err.Error())
			}
		})
	}
}

func TestFormat(t *testing.T) {
	tests := []struct {
		ns, name, want string
	}{
		{"demo", "echo", "demo/echo"},
		{"", "echo", "echo"},
		{"demo", "", "demo/"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			if got := Format(tc.ns, tc.name); got != tc.want {
				t.Fatalf("Format(%q, %q) = %q, want %q", tc.ns, tc.name, got, tc.want)
			}
		})
	}
}

func TestFromProto_valid(t *testing.T) {
	ns, name, err := FromProto(&runtimev1.AgentRef{Namespace: "demo", Name: "echo"})
	if err != nil {
		t.Fatalf("FromProto: %v", err)
	}
	if ns != "demo" || name != "echo" {
		t.Fatalf("got %q/%q, want demo/echo", ns, name)
	}
}

func TestParseRef_withVersion(t *testing.T) {
	ns, name, ver, err := ParseRef("demo/echo-agent@1.2.0")
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}
	if ns != "demo" || name != "echo-agent" || ver != "1.2.0" {
		t.Fatalf("got %q/%q@%q, want demo/echo-agent@1.2.0", ns, name, ver)
	}
}

func TestParseRef_withoutVersion(t *testing.T) {
	ns, name, ver, err := ParseRef("demo/echo-agent")
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}
	if ns != "demo" || name != "echo-agent" || ver != "" {
		t.Fatalf("got %q/%q@%q, want demo/echo-agent with empty version", ns, name, ver)
	}
}

func TestParseRef_emptyVersion(t *testing.T) {
	_, _, _, err := ParseRef("demo/echo-agent@")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestIsLockHashVersion(t *testing.T) {
	if !IsLockHashVersion("sha256:abc") {
		t.Fatal("expected lock hash version")
	}
	if IsLockHashVersion("1.0.0") {
		t.Fatal("semver should not be treated as lock hash")
	}
}

func TestFormatVersioned(t *testing.T) {
	if got := FormatVersioned("demo", "echo", "1.0.0"); got != "demo/echo@1.0.0" {
		t.Fatalf("FormatVersioned = %q, want demo/echo@1.0.0", got)
	}
	if got := FormatVersioned("demo", "echo", ""); got != "demo/echo" {
		t.Fatalf("FormatVersioned = %q, want demo/echo", got)
	}
}

func TestFromProto_errors(t *testing.T) {
	tests := []*runtimev1.AgentRef{
		nil,
		{},
		{Namespace: "demo"},
		{Name: "echo"},
		{Namespace: "", Name: "echo"},
	}
	for i, ref := range tests {
		t.Run(fmt.Sprintf("case%d", i), func(t *testing.T) {
			_, _, err := FromProto(ref)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			st, ok := status.FromError(err)
			if !ok {
				t.Fatalf("expected gRPC status, got %T: %v", err, err)
			}
			if st.Code() != codes.InvalidArgument {
				t.Fatalf("code = %v, want InvalidArgument", st.Code())
			}
			if !strings.Contains(st.Message(), "namespace and name") {
				t.Fatalf("message %q, want namespace and name", st.Message())
			}
		})
	}
}
