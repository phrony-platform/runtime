package core

import (
	"testing"

	"github.com/phrony-platform/runtime/internal/secrets"
)

func TestNewServer_invalidEncryptionKey(t *testing.T) {
	t.Setenv(secrets.EnvEncryptionKey, "not-a-valid-key")
	_, err := NewServer(testServeDB(t))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
