package secrets

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestNewEncryptor_invalidKeyLength(t *testing.T) {
	_, err := NewEncryptor([]byte("short"), 1)
	if err == nil {
		t.Fatal("NewEncryptor() = nil, want error")
	}
}

func TestNewEncryptor_invalidKeyVersion(t *testing.T) {
	key := bytes.Repeat([]byte{0x01}, aesKeySize)
	_, err := NewEncryptor(key, 0)
	if err == nil {
		t.Fatal("NewEncryptor() = nil, want error")
	}
}

func TestNewEncryptorFromEncoded_invalid(t *testing.T) {
	_, err := NewEncryptorFromEncoded("not-valid-encoding")
	if err == nil {
		t.Fatal("NewEncryptorFromEncoded() = nil, want error")
	}
}

func TestEncrypt_nilEncryptor(t *testing.T) {
	var enc *Encryptor
	_, err := enc.Encrypt("v", "s", []byte("x"))
	if err == nil {
		t.Fatal("Encrypt() = nil, want error")
	}
}

func TestDecrypt_wrongKeyVersion(t *testing.T) {
	key := bytes.Repeat([]byte{0xab}, aesKeySize)
	enc, err := NewEncryptor(key, 1)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}
	sealed, err := enc.Encrypt("v", "s", []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	sealed.KeyVersion = 99
	if _, err := enc.Decrypt("v", "s", sealed); err == nil {
		t.Fatal("Decrypt() = nil, want unsupported key version error")
	}
}

func TestNewEncryptorFromEnv_set(t *testing.T) {
	key := bytes.Repeat([]byte{0x03}, aesKeySize)
	t.Setenv(EnvEncryptionKey, hex.EncodeToString(key))

	enc, err := NewEncryptorFromEnv()
	if err != nil {
		t.Fatalf("NewEncryptorFromEnv: %v", err)
	}
	if enc == nil {
		t.Fatal("expected encryptor when env is set")
	}
}
