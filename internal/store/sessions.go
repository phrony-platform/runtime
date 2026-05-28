package store

import (
	"context"
	"encoding/json"
)

const insertSession = `
INSERT INTO sessions (id, agent_version_id, input, status)
VALUES ($1, $2, $3::jsonb, $4)
RETURNING id
`

type InsertSessionParams struct {
	ID             string          `json:"id"`
	AgentVersionID string          `json:"agent_version_id"`
	Input          json.RawMessage `json:"input"`
	Status         string          `json:"status"`
}

func (q *Queries) InsertSession(ctx context.Context, arg InsertSessionParams) (string, error) {
	row := q.db.QueryRowContext(ctx, insertSession,
		arg.ID,
		arg.AgentVersionID,
		arg.Input,
		arg.Status,
	)
	var id string
	err := row.Scan(&id)
	return id, err
}
