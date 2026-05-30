package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
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

const selectSession = `
SELECT id, agent_version_id, input, status, output, error, created_at, updated_at
FROM sessions
WHERE id = $1
`

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
	row := q.db.QueryRowContext(ctx, updateSession,
		arg.ID,
		arg.Status,
		output,
		errText,
	)
	var updatedAt time.Time
	err := row.Scan(&updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, err
	}
	return updatedAt, err
}
