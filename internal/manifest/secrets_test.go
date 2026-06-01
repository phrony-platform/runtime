package manifest

import (
	"strings"
	"testing"
)

func TestParse_secretsRejectPlaintext(t *testing.T) {
	t.Parallel()
	yaml := `
apiVersion: phrony.com/v1
kind: Agent
metadata:
  name: a
  namespace: ns
  version: 1.0.0
secrets:
  anthropic:
    value: sk-secret
spec:
  purpose: p
  instructions:
    text: hi
  model:
    provider: anthropic
    name: claude
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("Parse() = nil, want error for plaintext value field")
	}
	if !strings.Contains(err.Error(), "value") {
		t.Fatalf("error = %v, want value rejection", err)
	}
}

func TestValidate_secrets(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		mutate    func(*Agent)
		wantPaths []string
	}{
		{
			name: "valid secrets with explicit model secret",
			mutate: func(a *Agent) {
				a.Secrets = map[string]SecretDefinition{
					"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
				}
				a.Spec.Model.Secret = "anthropic"
			},
			wantPaths: nil,
		},
		{
			name: "valid secrets default to provider name",
			mutate: func(a *Agent) {
				a.Secrets = map[string]SecretDefinition{
					"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
				}
			},
			wantPaths: nil,
		},
		{
			name: "missing fromEnv",
			mutate: func(a *Agent) {
				a.Secrets = map[string]SecretDefinition{
					"anthropic": {},
				}
			},
			wantPaths: []string{"secrets.anthropic"},
		},
		{
			name: "invalid secret name",
			mutate: func(a *Agent) {
				a.Secrets = map[string]SecretDefinition{
					"Anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
				}
			},
			wantPaths: []string{"secrets.Anthropic"},
		},
		{
			name: "model secret without secrets block",
			mutate: func(a *Agent) {
				a.Spec.Model.Secret = "anthropic"
			},
			wantPaths: []string{"spec.model.secret"},
		},
		{
			name: "unknown model secret ref",
			mutate: func(a *Agent) {
				a.Secrets = map[string]SecretDefinition{
					"openai": {FromEnv: "OPENAI_API_KEY"},
				}
				a.Spec.Model.Provider = "anthropic"
				a.Spec.Model.Secret = "anthropic"
			},
			wantPaths: []string{"spec.model.secret"},
		},
		{
			name: "secrets set but no default for provider",
			mutate: func(a *Agent) {
				a.Secrets = map[string]SecretDefinition{
					"openai": {FromEnv: "OPENAI_API_KEY"},
				}
				a.Spec.Model.Provider = "anthropic"
			},
			wantPaths: []string{"spec.model.secret"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			agent := validAgent()
			tc.mutate(agent)
			err := Validate(agent)
			if len(tc.wantPaths) == 0 {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Validate() = nil, want error")
			}
			valErrs, ok := err.(ValidationErrors)
			if !ok {
				t.Fatalf("error type %T, want ValidationErrors", err)
			}
			for _, path := range tc.wantPaths {
				if !pathInErrors(valErrs, path) {
					t.Fatalf("missing path %q in %v", path, valErrs)
				}
			}
		})
	}
}

func TestParseJSON_secretsRejectPlaintext(t *testing.T) {
	t.Parallel()
	data := []byte(`{
  "apiVersion": "phrony.com/v1",
  "kind": "Agent",
  "metadata": {"name":"a","namespace":"ns","version":"1.0.0"},
  "secrets": {"anthropic": {"plaintext": "sk"}},
  "spec": {
    "purpose": "p",
    "instructions": {"text": "hi"},
    "model": {"provider": "anthropic", "name": "claude"}
  }
}`)
	_, err := ParseJSON(data)
	if err == nil {
		t.Fatal("ParseJSON() = nil, want error")
	}
	if !strings.Contains(err.Error(), "plaintext") {
		t.Fatalf("error = %v, want plaintext rejection", err)
	}
}

func TestCloneAgent_copiesSecrets(t *testing.T) {
	t.Parallel()
	agent := validAgent()
	agent.Secrets = map[string]SecretDefinition{
		"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
	}
	cloned := cloneAgent(agent)
	if cloned.Secrets["anthropic"].FromEnv != "ANTHROPIC_API_KEY" {
		t.Fatalf("secrets = %+v", cloned.Secrets)
	}
	cloned.Secrets["anthropic"] = SecretDefinition{FromEnv: "OTHER"}
	if agent.Secrets["anthropic"].FromEnv != "ANTHROPIC_API_KEY" {
		t.Fatal("secrets map not copied")
	}
}
