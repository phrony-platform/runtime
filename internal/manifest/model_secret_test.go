package manifest

import "testing"

func TestModelSecretName(t *testing.T) {
	t.Run("explicit secret", func(t *testing.T) {
		got := ModelSecretName(ModelConfig{Provider: "anthropic", Secret: "my-key"})
		if got != "my-key" {
			t.Fatalf("ModelSecretName() = %q, want my-key", got)
		}
	})
	t.Run("defaults to provider", func(t *testing.T) {
		got := ModelSecretName(ModelConfig{Provider: "openai"})
		if got != "openai" {
			t.Fatalf("ModelSecretName() = %q, want openai", got)
		}
	})
}
