package secrets

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestEncryptor_roundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{0xab}, aesKeySize)
	enc, err := NewEncryptor(key, 1)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	sealed, err := enc.Encrypt("version-id", "anthropic", []byte("sk-test"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if sealed.KeyVersion != 1 {
		t.Fatalf("key_version = %d, want 1", sealed.KeyVersion)
	}

	plain, err := enc.Decrypt("version-id", "anthropic", sealed)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(plain) != "sk-test" {
		t.Fatalf("plaintext = %q, want sk-test", plain)
	}
}

func TestEncryptor_associatedDataBinding(t *testing.T) {
	key := bytes.Repeat([]byte{0xcd}, aesKeySize)
	enc, err := NewEncryptor(key, 1)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	sealed, err := enc.Encrypt("version-a", "openai", []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := enc.Decrypt("version-b", "openai", sealed); err == nil {
		t.Fatal("Decrypt with wrong version id: want error")
	}
	if _, err := enc.Decrypt("version-a", "anthropic", sealed); err == nil {
		t.Fatal("Decrypt with wrong secret name: want error")
	}
}

func TestNewEncryptorFromEncoded_base64(t *testing.T) {
	key := bytes.Repeat([]byte{0x01}, aesKeySize)
	encoded := base64.StdEncoding.EncodeToString(key)

	enc, err := NewEncryptorFromEncoded(encoded)
	if err != nil {
		t.Fatalf("NewEncryptorFromEncoded: %v", err)
	}
	sealed, err := enc.Encrypt("v", "s", []byte("x"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := enc.Decrypt("v", "s", sealed); err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
}

func TestNewEncryptorFromEncoded_hex(t *testing.T) {
	key := bytes.Repeat([]byte{0x02}, aesKeySize)
	encoded := hex.EncodeToString(key)

	enc, err := NewEncryptorFromEncoded(encoded)
	if err != nil {
		t.Fatalf("NewEncryptorFromEncoded: %v", err)
	}
	if enc == nil {
		t.Fatal("expected encryptor")
	}
}

func TestNewEncryptorFromEnv_unset(t *testing.T) {
	t.Setenv(EnvEncryptionKey, "")
	enc, err := NewEncryptorFromEnv()
	if err != nil {
		t.Fatalf("NewEncryptorFromEnv: %v", err)
	}
	if enc != nil {
		t.Fatal("expected nil encryptor when env unset")
	}
}
