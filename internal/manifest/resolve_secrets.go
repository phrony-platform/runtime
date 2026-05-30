package manifest

import (
	"fmt"
	"os"
	"strings"
)

// ResolveSecretsFromEnv reads each secrets.<name>.fromEnv variable from the process environment.
// Returns nil when the manifest has no secrets section.
func ResolveSecretsFromEnv(agent *Agent) (map[string][]byte, error) {
	if agent == nil || len(agent.Secrets) == 0 {
		return nil, nil
	}
	out := make(map[string][]byte, len(agent.Secrets))
	for name, def := range agent.Secrets {
		varName := strings.TrimSpace(def.FromEnv)
		val := strings.TrimSpace(os.Getenv(varName))
		if val == "" {
			return nil, fmt.Errorf(
				"secret %q: environment variable %s is not set; set %s and retry deploy",
				name, varName, varName,
			)
		}
		out[name] = []byte(val)
	}
	return out, nil
}

// UnsetSecretEnvVars lists fromEnv variables that are unset or empty in the current environment.
func UnsetSecretEnvVars(agent *Agent) []string {
	if agent == nil || len(agent.Secrets) == 0 {
		return nil
	}
	var unset []string
	for name, def := range agent.Secrets {
		varName := strings.TrimSpace(def.FromEnv)
		if strings.TrimSpace(os.Getenv(varName)) == "" {
			unset = append(unset, fmt.Sprintf("secret %q: %s", name, varName))
		}
	}
	return unset
}
