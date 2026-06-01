package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

const insertSessionEvidence = `
INSERT INTO session_evidence (session_id, payload)
VALUES ($1, $2::jsonb)
ON CONFLICT (session_id) DO NOTHING
RETURNING session_id
`

// InsertSessionEvidence records descriptive metadata for a session (idempotent).
func (q *Queries) InsertSessionEvidence(ctx context.Context, sessionID string, payload json.RawMessage) error {
	if sessionID == "" {
		return nil
	}
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	row := q.db.QueryRowContext(ctx, insertSessionEvidence, sessionID, payload)
	var id string
	err := row.Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

const selectSessionEvidence = `
SELECT payload
FROM session_evidence
WHERE session_id = $1
`

// GetSessionEvidence returns the stored evidence payload for a session.
func (q *Queries) GetSessionEvidence(ctx context.Context, sessionID string) (json.RawMessage, error) {
	row := q.db.QueryRowContext(ctx, selectSessionEvidence, sessionID)
	var payload json.RawMessage
	err := row.Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sql.ErrNoRows
	}
	return payload, err
}
