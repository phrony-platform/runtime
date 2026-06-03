package store

import (
	"context"
)

const insertSessionSecret = `
INSERT INTO session_secrets (session_id, name, key_version, nonce, ciphertext)
VALUES ($1, $2, $3, $4, $5)
`

type InsertSessionSecretParams struct {
	SessionID  string
	Name       string
	KeyVersion int
	Nonce      []byte
	Ciphertext []byte
}

func (q *Queries) InsertSessionSecret(ctx context.Context, arg InsertSessionSecretParams) error {
	_, err := q.db.ExecContext(ctx, insertSessionSecret,
		arg.SessionID,
		arg.Name,
		arg.KeyVersion,
		arg.Nonce,
		arg.Ciphertext,
	)
	return err
}

const selectSessionSecret = `
SELECT key_version, nonce, ciphertext
FROM session_secrets
WHERE session_id = $1 AND name = $2
`

type SessionSecretRow struct {
	Name       string
	KeyVersion int
	Nonce      []byte
	Ciphertext []byte
}

func (q *Queries) SessionSecret(ctx context.Context, sessionID, name string) (SessionSecretRow, error) {
	row := q.db.QueryRowContext(ctx, selectSessionSecret, sessionID, name)
	var out SessionSecretRow
	err := row.Scan(&out.KeyVersion, &out.Nonce, &out.Ciphertext)
	return out, err
}

const deleteSessionSecrets = `
DELETE FROM session_secrets
WHERE session_id = $1
`

// DeleteSessionSecrets removes all encrypted secrets for a session (purge on terminal).
func (q *Queries) DeleteSessionSecrets(ctx context.Context, sessionID string) error {
	_, err := q.db.ExecContext(ctx, deleteSessionSecrets, sessionID)
	return err
}

const deleteTerminalSessionSecrets = `
DELETE FROM session_secrets ss
USING sessions s
WHERE ss.session_id = s.id
  AND s.status IN ('completed', 'failed', 'cancelled')
`

// DeleteTerminalSessionSecrets removes secrets for sessions already in a terminal status.
func (q *Queries) DeleteTerminalSessionSecrets(ctx context.Context) error {
	_, err := q.db.ExecContext(ctx, deleteTerminalSessionSecrets)
	return err
}
