package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const insertSession = `
INSERT INTO sessions (id, agent_version_id, input, status, parent_session_id, depth, bundle_version_id, root_session_id)
VALUES ($1, $2, $3::jsonb, $4, $5, $6, NULLIF($7, '')::uuid, $8)
RETURNING id
`

type InsertSessionParams struct {
	ID             string          `json:"id"`
	AgentVersionID string          `json:"agent_version_id"`
	Input          json.RawMessage `json:"input"`
	Status         string          `json:"status"`
	// ParentSessionID links a nested child session (agent delegation) to the
	// session that spawned it; nil for top-level runs.
	ParentSessionID *string `json:"parent_session_id"`
	// Depth is the delegation depth of the session (0 for a top-level run).
	Depth int `json:"depth"`
	// BundleVersionID is set for top-level sessions started from a deployed bundle.
	BundleVersionID *string `json:"bundle_version_id"`
	// RootSessionID is the timeline root for this session tree; defaults to ID when empty.
	RootSessionID string `json:"root_session_id"`
}

func (q *Queries) InsertSession(ctx context.Context, arg InsertSessionParams) (string, error) {
	var parent any
	if arg.ParentSessionID != nil {
		parent = *arg.ParentSessionID
	}
	var bundleVersionID string
	if arg.BundleVersionID != nil {
		bundleVersionID = *arg.BundleVersionID
	}
	rootSessionID := arg.RootSessionID
	if rootSessionID == "" {
		rootSessionID = arg.ID
	}
	row := q.db.QueryRowContext(ctx, insertSession,
		arg.ID,
		arg.AgentVersionID,
		arg.Input,
		arg.Status,
		parent,
		arg.Depth,
		bundleVersionID,
		rootSessionID,
	)
	var id string
	err := row.Scan(&id)
	return id, err
}

const selectSession = `
SELECT id, agent_version_id, input, status, error, root_session_id, event_seq, created_at, updated_at
FROM sessions
WHERE id = $1
`

const selectSessionDelegationMeta = `
SELECT parent_session_id, bundle_version_id, depth
FROM sessions
WHERE id = $1
`

type SessionDelegationMeta struct {
	ParentSessionID *string
	BundleVersionID *string
	Depth           int
}

func (q *Queries) GetSessionDelegationMeta(ctx context.Context, sessionID string) (SessionDelegationMeta, error) {
	row := q.db.QueryRowContext(ctx, selectSessionDelegationMeta, sessionID)
	var out SessionDelegationMeta
	var parent sql.NullString
	var bundleVersion sql.NullString
	err := row.Scan(&parent, &bundleVersion, &out.Depth)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionDelegationMeta{}, err
	}
	if err != nil {
		return SessionDelegationMeta{}, err
	}
	if parent.Valid {
		out.ParentSessionID = &parent.String
	}
	if bundleVersion.Valid {
		out.BundleVersionID = &bundleVersion.String
	}
	return out, nil
}

func (q *Queries) GetSession(ctx context.Context, sessionID string) (Session, error) {
	row := q.db.QueryRowContext(ctx, selectSession, sessionID)
	var s Session
	var errText sql.NullString
	err := row.Scan(
		&s.ID,
		&s.AgentVersionID,
		&s.Input,
		&s.Status,
		&errText,
		&s.RootSessionID,
		&s.EventSeq,
		&s.CreatedAt,
		&s.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, err
	}
	if err != nil {
		return Session{}, err
	}
	if errText.Valid {
		s.Error = &errText.String
	}
	return s, nil
}

const updateSession = `
UPDATE sessions
SET status = $2,
	error = COALESCE($3, error),
	updated_at = NOW()
WHERE id = $1
RETURNING updated_at
`

type UpdateSessionParams struct {
	ID     string
	Status string
	// Error, when non-nil, replaces the session error text; use a pointer to empty string to clear.
	Error *string
}

func (q *Queries) UpdateSession(ctx context.Context, arg UpdateSessionParams) (time.Time, error) {
	var errText any
	if arg.Error != nil {
		errText = *arg.Error
	}
	row := q.db.QueryRowContext(ctx, updateSession,
		arg.ID,
		arg.Status,
		errText,
	)
	var updatedAt time.Time
	err := row.Scan(&updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, err
	}
	return updatedAt, err
}

type SessionListRow struct {
	ID              string
	AgentVersionID  string
	Status          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	BundleVersionID sql.NullString
	AgentNamespace  string
	AgentName       string
	AgentVersion    string
	BundleNamespace sql.NullString
	BundleName      sql.NullString
	BundleVersion   sql.NullString
}

const cancelSession = `
UPDATE sessions
SET status = 'cancelled', updated_at = NOW()
WHERE id = $1
  AND status NOT IN ('completed', 'failed', 'cancelled')
RETURNING id
`

func (q *Queries) CancelSession(ctx context.Context, sessionID string) (string, error) {
	row := q.db.QueryRowContext(ctx, cancelSession, sessionID)
	var id string
	err := row.Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return id, err
}

const completeSession = `
UPDATE sessions
SET status = 'completed', updated_at = NOW()
WHERE id = $1
  AND status NOT IN ('completed', 'failed', 'cancelled')
RETURNING id
`

func (q *Queries) CompleteSession(ctx context.Context, sessionID string) (string, error) {
	row := q.db.QueryRowContext(ctx, completeSession, sessionID)
	var id string
	err := row.Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return id, err
}

const listSessions = `
SELECT
  s.id, s.agent_version_id, s.status, s.created_at, s.updated_at, s.bundle_version_id,
  COALESCE(ag.namespace, av.manifest->'metadata'->>'namespace', '') AS agent_namespace,
  COALESCE(ag.name, av.manifest->'metadata'->>'name', '') AS agent_name,
  av.version AS agent_version,
  b.namespace AS bundle_namespace,
  b.name AS bundle_name,
  bv.version AS bundle_version
FROM sessions s
JOIN agent_versions av ON av.id = s.agent_version_id
LEFT JOIN agents ag ON ag.id = av.agent_id
LEFT JOIN bundle_versions bv ON bv.id = s.bundle_version_id
LEFT JOIN bundles b ON b.id = bv.bundle_id
WHERE (NULLIF($1, '') IS NULL OR s.agent_version_id = NULLIF($1, '')::uuid)
  AND ($2 = '' OR s.status = $2)
  AND ($3::boolean OR s.parent_session_id IS NULL)
  AND (NULLIF($4, '') IS NULL OR s.bundle_version_id IN
       (SELECT id FROM bundle_versions WHERE bundle_id = NULLIF($4, '')::uuid))
  AND ($5 = '' OR ($5 = 'bundle' AND s.bundle_version_id IS NOT NULL)
                OR ($5 = 'agent'  AND s.bundle_version_id IS NULL))
ORDER BY s.updated_at DESC
`

type ListSessionsParams struct {
	AgentVersionID string
	// Status filter; empty means all statuses.
	Status string
	// IncludeChildren includes delegated child sessions when true.
	IncludeChildren bool
	// BundleID filters to sessions originating from the given bundle; empty means no filter.
	BundleID string
	// Kind filters by session kind: empty (all), "agent", or "bundle".
	Kind string
}

const listDescendantSessionIDs = `
WITH RECURSIVE descendants AS (
	SELECT id, depth
	FROM sessions
	WHERE id = $1
	UNION ALL
	SELECT s.id, s.depth
	FROM sessions s
	INNER JOIN descendants d ON s.parent_session_id = d.id
)
SELECT id FROM descendants
ORDER BY depth ASC, id ASC
`

// ListDescendantSessionIDs returns the root session id and all delegated child session ids.
func (q *Queries) ListDescendantSessionIDs(ctx context.Context, rootSessionID string) ([]string, error) {
	rows, err := q.db.QueryContext(ctx, listDescendantSessionIDs, rootSessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (q *Queries) ListSessions(ctx context.Context, arg ListSessionsParams) ([]SessionListRow, error) {
	rows, err := q.db.QueryContext(ctx, listSessions, arg.AgentVersionID, arg.Status, arg.IncludeChildren, arg.BundleID, arg.Kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SessionListRow
	for rows.Next() {
		var row SessionListRow
		if err := rows.Scan(
			&row.ID, &row.AgentVersionID, &row.Status, &row.CreatedAt, &row.UpdatedAt, &row.BundleVersionID,
			&row.AgentNamespace, &row.AgentName, &row.AgentVersion,
			&row.BundleNamespace, &row.BundleName, &row.BundleVersion,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
