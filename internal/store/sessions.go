package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const insertSession = `
INSERT INTO sessions (id, agent_version_id, input, status, parent_session_id, depth, bundle_version_id)
VALUES ($1, $2, $3::jsonb, $4, $5, $6, NULLIF($7, '')::uuid)
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
	row := q.db.QueryRowContext(ctx, insertSession,
		arg.ID,
		arg.AgentVersionID,
		arg.Input,
		arg.Status,
		parent,
		arg.Depth,
		bundleVersionID,
	)
	var id string
	err := row.Scan(&id)
	return id, err
}

const selectSession = `
SELECT id, agent_version_id, input, status, output, error, history, created_at, updated_at
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
	var output sql.NullString
	var errText sql.NullString
	err := row.Scan(
		&s.ID,
		&s.AgentVersionID,
		&s.Input,
		&s.Status,
		&output,
		&errText,
		&s.History,
		&s.CreatedAt,
		&s.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, err
	}
	if err != nil {
		return Session{}, err
	}
	if output.Valid {
		s.Output = json.RawMessage(output.String)
	}
	if errText.Valid {
		s.Error = &errText.String
	}
	return s, nil
}

const updateSession = `
UPDATE sessions
SET status = $2,
	output = COALESCE($3::jsonb, output),
	error = COALESCE($4, error),
	history = COALESCE($5::jsonb, history),
	updated_at = NOW()
WHERE id = $1
RETURNING updated_at
`

type UpdateSessionParams struct {
	ID     string
	Status string
	// Output, when non-nil, replaces the session output JSON (use json.RawMessage("null") to clear).
	Output json.RawMessage
	// Error, when non-nil, replaces the session error text; use a pointer to empty string to clear.
	Error *string
	// History, when non-nil, replaces the conversation history JSON array.
	History json.RawMessage
}

func (q *Queries) UpdateSession(ctx context.Context, arg UpdateSessionParams) (time.Time, error) {
	var output any
	if arg.Output != nil {
		output = arg.Output
	}
	var errText any
	if arg.Error != nil {
		errText = *arg.Error
	}
	var history any
	if arg.History != nil {
		history = arg.History
	}
	row := q.db.QueryRowContext(ctx, updateSession,
		arg.ID,
		arg.Status,
		output,
		errText,
		history,
	)
	var updatedAt time.Time
	err := row.Scan(&updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, err
	}
	return updatedAt, err
}

type SessionListRow struct {
	ID             string
	AgentVersionID string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
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

const listSessionsByAgentVersionID = `
SELECT id, agent_version_id, status, created_at, updated_at
FROM sessions
WHERE (NULLIF($1, '') IS NULL OR agent_version_id = NULLIF($1, '')::uuid)
  AND ($2 = '' OR status = $2)
  AND ($3::boolean OR parent_session_id IS NULL)
ORDER BY updated_at DESC
`

type ListSessionsByAgentVersionIDParams struct {
	AgentVersionID string
	// Status filter; empty means all statuses.
	Status string
	// IncludeChildren includes delegated child sessions when true.
	IncludeChildren bool
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

func (q *Queries) ListSessionsByAgentVersionID(ctx context.Context, arg ListSessionsByAgentVersionIDParams) ([]SessionListRow, error) {
	rows, err := q.db.QueryContext(ctx, listSessionsByAgentVersionID, arg.AgentVersionID, arg.Status, arg.IncludeChildren)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SessionListRow
	for rows.Next() {
		var row SessionListRow
		if err := rows.Scan(&row.ID, &row.AgentVersionID, &row.Status, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
