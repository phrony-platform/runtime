package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

const insertEvent = `
INSERT INTO events (
	session_id, root_session_id, seq, type, turn, call_id, child_session_id, actor, payload
) VALUES (
	$1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb
)
RETURNING id
`

type InsertEventParams struct {
	SessionID      string
	RootSessionID  string
	Seq            int
	Type           string
	Turn           *int
	CallID         *string
	ChildSessionID *string
	Actor          string
	Payload        json.RawMessage
}

// InsertEvent appends an event at the given per-session sequence number.
func (q *Queries) InsertEvent(ctx context.Context, arg InsertEventParams) (int64, error) {
	payload := arg.Payload
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	var turn any
	if arg.Turn != nil {
		turn = *arg.Turn
	}
	var callID any
	if arg.CallID != nil {
		callID = *arg.CallID
	}
	var childSessionID any
	if arg.ChildSessionID != nil {
		childSessionID = *arg.ChildSessionID
	}
	row := q.db.QueryRowContext(ctx, insertEvent,
		arg.SessionID,
		arg.RootSessionID,
		arg.Seq,
		arg.Type,
		turn,
		callID,
		childSessionID,
		arg.Actor,
		payload,
	)
	var id int64
	err := row.Scan(&id)
	return id, err
}

const nextSessionSeq = `
UPDATE sessions
SET event_seq = event_seq + 1
WHERE id = $1
RETURNING event_seq
`

// NextSessionSeq increments and returns the next gap-free event sequence for a session.
func (q *Queries) NextSessionSeq(ctx context.Context, sessionID string) (int, error) {
	row := q.db.QueryRowContext(ctx, nextSessionSeq, sessionID)
	var seq int
	err := row.Scan(&seq)
	return seq, err
}

const listEventsBySession = `
SELECT id, session_id, root_session_id, seq, ts, type, turn, call_id, child_session_id, actor, payload
FROM events
WHERE session_id = $1
ORDER BY seq ASC
`

// ListEventsBySession returns events for a session in per-session sequence order.
func (q *Queries) ListEventsBySession(ctx context.Context, sessionID string) ([]Event, error) {
	rows, err := q.db.QueryContext(ctx, listEventsBySession, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

const listEventsByRoot = `
SELECT id, session_id, root_session_id, seq, ts, type, turn, call_id, child_session_id, actor, payload
FROM events
WHERE root_session_id = $1
ORDER BY ts ASC, id ASC
`

// ListEventsByRoot returns the merged parent+child timeline for a session tree.
func (q *Queries) ListEventsByRoot(ctx context.Context, rootSessionID string) ([]Event, error) {
	rows, err := q.db.QueryContext(ctx, listEventsByRoot, rootSessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func scanEvents(rows *sql.Rows) ([]Event, error) {
	var out []Event
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func scanEvent(scanner interface {
	Scan(dest ...any) error
}) (Event, error) {
	var ev Event
	var turn sql.NullInt32
	var callID sql.NullString
	var childSessionID sql.NullString
	err := scanner.Scan(
		&ev.ID,
		&ev.SessionID,
		&ev.RootSessionID,
		&ev.Seq,
		&ev.TS,
		&ev.Type,
		&turn,
		&callID,
		&childSessionID,
		&ev.Actor,
		&ev.Payload,
	)
	if err != nil {
		return Event{}, err
	}
	if turn.Valid {
		t := int(turn.Int32)
		ev.Turn = &t
	}
	if callID.Valid {
		ev.CallID = &callID.String
	}
	if childSessionID.Valid {
		ev.ChildSessionID = &childSessionID.String
	}
	return ev, nil
}
