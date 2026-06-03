package secrets

import (
	"context"
	"fmt"

	"github.com/phrony-platform/runtime/internal/store"
)

// PersistSessionSecrets encrypts and stores resolved secret values for a session.
// Ciphertext is bound to the session id (AAD: sessionID + "\x00" + name) so a
// payload cannot be replayed under a different session or name.
func (e *Encryptor) PersistSessionSecrets(ctx context.Context, q *store.Queries, sessionID string, resolved map[string][]byte) error {
	if len(resolved) == 0 {
		return nil
	}
	if e == nil {
		return fmt.Errorf("secrets encryptor is not configured")
	}
	for name, plaintext := range resolved {
		sealed, err := e.Encrypt(sessionID, name, plaintext)
		if err != nil {
			return fmt.Errorf("encrypt secret %q: %w", name, err)
		}
		if err := q.InsertSessionSecret(ctx, store.InsertSessionSecretParams{
			SessionID:  sessionID,
			Name:       name,
			KeyVersion: sealed.KeyVersion,
			Nonce:      sealed.Nonce,
			Ciphertext: sealed.Ciphertext,
		}); err != nil {
			return fmt.Errorf("persist secret %q: %w", name, err)
		}
	}
	return nil
}

// DecryptForSession loads and decrypts a named secret for a session. The caller
// is responsible for zeroing the returned plaintext after use.
func (e *Encryptor) DecryptForSession(ctx context.Context, q *store.Queries, sessionID, secretName string) ([]byte, error) {
	if e == nil {
		return nil, fmt.Errorf("secrets encryptor is not configured")
	}
	row, err := q.SessionSecret(ctx, sessionID, secretName)
	if err != nil {
		return nil, err
	}
	plaintext, err := e.Decrypt(sessionID, secretName, Encrypted{
		KeyVersion: row.KeyVersion,
		Nonce:      row.Nonce,
		Ciphertext: row.Ciphertext,
	})
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}

// PurgeSessionSecrets deletes all stored secrets for a session. It is safe to
// call when no encryptor is configured, since terminal cleanup must run
// regardless of whether secrets were ever persisted.
func PurgeSessionSecrets(ctx context.Context, q *store.Queries, sessionID string) error {
	return q.DeleteSessionSecrets(ctx, sessionID)
}
