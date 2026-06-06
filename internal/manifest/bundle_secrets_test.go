package manifest

import (
	"strings"
	"testing"
)

func TestUnionBundleSecrets_mergesDistinctSecrets(t *testing.T) {
	members := []ClosureMember{
		{
			ChildName: "orchestrator",
			Resolved: &ResolvedAgent{Agent: &Agent{
				Secrets: map[string]SecretDefinition{
					"openai": {FromEnv: "OPENAI_API_KEY"},
				},
			}},
		},
		{
			ChildName: "specialist",
			Resolved: &ResolvedAgent{Agent: &Agent{
				Secrets: map[string]SecretDefinition{
					"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
				},
			}},
		},
	}
	union, err := UnionBundleSecrets(members)
	if err != nil {
		t.Fatalf("UnionBundleSecrets: %v", err)
	}
	if len(union) != 2 {
		t.Fatalf("union len = %d, want 2", len(union))
	}
	if union["openai"].FromEnv != "OPENAI_API_KEY" {
		t.Fatalf("openai fromEnv = %q", union["openai"].FromEnv)
	}
	if union["anthropic"].FromEnv != "ANTHROPIC_API_KEY" {
		t.Fatalf("anthropic fromEnv = %q", union["anthropic"].FromEnv)
	}
}

func TestUnionBundleSecrets_sameNameSameFromEnv(t *testing.T) {
	members := []ClosureMember{
		{
			ChildName: "orchestrator",
			Resolved: &ResolvedAgent{Agent: &Agent{
				Secrets: map[string]SecretDefinition{
					"openai": {FromEnv: "OPENAI_API_KEY"},
				},
			}},
		},
		{
			ChildName: "specialist",
			Resolved: &ResolvedAgent{Agent: &Agent{
				Secrets: map[string]SecretDefinition{
					"openai": {FromEnv: "OPENAI_API_KEY"},
				},
			}},
		},
	}
	union, err := UnionBundleSecrets(members)
	if err != nil {
		t.Fatalf("UnionBundleSecrets: %v", err)
	}
	if len(union) != 1 {
		t.Fatalf("union len = %d, want 1", len(union))
	}
}

func TestUnionBundleSecrets_conflictDifferentFromEnv(t *testing.T) {
	members := []ClosureMember{
		{
			ChildName: "orchestrator",
			Resolved: &ResolvedAgent{Agent: &Agent{
				Secrets: map[string]SecretDefinition{
					"openai": {FromEnv: "OPENAI_API_KEY"},
				},
			}},
		},
		{
			ChildName: "specialist",
			Resolved: &ResolvedAgent{Agent: &Agent{
				Secrets: map[string]SecretDefinition{
					"openai": {FromEnv: "OPENAI_API_KEY_DEV"},
				},
			}},
		},
	}
	_, err := UnionBundleSecrets(members)
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, `secret "openai"`) ||
		!strings.Contains(msg, `member "orchestrator"`) ||
		!strings.Contains(msg, `member "specialist"`) {
		t.Fatalf("error = %q, want conflict citing both members", msg)
	}
}

func TestResolveSecretsFromDefinitions_readsEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	got, err := ResolveSecretsFromDefinitions(map[string]SecretDefinition{
		"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
	})
	if err != nil {
		t.Fatalf("ResolveSecretsFromDefinitions: %v", err)
	}
	if string(got["anthropic"]) != "sk-test" {
		t.Fatalf("secret = %q, want sk-test", got["anthropic"])
	}
}

func TestBundleSecretDeclaredBy(t *testing.T) {
	members := []SecretMember{
		{ChildName: "orchestrator", Agent: &Agent{
			Secrets: map[string]SecretDefinition{"openai": {FromEnv: "OPENAI_API_KEY"}},
		}},
		{ChildName: "specialist", Agent: &Agent{
			Secrets: map[string]SecretDefinition{
				"openai":    {FromEnv: "OPENAI_API_KEY"},
				"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
			},
		}},
	}
	declared := BundleSecretDeclaredBy(members)
	if len(declared["openai"]) != 2 {
		t.Fatalf("openai declared_by = %v, want 2 members", declared["openai"])
	}
	if len(declared["anthropic"]) != 1 || declared["anthropic"][0] != "specialist" {
		t.Fatalf("anthropic declared_by = %v", declared["anthropic"])
	}
}
