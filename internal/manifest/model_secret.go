package manifest

import "strings"

// ModelSecretName returns the secrets map key used for spec.model API credentials.
func ModelSecretName(m ModelConfig) string {
	if s := strings.TrimSpace(m.Secret); s != "" {
		return s
	}
	return strings.TrimSpace(m.Provider)
}
