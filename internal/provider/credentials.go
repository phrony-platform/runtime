package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/secrets"
	"github.com/phrony-platform/runtime/internal/store"
)

// APIKeyForModel decrypts the manifest secret bound to spec.model for a session.
func APIKeyForModel(
	ctx context.Context,
	enc *secrets.Encryptor,
	q *store.Queries,
	sessionID string,
	agent *manifest.Agent,
) (string, error) {
	if agent == nil {
		return "", fmt.Errorf("agent manifest is required")
	}
	if len(agent.Secrets) == 0 {
		return "", fmt.Errorf("agent has no secrets; declare secrets in the manifest and run with resolved values")
	}
	if enc == nil {
		return "", fmt.Errorf("secrets encryptor is not configured")
	}
	if q == nil {
		return "", fmt.Errorf("database is not configured")
	}

	secretName := manifest.ModelSecretName(agent.Spec.Model)
	if secretName == "" {
		return "", fmt.Errorf("spec.model secret name could not be resolved")
	}
	if _, ok := agent.Secrets[secretName]; !ok {
		return "", fmt.Errorf("secret %q is not defined in manifest secrets", secretName)
	}

	raw, err := enc.DecryptForSession(ctx, q, sessionID, secretName)
	if err != nil {
		return "", fmt.Errorf("decrypt secret %q: %w", secretName, err)
	}
	defer zeroBytes(raw)

	key := strings.TrimSpace(string(raw))
	if key == "" {
		return "", fmt.Errorf("secret %q is empty", secretName)
	}
	return key, nil
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func zeroString(s string) {
	if s == "" {
		return
	}
	b := []byte(s)
	zeroBytes(b)
}
