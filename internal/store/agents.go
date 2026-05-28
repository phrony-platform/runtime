package store

import (
	"context"
	"encoding/json"
)

const upsertAgent = `
INSERT INTO agents (id, namespace, name, owner, labels)
VALUES ($1, $2, $3, $4, $5::jsonb)
ON CONFLICT (namespace, name) DO UPDATE SET
	owner = EXCLUDED.owner,
	labels = EXCLUDED.labels,
	updated_at = NOW()
RETURNING id
`

type UpsertAgentParams struct {
	ID        string          `json:"id"`
	Namespace string          `json:"namespace"`
	Name      string          `json:"name"`
	Owner     string          `json:"owner"`
	Labels    json.RawMessage `json:"labels"`
}

func (q *Queries) UpsertAgent(ctx context.Context, arg UpsertAgentParams) (string, error) {
	row := q.db.QueryRowContext(ctx, upsertAgent,
		arg.ID,
		arg.Namespace,
		arg.Name,
		arg.Owner,
		arg.Labels,
	)
	var id string
	err := row.Scan(&id)
	return id, err
}

const upsertAgentVersion = `
INSERT INTO agent_versions (id, agent_id, version, content_hash, manifest)
VALUES ($1, $2, $3, $4, $5::jsonb)
ON CONFLICT (agent_id, version) DO UPDATE SET
	content_hash = EXCLUDED.content_hash,
	manifest = EXCLUDED.manifest,
	deployed_at = NOW()
RETURNING id
`

type UpsertAgentVersionParams struct {
	ID          string          `json:"id"`
	AgentID     string          `json:"agent_id"`
	Version     string          `json:"version"`
	ContentHash string          `json:"content_hash"`
	Manifest    json.RawMessage `json:"manifest"`
}

func (q *Queries) UpsertAgentVersion(ctx context.Context, arg UpsertAgentVersionParams) (string, error) {
	row := q.db.QueryRowContext(ctx, upsertAgentVersion,
		arg.ID,
		arg.AgentID,
		arg.Version,
		arg.ContentHash,
		arg.Manifest,
	)
	var id string
	err := row.Scan(&id)
	return id, err
}
