package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

const latestAgentVersionID = `
SELECT av.id
FROM agent_versions av
INNER JOIN agents a ON a.id = av.agent_id
WHERE a.namespace = $1 AND a.name = $2
	AND a.archived_at IS NULL
	AND av.deprecated_at IS NULL
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
SELECT av.id, av.deprecated_at, a.archived_at
FROM agent_versions av
INNER JOIN agents a ON a.id = av.agent_id
WHERE a.namespace = $1 AND a.name = $2 AND av.version = $3
`

// AgentVersionLookupResult is the resolved runnable version or why it cannot run.
type AgentVersionLookupResult struct {
	ID           string
	Deprecated   bool
	AgentArchive bool
}

func (q *Queries) AgentVersionIDByLabel(ctx context.Context, namespace, name, version string) (AgentVersionLookupResult, error) {
	row := q.db.QueryRowContext(ctx, agentVersionIDByLabel, namespace, name, version)
	var id string
	var deprecatedAt, archivedAt sql.NullTime
	err := row.Scan(&id, &deprecatedAt, &archivedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentVersionLookupResult{}, err
	}
	if err != nil {
		return AgentVersionLookupResult{}, err
	}
	return AgentVersionLookupResult{
		ID:           id,
		Deprecated:   deprecatedAt.Valid,
		AgentArchive: archivedAt.Valid,
	}, nil
}

const agentVersionByAgentAndLabel = `
SELECT av.id, av.content_hash
FROM agent_versions av
WHERE av.agent_id = $1 AND av.version = $2
`

// AgentVersionByAgentAndLabel returns a deployed version row for the agent, or sql.ErrNoRows.
func (q *Queries) AgentVersionByAgentAndLabel(ctx context.Context, agentID, version string) (id, contentHash string, err error) {
	row := q.db.QueryRowContext(ctx, agentVersionByAgentAndLabel, agentID, version)
	err = row.Scan(&id, &contentHash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}
	return id, contentHash, err
}

const agentVersionManifest = `
SELECT manifest
FROM agent_versions
WHERE id = $1
`

// GetAgentVersionManifest returns the ref-only manifest JSON for a deployed version.
func (q *Queries) GetAgentVersionManifest(ctx context.Context, agentVersionID string) (json.RawMessage, error) {
	row := q.db.QueryRowContext(ctx, agentVersionManifest, agentVersionID)
	var manifest json.RawMessage
	err := row.Scan(&manifest)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return manifest, err
}
