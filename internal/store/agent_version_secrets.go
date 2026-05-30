package store

import (
	"context"
)

const insertAgentVersionSecret = `
INSERT INTO agent_version_secrets (agent_version_id, name, key_version, nonce, ciphertext)
VALUES ($1, $2, $3, $4, $5)
`

type InsertAgentVersionSecretParams struct {
	AgentVersionID string
	Name           string
	KeyVersion     int
	Nonce          []byte
	Ciphertext     []byte
}

func (q *Queries) InsertAgentVersionSecret(ctx context.Context, arg InsertAgentVersionSecretParams) error {
	_, err := q.db.ExecContext(ctx, insertAgentVersionSecret,
		arg.AgentVersionID,
		arg.Name,
		arg.KeyVersion,
		arg.Nonce,
		arg.Ciphertext,
	)
	return err
}

const selectAgentVersionSecret = `
SELECT key_version, nonce, ciphertext
FROM agent_version_secrets
WHERE agent_version_id = $1 AND name = $2
`

type AgentVersionSecretRow struct {
	KeyVersion int
	Nonce      []byte
	Ciphertext []byte
}

func (q *Queries) AgentVersionSecret(ctx context.Context, agentVersionID, name string) (AgentVersionSecretRow, error) {
	row := q.db.QueryRowContext(ctx, selectAgentVersionSecret, agentVersionID, name)
	var out AgentVersionSecretRow
	err := row.Scan(&out.KeyVersion, &out.Nonce, &out.Ciphertext)
	return out, err
}
