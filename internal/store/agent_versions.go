package store

import (
	"context"
	"database/sql"
	"errors"
)

const latestAgentVersionID = `
SELECT av.id
FROM agent_versions av
INNER JOIN agents a ON a.id = av.agent_id
WHERE a.namespace = $1 AND a.name = $2
ORDER BY av.deployed_at DESC
LIMIT 1
`

func (q *Queries) LatestAgentVersionID(ctx context.Context, namespace, name string) (string, error) {
	row := q.db.QueryRowContext(ctx, latestAgentVersionID, namespace, name)
	var id string
	err := row.Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return id, err
}

const agentVersionIDByLabel = `
SELECT av.id
FROM agent_versions av
INNER JOIN agents a ON a.id = av.agent_id
WHERE a.namespace = $1 AND a.name = $2 AND av.version = $3
`

func (q *Queries) AgentVersionIDByLabel(ctx context.Context, namespace, name, version string) (string, error) {
	row := q.db.QueryRowContext(ctx, agentVersionIDByLabel, namespace, name, version)
	var id string
	err := row.Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return id, err
}
