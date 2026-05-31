package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type AgentListRow struct {
	ID         string
	Namespace  string
	Name       string
	Owner      string
	ArchivedAt sql.NullTime
	CreatedAt  time.Time
}

const listAgents = `
SELECT id, namespace, name, owner, archived_at, created_at
FROM agents
WHERE ($1::text = '' OR namespace = $1)
ORDER BY namespace, name
`

func (q *Queries) ListAgents(ctx context.Context, namespace string) ([]AgentListRow, error) {
	rows, err := q.db.QueryContext(ctx, listAgents, namespace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AgentListRow
	for rows.Next() {
		var row AgentListRow
		if err := rows.Scan(&row.ID, &row.Namespace, &row.Name, &row.Owner, &row.ArchivedAt, &row.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

type AgentVersionListRow struct {
	ID           string
	Version      string
	ContentHash  string
	DeployedAt   time.Time
	DeprecatedAt sql.NullTime
	RetiredAt    sql.NullTime
}

const listAgentVersions = `
SELECT id, version, content_hash, deployed_at, deprecated_at, retired_at
FROM agent_versions
WHERE agent_id = $1
ORDER BY deployed_at DESC
`

func (q *Queries) ListAgentVersions(ctx context.Context, agentID string) ([]AgentVersionListRow, error) {
	rows, err := q.db.QueryContext(ctx, listAgentVersions, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AgentVersionListRow
	for rows.Next() {
		var row AgentVersionListRow
		if err := rows.Scan(&row.ID, &row.Version, &row.ContentHash, &row.DeployedAt, &row.DeprecatedAt, &row.RetiredAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

const agentByID = `
SELECT id, namespace, name, archived_at
FROM agents
WHERE id = $1
`

type AgentByIDRow struct {
	ID         string
	Namespace  string
	Name       string
	ArchivedAt sql.NullTime
}

const agentByNamespaceName = `
SELECT id, namespace, name, archived_at
FROM agents
WHERE namespace = $1 AND name = $2
`

func (q *Queries) AgentByNamespaceName(ctx context.Context, namespace, name string) (AgentByIDRow, error) {
	row := q.db.QueryRowContext(ctx, agentByNamespaceName, namespace, name)
	var out AgentByIDRow
	err := row.Scan(&out.ID, &out.Namespace, &out.Name, &out.ArchivedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentByIDRow{}, err
	}
	return out, err
}

func (q *Queries) AgentByID(ctx context.Context, agentID string) (AgentByIDRow, error) {
	row := q.db.QueryRowContext(ctx, agentByID, agentID)
	var out AgentByIDRow
	err := row.Scan(&out.ID, &out.Namespace, &out.Name, &out.ArchivedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentByIDRow{}, err
	}
	return out, err
}

const deprecateAgentVersion = `
UPDATE agent_versions
SET deprecated_at = NOW()
WHERE agent_id = $1 AND version = $2 AND deprecated_at IS NULL
RETURNING id
`

const retireAgentVersion = `
UPDATE agent_versions
SET retired_at = NOW()
WHERE agent_id = $1 AND version = $2 AND retired_at IS NULL
RETURNING id
`

func (q *Queries) RetireAgentVersion(ctx context.Context, agentID, version string) (string, error) {
	row := q.db.QueryRowContext(ctx, retireAgentVersion, agentID, version)
	var id string
	err := row.Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return id, err
}

func (q *Queries) DeprecateAgentVersion(ctx context.Context, agentID, version string) (string, error) {
	row := q.db.QueryRowContext(ctx, deprecateAgentVersion, agentID, version)
	var id string
	err := row.Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return id, err
}

const archiveAgent = `
UPDATE agents
SET archived_at = NOW(), updated_at = NOW()
WHERE id = $1 AND archived_at IS NULL
RETURNING id
`

func (q *Queries) ArchiveAgent(ctx context.Context, agentID string) (string, error) {
	row := q.db.QueryRowContext(ctx, archiveAgent, agentID)
	var id string
	err := row.Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return id, err
}

const deprecateAllAgentVersions = `
UPDATE agent_versions
SET deprecated_at = NOW()
WHERE agent_id = $1 AND deprecated_at IS NULL
`

func (q *Queries) DeprecateAllAgentVersions(ctx context.Context, agentID string) error {
	_, err := q.db.ExecContext(ctx, deprecateAllAgentVersions, agentID)
	return err
}
