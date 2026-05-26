package common

import (
	"errors"
	"testing"

	"github.com/go-playground/validator/v10"
)

func TestLoadSettings_missingDatabaseURL(t *testing.T) {
	t.Setenv(envDatabaseURL, "")
	t.Setenv(envGRPCAddr, "")
	t.Setenv(envRuntimeAddr, "")

	_, err := LoadSettings()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var verrs validator.ValidationErrors
	if !errors.As(err, &verrs) {
		t.Fatalf("expected validator.ValidationErrors, got %v", err)
	}
}

func TestLoadSettings_defaults(t *testing.T) {
	t.Setenv(envDatabaseURL, "postgres://example")
	t.Setenv(envGRPCAddr, "")
	t.Setenv(envRuntimeAddr, "")

	settings, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if settings.DatabaseURL != "postgres://example" {
		t.Fatalf("DatabaseURL = %q, want postgres://example", settings.DatabaseURL)
	}
	if settings.GRPCAddr != defaultGRPCAddr {
		t.Fatalf("GRPCAddr = %q, want %q", settings.GRPCAddr, defaultGRPCAddr)
	}
	if settings.RuntimeAddr != defaultRuntimeAddr {
		t.Fatalf("RuntimeAddr = %q, want %q", settings.RuntimeAddr, defaultRuntimeAddr)
	}
}
