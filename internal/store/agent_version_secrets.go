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
	Name       string
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

const listAgentVersionSecrets = `
SELECT name, key_version, nonce, ciphertext
FROM agent_version_secrets
WHERE agent_version_id = $1
ORDER BY name
`

// ListSecretsForVersion returns encrypted secret rows for an agent version (internal use).
func (q *Queries) ListSecretsForVersion(ctx context.Context, agentVersionID string) ([]AgentVersionSecretRow, error) {
	rows, err := q.db.QueryContext(ctx, listAgentVersionSecrets, agentVersionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AgentVersionSecretRow
	for rows.Next() {
		var row AgentVersionSecretRow
		if err := rows.Scan(&row.Name, &row.KeyVersion, &row.Nonce, &row.Ciphertext); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
