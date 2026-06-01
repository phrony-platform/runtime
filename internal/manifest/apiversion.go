package manifest

// Canonical and deprecated Agent Spec apiVersion values.
const (
	APIVersionV1           = "phrony.com/v1"
	APIVersionV1Deprecated = "phrony.dev/v1"
)

// IsSupportedAPIVersion reports whether v is the canonical or deprecated alias.
func IsSupportedAPIVersion(v string) bool {
	return v == APIVersionV1 || v == APIVersionV1Deprecated
}

// NormalizeAPIVersion rewrites the deprecated apiVersion alias to the canonical value.
// It returns true when the input used the deprecated alias.
func NormalizeAPIVersion(agent *Agent) bool {
	if agent == nil {
		return false
	}
	if agent.APIVersion == APIVersionV1Deprecated {
		agent.APIVersion = APIVersionV1
		return true
	}
	return false
}
