package store

import (
	"context"
	"encoding/json"
	"time"
)

const insertSessionEvent = `
INSERT INTO session_events (session_id, type, payload)
VALUES ($1, $2, $3::jsonb)
RETURNING id
`

type InsertSessionEventParams struct {
	SessionID string
	Type      string
	Payload   json.RawMessage
}

// InsertSessionEvent appends an audit event for a session. Returns the row id for ordering.
func (q *Queries) InsertSessionEvent(ctx context.Context, arg InsertSessionEventParams) (int64, error) {
	payload := arg.Payload
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	row := q.db.QueryRowContext(ctx, insertSessionEvent, arg.SessionID, arg.Type, payload)
	var id int64
	err := row.Scan(&id)
	return id, err
}

const listSessionEventsBySessionID = `
SELECT id, session_id, type, payload, created_at
FROM session_events
WHERE session_id = $1
ORDER BY id ASC
`

type SessionEvent struct {
	ID        int64           `json:"id"`
	SessionID string          `json:"session_id"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

// ListSessionEventsBySessionID returns session audit events in insertion order.
func (q *Queries) ListSessionEventsBySessionID(ctx context.Context, sessionID string) ([]SessionEvent, error) {
	rows, err := q.db.QueryContext(ctx, listSessionEventsBySessionID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SessionEvent
	for rows.Next() {
		var ev SessionEvent
		if err := rows.Scan(&ev.ID, &ev.SessionID, &ev.Type, &ev.Payload, &ev.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}
