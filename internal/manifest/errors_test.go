package manifest

import (
	"errors"
	"strings"
	"testing"
)

func TestFieldError_Error(t *testing.T) {
	t.Parallel()
	if got := (FieldError{Path: "metadata.name", Message: "is required"}).Error(); got != "metadata.name: is required" {
		t.Fatalf("Error() = %q", got)
	}
	if got := (FieldError{Message: "manifest is nil"}).Error(); got != "manifest is nil" {
		t.Fatalf("Error() with empty path = %q", got)
	}
}

func TestValidationErrors_Error(t *testing.T) {
	t.Parallel()
	if got := (ValidationErrors{}).Error(); got != "invalid manifest" {
		t.Fatalf("empty Error() = %q", got)
	}
	if got := (ValidationErrors{{Path: "kind", Message: "must be Agent"}}).Error(); got != "kind: must be Agent" {
		t.Fatalf("single Error() = %q", got)
	}
	multi := ValidationErrors{
		{Path: "apiVersion", Message: "must be phrony.dev/v1"},
		{Path: "kind", Message: "must be Agent"},
	}
	got := multi.Error()
	for _, sub := range []string{"invalid manifest (2 errors)", "apiVersion", "kind"} {
		if !strings.Contains(got, sub) {
			t.Fatalf("multi Error() = %q, want substring %q", got, sub)
		}
	}
}

func TestValidationErrors_Unwrap(t *testing.T) {
	t.Parallel()
	if (ValidationErrors{}).Unwrap() != nil {
		t.Fatal("Unwrap(empty) should be nil")
	}
	first := FieldError{Path: "metadata.name", Message: "is required"}
	errs := ValidationErrors{first, {Path: "kind", Message: "must be Agent"}}
	if !errors.Is(errs.Unwrap(), first) {
		t.Fatalf("Unwrap() = %v, want %v", errs.Unwrap(), first)
	}
	var valErrs ValidationErrors
	if !errors.As(errs, &valErrs) {
		t.Fatal("errors.As should find ValidationErrors")
	}
}
