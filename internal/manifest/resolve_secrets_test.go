package manifest

import (
	"strings"
	"testing"
)

func TestResolveSecretsFromEnv(t *testing.T) {
	agent := validAgent()
	agent.Secrets = map[string]SecretDefinition{
		"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
	}

	t.Setenv("ANTHROPIC_API_KEY", "sk-test")

	got, err := ResolveSecretsFromEnv(agent)
	if err != nil {
		t.Fatalf("ResolveSecretsFromEnv() = %v, want nil", err)
	}
	if string(got["anthropic"]) != "sk-test" {
		t.Fatalf("anthropic = %q, want sk-test", got["anthropic"])
	}
}

func TestResolveSecretsFromEnv_noSecrets(t *testing.T) {
	t.Parallel()
	got, err := ResolveSecretsFromEnv(validAgent())
	if err != nil {
		t.Fatalf("ResolveSecretsFromEnv() = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("got = %v, want nil", got)
	}
}

func TestResolveSecretsFromEnv_missingEnv(t *testing.T) {
	agent := validAgent()
	agent.Secrets = map[string]SecretDefinition{
		"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
	}
	t.Setenv("ANTHROPIC_API_KEY", "")

	_, err := ResolveSecretsFromEnv(agent)
	if err == nil {
		t.Fatal("ResolveSecretsFromEnv() = nil, want error")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Fatalf("error = %v, want env var name", err)
	}
	if !strings.Contains(err.Error(), "retry run") {
		t.Fatalf("error = %v, want deploy hint", err)
	}
}

func TestUnsetSecretEnvVars(t *testing.T) {
	agent := validAgent()
	agent.Secrets = map[string]SecretDefinition{
		"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
		"openai":    {FromEnv: "OPENAI_API_KEY"},
	}
	t.Setenv("ANTHROPIC_API_KEY", "sk-a")
	t.Setenv("OPENAI_API_KEY", "")

	unset := UnsetSecretEnvVars(agent)
	if len(unset) != 1 || !strings.Contains(unset[0], "OPENAI_API_KEY") {
		t.Fatalf("unset = %v, want only OPENAI_API_KEY", unset)
	}
}
