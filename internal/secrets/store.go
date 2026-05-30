package secrets

import (
	"context"
	"fmt"

	"github.com/phrony-platform/runtime/internal/store"
)

// DecryptForVersion loads and decrypts a named secret for an agent version.
func (e *Encryptor) DecryptForVersion(ctx context.Context, q *store.Queries, agentVersionID, secretName string) ([]byte, error) {
	if e == nil {
		return nil, fmt.Errorf("secrets encryptor is not configured")
	}
	row, err := q.AgentVersionSecret(ctx, agentVersionID, secretName)
	if err != nil {
		return nil, err
	}
	plaintext, err := e.Decrypt(agentVersionID, secretName, Encrypted{
		KeyVersion: row.KeyVersion,
		Nonce:      row.Nonce,
		Ciphertext: row.Ciphertext,
	})
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}
