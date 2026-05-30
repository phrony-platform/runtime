package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	// EnvEncryptionKey is the runtime daemon env var for the AES-256 master key.
	EnvEncryptionKey = "RUNTIME_SECRETS_ENCRYPTION_KEY"
	defaultKeyVersion = 1
	aesKeySize        = 32
	gcmNonceSize      = 12
)

// Encrypted is the at-rest representation of one secret value.
type Encrypted struct {
	KeyVersion int
	Nonce      []byte
	Ciphertext []byte
}

// Encryptor seals and opens secret payloads with AES-256-GCM.
type Encryptor struct {
	aead       cipher.AEAD
	keyVersion int
}

// NewEncryptorFromEnv loads a 32-byte key from RUNTIME_SECRETS_ENCRYPTION_KEY (base64 or hex).
// When the env var is unset, returns (nil, nil).
func NewEncryptorFromEnv() (*Encryptor, error) {
	encoded := strings.TrimSpace(os.Getenv(EnvEncryptionKey))
	if encoded == "" {
		return nil, nil
	}
	return NewEncryptorFromEncoded(encoded)
}

// NewEncryptorFromEncoded parses a base64- or hex-encoded 32-byte master key.
func NewEncryptorFromEncoded(encoded string) (*Encryptor, error) {
	key, err := decodeMasterKey(encoded)
	if err != nil {
		return nil, err
	}
	return NewEncryptor(key, defaultKeyVersion)
}

// NewEncryptor constructs an encryptor with the given raw key material.
func NewEncryptor(key []byte, keyVersion int) (*Encryptor, error) {
	if len(key) != aesKeySize {
		return nil, fmt.Errorf("encryption key must be %d bytes, got %d", aesKeySize, len(key))
	}
	if keyVersion < 1 {
		return nil, errors.New("key version must be >= 1")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return &Encryptor{aead: aead, keyVersion: keyVersion}, nil
}

func decodeMasterKey(encoded string) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)
	if key, err := hex.DecodeString(encoded); err == nil && len(key) == aesKeySize {
		return key, nil
	}
	if key, err := base64.StdEncoding.DecodeString(encoded); err == nil && len(key) == aesKeySize {
		return key, nil
	}
	if key, err := base64.RawStdEncoding.DecodeString(encoded); err == nil && len(key) == aesKeySize {
		return key, nil
	}
	return nil, fmt.Errorf("encryption key must be %d bytes encoded as base64 or hex", aesKeySize)
}

func associatedData(agentVersionID, secretName string) []byte {
	return []byte(agentVersionID + "\x00" + secretName)
}

// Encrypt seals plaintext for the given agent version and secret name.
func (e *Encryptor) Encrypt(agentVersionID, secretName string, plaintext []byte) (Encrypted, error) {
	if e == nil {
		return Encrypted{}, errors.New("encryptor is not configured")
	}
	nonce := make([]byte, gcmNonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return Encrypted{}, fmt.Errorf("generate nonce: %w", err)
	}
	aad := associatedData(agentVersionID, secretName)
	ciphertext := e.aead.Seal(nil, nonce, plaintext, aad)
	return Encrypted{
		KeyVersion: e.keyVersion,
		Nonce:      nonce,
		Ciphertext: ciphertext,
	}, nil
}

// Decrypt opens an encrypted payload for the given agent version and secret name.
func (e *Encryptor) Decrypt(agentVersionID, secretName string, enc Encrypted) ([]byte, error) {
	if e == nil {
		return nil, errors.New("encryptor is not configured")
	}
	if enc.KeyVersion != e.keyVersion {
		return nil, fmt.Errorf("unsupported key version %d", enc.KeyVersion)
	}
	aad := associatedData(agentVersionID, secretName)
	plaintext, err := e.aead.Open(nil, enc.Nonce, enc.Ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("decrypt secret: %w", err)
	}
	return plaintext, nil
}
