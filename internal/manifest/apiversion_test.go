package manifest

import "testing"

func TestIsSupportedAPIVersion(t *testing.T) {
	if !IsSupportedAPIVersion(APIVersionV1) {
		t.Fatal("phrony.com/v1 should be supported")
	}
	if IsSupportedAPIVersion("phrony.dev/v1") {
		t.Fatal("phrony.dev/v1 should not be supported")
	}
}

func TestValidate_rejectsDeprecatedAPIVersion(t *testing.T) {
	t.Parallel()
	agent := validAgent()
	agent.APIVersion = "phrony.dev/v1"
	if err := Validate(agent); err == nil {
		t.Fatal("Validate() = nil, want error for unsupported apiVersion")
	}
}
