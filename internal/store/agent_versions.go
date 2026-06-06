package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
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
SELECT av.id, av.deprecated_at, av.retired_at, a.archived_at
FROM agent_versions av
INNER JOIN agents a ON a.id = av.agent_id
WHERE a.namespace = $1 AND a.name = $2 AND av.version = $3
`

// AgentVersionLookupResult is the resolved runnable version or why it cannot run.
type AgentVersionLookupResult struct {
	ID           string
	Deprecated   bool
	Retired      bool
	AgentArchive bool
}

func (q *Queries) AgentVersionIDByLabel(ctx context.Context, namespace, name, version string) (AgentVersionLookupResult, error) {
	row := q.db.QueryRowContext(ctx, agentVersionIDByLabel, namespace, name, version)
	var id string
	var deprecatedAt, retiredAt, archivedAt sql.NullTime
	err := row.Scan(&id, &deprecatedAt, &retiredAt, &archivedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentVersionLookupResult{}, err
	}
	if err != nil {
		return AgentVersionLookupResult{}, err
	}
	return AgentVersionLookupResult{
		ID:           id,
		Deprecated:   deprecatedAt.Valid,
		Retired:      retiredAt.Valid,
		AgentArchive: archivedAt.Valid,
	}, nil
}

const agentVersionByLabel = `
SELECT av.id, av.version, av.content_hash, av.manifest, av.deployed_at, av.deprecated_at, av.retired_at
FROM agent_versions av
INNER JOIN agents a ON a.id = av.agent_id
WHERE a.namespace = $1 AND a.name = $2 AND av.version = $3
`

// AgentVersionByLabelResult is a published version with manifest and lifecycle metadata.
type AgentVersionByLabelResult struct {
	ID           string
	Version      string
	ContentHash  string
	Manifest     json.RawMessage
	PublishedAt  time.Time
	DeprecatedAt sql.NullTime
	RetiredAt    sql.NullTime
}

// GetAgentVersionByLabel returns manifest and metadata for a published version label.
func (q *Queries) GetAgentVersionByLabel(ctx context.Context, namespace, name, version string) (AgentVersionByLabelResult, error) {
	row := q.db.QueryRowContext(ctx, agentVersionByLabel, namespace, name, version)
	var out AgentVersionByLabelResult
	err := row.Scan(
		&out.ID,
		&out.Version,
		&out.ContentHash,
		&out.Manifest,
		&out.PublishedAt,
		&out.DeprecatedAt,
		&out.RetiredAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentVersionByLabelResult{}, err
	}
	return out, err
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

const agentVersionIdentity = `
SELECT a.namespace, a.name, av.version, av.manifest
FROM agent_versions av
INNER JOIN agents a ON a.id = av.agent_id
WHERE av.id = $1
`

// AgentVersionIdentity is the published agent ref and manifest for a version row.
type AgentVersionIdentity struct {
	Namespace string
	Name      string
	Version   string
	Manifest  json.RawMessage
}

// GetAgentVersionIdentity returns agent ref labels and manifest for a version id.
func (q *Queries) GetAgentVersionIdentity(ctx context.Context, agentVersionID string) (AgentVersionIdentity, error) {
	row := q.db.QueryRowContext(ctx, agentVersionIdentity, agentVersionID)
	var out AgentVersionIdentity
	err := row.Scan(&out.Namespace, &out.Name, &out.Version, &out.Manifest)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentVersionIdentity{}, err
	}
	return out, err
}

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
