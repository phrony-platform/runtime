package manifest

import "testing"

func TestIsSupportedAPIVersion(t *testing.T) {
	t.Parallel()
	if !IsSupportedAPIVersion(APIVersionV1) {
		t.Fatal("canonical apiVersion should be supported")
	}
	if !IsSupportedAPIVersion(APIVersionV1Deprecated) {
		t.Fatal("deprecated apiVersion alias should be supported")
	}
	if IsSupportedAPIVersion("wrong/v0") {
		t.Fatal("unknown apiVersion should not be supported")
	}
}

func TestNormalizeAPIVersion(t *testing.T) {
	t.Parallel()
	agent := &Agent{APIVersion: APIVersionV1Deprecated}
	if !NormalizeAPIVersion(agent) {
		t.Fatal("NormalizeAPIVersion() = false, want true for deprecated alias")
	}
	if agent.APIVersion != APIVersionV1 {
		t.Fatalf("apiVersion = %q, want %q", agent.APIVersion, APIVersionV1)
	}
	if NormalizeAPIVersion(agent) {
		t.Fatal("second NormalizeAPIVersion() = true, want false")
	}
}

func TestValidate_deprecatedAPIVersionAccepted(t *testing.T) {
	t.Parallel()
	agent := validAgent()
	agent.APIVersion = APIVersionV1Deprecated
	if err := Validate(agent); err != nil {
		t.Fatalf("Validate() with deprecated apiVersion = %v", err)
	}
}
