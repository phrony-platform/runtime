package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/phrony-platform/runtime/internal/model"
)

const insertApproval = `
INSERT INTO approvals (
	id, session_id, call_id, status, route, reason,
	tool, version, args, authority_ref, policy_name,
	approvals_required, approvals_received, comprehension_required,
	on_reject, on_modify, expires_at, policy_runtime
) VALUES (
	$1, $2, $3, $4, $5, $6,
	$7, $8, $9, $10, $11,
	$12, $13, $14,
	$15, $16, $17, $18
)
RETURNING created_at
`

type InsertApprovalParams struct {
	ID                    string
	SessionID             string
	CallID                string
	Status                string
	Route                 string
	Reason                string
	Tool                  string
	Version               string
	Args                  json.RawMessage
	AuthorityRef          string
	PolicyName            string
	ApprovalsRequired     int
	ApprovalsReceived     int
	ComprehensionRequired bool
	OnReject              string
	OnModify              string
	ExpiresAt             *time.Time
	PolicyRuntime         json.RawMessage
}

func (q *Queries) InsertApproval(ctx context.Context, arg InsertApprovalParams) (time.Time, error) {
	args := arg.Args
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	required := arg.ApprovalsRequired
	if required <= 0 {
		required = 1
	}
	row := q.db.QueryRowContext(ctx, insertApproval,
		arg.ID, arg.SessionID, arg.CallID, arg.Status, arg.Route, arg.Reason,
		arg.Tool, arg.Version, args, arg.AuthorityRef, arg.PolicyName,
		required, arg.ApprovalsReceived, arg.ComprehensionRequired,
		arg.OnReject, arg.OnModify, arg.ExpiresAt, nullableJSON(arg.PolicyRuntime),
	)
	var createdAt time.Time
	err := row.Scan(&createdAt)
	return createdAt, err
}

func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

const decideApproval = `
UPDATE approvals
SET status = $2,
	decided_by = $3,
	comment = $4,
	decided_at = NOW(),
	approvals_received = $5
WHERE id = $1 AND status = 'pending'
RETURNING decided_at
`

type DecideApprovalParams struct {
	ID                string
	Status            string
	DecidedBy         string
	Comment           string
	ApprovalsReceived int
}

func (q *Queries) DecideApproval(ctx context.Context, arg DecideApprovalParams) (time.Time, error) {
	row := q.db.QueryRowContext(ctx, decideApproval,
		arg.ID, arg.Status, arg.DecidedBy, arg.Comment, arg.ApprovalsReceived,
	)
	var decidedAt time.Time
	err := row.Scan(&decidedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, err
	}
	return decidedAt, err
}

const incrementApprovalsReceived = `
UPDATE approvals
SET approvals_received = approvals_received + 1
WHERE id = $1 AND status = 'pending'
RETURNING approvals_received, approvals_required
`

func (q *Queries) IncrementApprovalsReceived(ctx context.Context, approvalID string) (received, required int, err error) {
	row := q.db.QueryRowContext(ctx, incrementApprovalsReceived, approvalID)
	err = row.Scan(&received, &required)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, err
	}
	return received, required, err
}

// ErrDuplicateApprovalVote is returned when the same actor votes twice on one approval.
var ErrDuplicateApprovalVote = errors.New("duplicate approval vote")

const insertApprovalVote = `
INSERT INTO approval_votes (
	approval_id, decided_by, decision, comment, comprehension_acknowledged
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (approval_id, decided_by) DO NOTHING
RETURNING created_at
`

type InsertApprovalVoteParams struct {
	ApprovalID                 string
	DecidedBy                  string
	Decision                   string
	Comment                    string
	ComprehensionAcknowledged bool
}

func (q *Queries) InsertApprovalVote(ctx context.Context, arg InsertApprovalVoteParams) (time.Time, error) {
	row := q.db.QueryRowContext(ctx, insertApprovalVote,
		arg.ApprovalID, arg.DecidedBy, arg.Decision, arg.Comment, arg.ComprehensionAcknowledged,
	)
	var createdAt time.Time
	err := row.Scan(&createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, ErrDuplicateApprovalVote
	}
	return createdAt, err
}

const markApprovalEscalated = `
UPDATE approvals
SET status = $2,
	decided_by = $3,
	decided_at = NOW()
WHERE id = $1 AND status = 'pending'
RETURNING decided_at
`

func (q *Queries) MarkApprovalEscalated(ctx context.Context, id, decidedBy string) (time.Time, error) {
	row := q.db.QueryRowContext(ctx, markApprovalEscalated, id, model.ApprovalStatusEscalated, decidedBy)
	var decidedAt time.Time
	err := row.Scan(&decidedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, err
	}
	return decidedAt, err
}

const getApprovalByID = `
SELECT
	id, session_id, call_id, status, route, reason, decided_by, comment,
	created_at, decided_at,
	tool, version, args, authority_ref, policy_name,
	approvals_required, approvals_received, comprehension_required,
	on_reject, on_modify, expires_at, policy_runtime
FROM approvals
WHERE id = $1
`

type Approval struct {
	ID                    string          `json:"id"`
	SessionID             string          `json:"session_id"`
	CallID                string          `json:"call_id"`
	Status                string          `json:"status"`
	Route                 string          `json:"route"`
	Reason                string          `json:"reason"`
	DecidedBy             string          `json:"decided_by"`
	Comment               string          `json:"comment"`
	CreatedAt             time.Time       `json:"created_at"`
	DecidedAt             *time.Time      `json:"decided_at"`
	Tool                  string          `json:"tool"`
	Version               string          `json:"version"`
	Args                  json.RawMessage `json:"args"`
	AuthorityRef          string          `json:"authority_ref"`
	PolicyName            string          `json:"policy_name"`
	ApprovalsRequired     int             `json:"approvals_required"`
	ApprovalsReceived     int             `json:"approvals_received"`
	ComprehensionRequired bool            `json:"comprehension_required"`
	OnReject              string          `json:"on_reject"`
	OnModify              string          `json:"on_modify"`
	ExpiresAt             *time.Time      `json:"expires_at"`
	PolicyRuntime         json.RawMessage `json:"policy_runtime"`
}

func (q *Queries) GetApproval(ctx context.Context, id string) (Approval, error) {
	row := q.db.QueryRowContext(ctx, getApprovalByID, id)
	return scanApproval(row)
}

const getPendingApprovalBySession = `
SELECT
	id, session_id, call_id, status, route, reason, decided_by, comment,
	created_at, decided_at,
	tool, version, args, authority_ref, policy_name,
	approvals_required, approvals_received, comprehension_required,
	on_reject, on_modify, expires_at, policy_runtime
FROM approvals
WHERE session_id = $1 AND status = 'pending'
ORDER BY created_at DESC
LIMIT 1
`

func (q *Queries) GetPendingApprovalBySession(ctx context.Context, sessionID string) (Approval, error) {
	row := q.db.QueryRowContext(ctx, getPendingApprovalBySession, sessionID)
	return scanApproval(row)
}

func scanApproval(row *sql.Row) (Approval, error) {
	var a Approval
	var decidedAt, expiresAt sql.NullTime
	var args, policyRuntime []byte
	err := row.Scan(
		&a.ID, &a.SessionID, &a.CallID, &a.Status, &a.Route, &a.Reason,
		&a.DecidedBy, &a.Comment, &a.CreatedAt, &decidedAt,
		&a.Tool, &a.Version, &args, &a.AuthorityRef, &a.PolicyName,
		&a.ApprovalsRequired, &a.ApprovalsReceived, &a.ComprehensionRequired,
		&a.OnReject, &a.OnModify, &expiresAt, &policyRuntime,
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
	if expiresAt.Valid {
		a.ExpiresAt = &expiresAt.Time
	}
	if len(args) > 0 {
		a.Args = json.RawMessage(args)
	}
	if len(policyRuntime) > 0 {
		a.PolicyRuntime = json.RawMessage(policyRuntime)
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

const updateToolInvocationArgs = `
UPDATE tool_invocations
SET args = $2::jsonb, updated_at = NOW()
WHERE call_id = $1
`

func (q *Queries) UpdateToolInvocationArgs(ctx context.Context, callID string, args json.RawMessage) error {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	_, err := q.db.ExecContext(ctx, updateToolInvocationArgs, callID, args)
	return err
}

const listApprovalVotes = `
SELECT decided_by, decision, comment, comprehension_acknowledged, created_at
FROM approval_votes
WHERE approval_id = $1
ORDER BY created_at ASC
`

type ApprovalVote struct {
	DecidedBy                 string    `json:"decided_by"`
	Decision                  string    `json:"decision"`
	Comment                   string    `json:"comment"`
	ComprehensionAcknowledged bool      `json:"comprehension_acknowledged"`
	CreatedAt                 time.Time `json:"created_at"`
}

func (q *Queries) ListApprovalVotes(ctx context.Context, approvalID string) ([]ApprovalVote, error) {
	rows, err := q.db.QueryContext(ctx, listApprovalVotes, approvalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ApprovalVote
	for rows.Next() {
		var v ApprovalVote
		if err := rows.Scan(&v.DecidedBy, &v.Decision, &v.Comment, &v.ComprehensionAcknowledged, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

const listApprovals = `
SELECT
	a.id, a.session_id, a.call_id, a.status, a.route, a.reason, a.decided_by, a.comment,
	a.created_at, a.decided_at,
	a.tool, a.version, a.args, a.authority_ref, a.policy_name,
	a.approvals_required, a.approvals_received, a.comprehension_required,
	a.on_reject, a.on_modify, a.expires_at, a.policy_runtime
FROM approvals a
JOIN sessions s ON s.id = a.session_id
JOIN agent_versions av ON av.id = s.agent_version_id
LEFT JOIN agents ag ON ag.id = av.agent_id
WHERE ($1::text = '' OR a.status = $1)
	AND ($2::text = '' OR a.route = $2)
	AND ($3::text = '' OR a.session_id = $3)
	AND ($4::text = '' OR COALESCE(ag.namespace, av.manifest->'metadata'->>'namespace') = $4)
	AND ($5::text = '' OR COALESCE(ag.name, av.manifest->'metadata'->>'name') = $5)
ORDER BY a.created_at DESC
`

type ListApprovalsParams struct {
	Status         string
	Route          string
	SessionID      string
	AgentNamespace string
	AgentName      string
}

func (q *Queries) ListApprovals(ctx context.Context, arg ListApprovalsParams) ([]Approval, error) {
	rows, err := q.db.QueryContext(ctx, listApprovals,
		arg.Status, arg.Route, arg.SessionID, arg.AgentNamespace, arg.AgentName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Approval
	for rows.Next() {
		var a Approval
		var decidedAt, expiresAt sql.NullTime
		var args, policyRuntime []byte
		if err := rows.Scan(
			&a.ID, &a.SessionID, &a.CallID, &a.Status, &a.Route, &a.Reason,
			&a.DecidedBy, &a.Comment, &a.CreatedAt, &decidedAt,
			&a.Tool, &a.Version, &args, &a.AuthorityRef, &a.PolicyName,
			&a.ApprovalsRequired, &a.ApprovalsReceived, &a.ComprehensionRequired,
			&a.OnReject, &a.OnModify, &expiresAt, &policyRuntime,
		); err != nil {
			return nil, err
		}
		if decidedAt.Valid {
			a.DecidedAt = &decidedAt.Time
		}
		if expiresAt.Valid {
			a.ExpiresAt = &expiresAt.Time
		}
		if len(args) > 0 {
			a.Args = json.RawMessage(args)
		}
		if len(policyRuntime) > 0 {
			a.PolicyRuntime = json.RawMessage(policyRuntime)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
