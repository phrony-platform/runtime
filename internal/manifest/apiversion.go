package manifest

// APIVersionV1 is the supported Agent Spec apiVersion.
const APIVersionV1 = "phrony.com/v1"

// IsSupportedAPIVersion reports whether v is the canonical apiVersion.
func IsSupportedAPIVersion(v string) bool {
	return v == APIVersionV1
}
