package manifest_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phrony-platform/runtime/internal/manifest"
)

func TestParseValidate_golden(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		file string
	}{
		{name: "full-agent", file: "full-agent.yaml"},
		{name: "minimal", file: "minimal.yaml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := readTestdata(t, tc.file)
			agent, err := manifest.Parse(data)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if err := manifest.Validate(agent); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			assertGoldenAgentFields(t, tc.name, agent)
		})
	}
}

func assertGoldenAgentFields(t *testing.T, name string, agent *manifest.Agent) {
	t.Helper()
	if agent.APIVersion != manifest.APIVersionV1 {
		t.Fatalf("apiVersion = %q, want %q", agent.APIVersion, manifest.APIVersionV1)
	}
	if agent.Kind != manifest.KindAgent {
		t.Fatalf("kind = %q, want %q", agent.Kind, manifest.KindAgent)
	}
	if agent.Metadata.Name != "echo-agent" {
		t.Fatalf("metadata.name = %q, want echo-agent", agent.Metadata.Name)
	}
	if agent.Metadata.Namespace != "demo" {
		t.Fatalf("metadata.namespace = %q, want demo", agent.Metadata.Namespace)
	}
	if agent.Metadata.Version != "1.2.0" {
		t.Fatalf("metadata.version = %q, want 1.2.0", agent.Metadata.Version)
	}
	if agent.Spec.Instructions.Ref != "prompts/system" {
		t.Fatalf("spec.instructions.ref = %q", agent.Spec.Instructions.Ref)
	}
	if agent.Spec.Model.Provider != "anthropic" {
		t.Fatalf("spec.model.provider = %q", agent.Spec.Model.Provider)
	}
	if name == "full-agent" {
		if agent.Metadata.Owner != "platform-team" {
			t.Fatalf("metadata.owner = %q", agent.Metadata.Owner)
		}
		if agent.Metadata.Labels["app"] != "echo" {
			t.Fatalf("metadata.labels = %v", agent.Metadata.Labels)
		}
		if agent.Spec.Instructions.Version != 2 {
			t.Fatalf("spec.instructions.version = %v, want 2", agent.Spec.Instructions.Version)
		}
		if agent.Spec.Model.Reasoning == nil || agent.Spec.Model.Reasoning.Effort != "low" {
			t.Fatalf("spec.model.reasoning = %+v", agent.Spec.Model.Reasoning)
		}
		if agent.Output == nil || agent.Output.Format != "json" {
			t.Fatalf("output = %+v", agent.Output)
		}
		if agent.Output.Schema == nil || agent.Output.Schema.Ref != "schemas/result" {
			t.Fatalf("output.schema = %+v", agent.Output.Schema)
		}
		if agent.Output.Schema.Version != 1 {
			t.Fatalf("output.schema.version = %v, want 1", agent.Output.Schema.Version)
		}
		if agent.Output.Strict == nil || !*agent.Output.Strict {
			t.Fatalf("output.strict = %v", agent.Output.Strict)
		}
		if agent.Output.OnInvalid != "retry" {
			t.Fatalf("output.on_invalid = %q", agent.Output.OnInvalid)
		}
	}
}

func TestValidate_negative(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		file    string
		wantSub string
	}{
		{
			name:    "instructions ref and text",
			file:    "invalid-instructions-both.yaml",
			wantSub: "spec.instructions",
		},
		{
			name:    "invalid semver",
			file:    "invalid-semver.yaml",
			wantSub: "metadata.version",
		},
		{
			name:    "temperature and top_p",
			file:    "invalid-temperature-top-p.yaml",
			wantSub: "spec.model.parameters",
		},
		{
			name:    "wrong apiVersion and kind",
			file:    "invalid-envelope.yaml",
			wantSub: "apiVersion",
		},
		{
			name:    "schema ref and inline",
			file:    "invalid-schema-both.yaml",
			wantSub: "output.schema",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			agent, err := manifest.Parse(readTestdata(t, tc.file))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			err = manifest.Validate(agent)
			if err == nil {
				t.Fatal("Validate() = nil, want error")
			}
			var valErrs manifest.ValidationErrors
			if !errors.As(err, &valErrs) {
				t.Fatalf("Validate() error type = %T, want ValidationErrors", err)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("Validate() = %v, want path containing %q", err, tc.wantSub)
			}
		})
	}
}

func TestParse_invalidYAML(t *testing.T) {
	t.Parallel()
	_, err := manifest.Parse([]byte("apiVersion: [\n"))
	if err == nil {
		t.Fatal("Parse() = nil, want error")
	}
}

func TestValidate_nilAgent(t *testing.T) {
	t.Parallel()
	err := manifest.Validate(nil)
	if err == nil {
		t.Fatal("Validate(nil) = nil, want error")
	}
}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
