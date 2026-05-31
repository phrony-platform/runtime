package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const insertDeployment = `
INSERT INTO deployments (id, agent_id, agent_version_id, action, actor)
VALUES ($1, $2, $3, $4, $5)
RETURNING id
`

func (q *Queries) InsertDeployment(ctx context.Context, id, agentID, agentVersionID, action, actor string) (string, error) {
	row := q.db.QueryRowContext(ctx, insertDeployment, id, agentID, agentVersionID, action, actor)
	var deploymentID string
	err := row.Scan(&deploymentID)
	return deploymentID, err
}

const activeAgentVersion = `
SELECT av.id, av.version, av.deprecated_at, av.retired_at, a.archived_at
FROM deployments d
INNER JOIN agent_versions av ON av.id = d.agent_version_id
INNER JOIN agents a ON a.id = d.agent_id
WHERE a.namespace = $1 AND a.name = $2
ORDER BY d.created_at DESC
LIMIT 1
`

// ActiveAgentVersionResult is the currently activated version for an agent.
type ActiveAgentVersionResult struct {
	AgentVersionID string
	Version        string
	Deprecated     bool
	Retired        bool
	AgentArchived  bool
}

// ActiveAgentVersion returns the agent version from the most recent deployment row.
func (q *Queries) ActiveAgentVersion(ctx context.Context, namespace, name string) (ActiveAgentVersionResult, error) {
	row := q.db.QueryRowContext(ctx, activeAgentVersion, namespace, name)
	var out ActiveAgentVersionResult
	var deprecatedAt, retiredAt, archivedAt sql.NullTime
	err := row.Scan(&out.AgentVersionID, &out.Version, &deprecatedAt, &retiredAt, &archivedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ActiveAgentVersionResult{}, err
	}
	if err != nil {
		return ActiveAgentVersionResult{}, err
	}
	out.Deprecated = deprecatedAt.Valid
	out.Retired = retiredAt.Valid
	out.AgentArchived = archivedAt.Valid
	return out, nil
}

const previousActiveVersion = `
WITH active AS (
	SELECT agent_version_id
	FROM deployments
	WHERE agent_id = $1
	ORDER BY created_at DESC
	LIMIT 1
)
SELECT d.agent_version_id
FROM deployments d
WHERE d.agent_id = $1
	AND d.agent_version_id <> (SELECT agent_version_id FROM active)
ORDER BY d.created_at DESC
LIMIT 1
`

// PreviousActiveVersion returns the agent_version_id from the most recent deployment
// of a version other than the current active one.
func (q *Queries) PreviousActiveVersion(ctx context.Context, agentID string) (string, error) {
	row := q.db.QueryRowContext(ctx, previousActiveVersion, agentID)
	var id string
	err := row.Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return id, err
}

type DeploymentListRow struct {
	Version   string
	Action    string
	Actor     string
	CreatedAt time.Time
}

const listDeployments = `
SELECT av.version, d.action, d.actor, d.created_at
FROM deployments d
INNER JOIN agent_versions av ON av.id = d.agent_version_id
WHERE d.agent_id = $1
ORDER BY d.created_at DESC
`

func (q *Queries) ListDeployments(ctx context.Context, agentID string) ([]DeploymentListRow, error) {
	rows, err := q.db.QueryContext(ctx, listDeployments, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DeploymentListRow
	for rows.Next() {
		var row DeploymentListRow
		if err := rows.Scan(&row.Version, &row.Action, &row.Actor, &row.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

const activeDeploymentDetail = `
SELECT av.version, d.created_at, d.actor
FROM deployments d
INNER JOIN agent_versions av ON av.id = d.agent_version_id
INNER JOIN agents a ON a.id = d.agent_id
WHERE a.namespace = $1 AND a.name = $2
ORDER BY d.created_at DESC
LIMIT 1
`

// ActiveDeploymentDetail is metadata for the current active deployment.
type ActiveDeploymentDetail struct {
	Version    string
	DeployedAt time.Time
	Actor      string
}

func (q *Queries) ActiveDeploymentDetail(ctx context.Context, namespace, name string) (ActiveDeploymentDetail, error) {
	row := q.db.QueryRowContext(ctx, activeDeploymentDetail, namespace, name)
	var out ActiveDeploymentDetail
	err := row.Scan(&out.Version, &out.DeployedAt, &out.Actor)
	if errors.Is(err, sql.ErrNoRows) {
		return ActiveDeploymentDetail{}, err
	}
	return out, err
}

const agentVersionLabelByID = `
SELECT version FROM agent_versions WHERE id = $1
`

func (q *Queries) AgentVersionLabelByID(ctx context.Context, agentVersionID string) (string, error) {
	row := q.db.QueryRowContext(ctx, agentVersionLabelByID, agentVersionID)
	var version string
	err := row.Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return version, err
}
