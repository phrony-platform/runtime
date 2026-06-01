package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const insertApproval = `
INSERT INTO approvals (
	id, session_id, call_id, status, route, reason
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING created_at
`

type InsertApprovalParams struct {
	ID        string
	SessionID string
	CallID    string
	Status    string
	Route     string
	Reason    string
}

func (q *Queries) InsertApproval(ctx context.Context, arg InsertApprovalParams) (time.Time, error) {
	row := q.db.QueryRowContext(ctx, insertApproval,
		arg.ID, arg.SessionID, arg.CallID, arg.Status, arg.Route, arg.Reason,
	)
	var createdAt time.Time
	err := row.Scan(&createdAt)
	return createdAt, err
}

const decideApproval = `
UPDATE approvals
SET status = $2,
	decided_by = $3,
	comment = $4,
	decided_at = NOW()
WHERE id = $1 AND status = 'pending'
RETURNING decided_at
`

type DecideApprovalParams struct {
	ID        string
	Status    string
	DecidedBy string
	Comment   string
}

func (q *Queries) DecideApproval(ctx context.Context, arg DecideApprovalParams) (time.Time, error) {
	row := q.db.QueryRowContext(ctx, decideApproval,
		arg.ID, arg.Status, arg.DecidedBy, arg.Comment,
	)
	var decidedAt time.Time
	err := row.Scan(&decidedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, err
	}
	return decidedAt, err
}

const getPendingApprovalBySession = `
SELECT id, session_id, call_id, status, route, reason, decided_by, comment, created_at, decided_at
FROM approvals
WHERE session_id = $1 AND status = 'pending'
ORDER BY created_at DESC
LIMIT 1
`

type Approval struct {
	ID        string     `json:"id"`
	SessionID string     `json:"session_id"`
	CallID    string     `json:"call_id"`
	Status    string     `json:"status"`
	Route     string     `json:"route"`
	Reason    string     `json:"reason"`
	DecidedBy string     `json:"decided_by"`
	Comment   string     `json:"comment"`
	CreatedAt time.Time  `json:"created_at"`
	DecidedAt *time.Time `json:"decided_at"`
}

func (q *Queries) GetPendingApprovalBySession(ctx context.Context, sessionID string) (Approval, error) {
	row := q.db.QueryRowContext(ctx, getPendingApprovalBySession, sessionID)
	var a Approval
	var decidedAt sql.NullTime
	err := row.Scan(
		&a.ID, &a.SessionID, &a.CallID, &a.Status, &a.Route, &a.Reason,
		&a.DecidedBy, &a.Comment, &a.CreatedAt, &decidedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Approval{}, err
	}
	if err != nil {
		return Approval{}, err
	}
	if decidedAt.Valid {
		a.DecidedAt = &decidedAt.Time
	}
	return a, nil
}

const updateToolInvocationStatus = `
UPDATE tool_invocations
SET status = $2, updated_at = NOW()
WHERE call_id = $1
`

func (q *Queries) UpdateToolInvocationStatus(ctx context.Context, callID, status string) error {
	_, err := q.db.ExecContext(ctx, updateToolInvocationStatus, callID, status)
	return err
}
