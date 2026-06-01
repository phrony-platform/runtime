package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/phrony-platform/runtime/internal/model"
)

const agentVersionContentHash = `
SELECT content_hash
FROM agent_versions
WHERE id = $1
`

// GetAgentVersionContentHash returns the manifest content hash for a deployed version.
func (q *Queries) GetAgentVersionContentHash(ctx context.Context, agentVersionID string) (string, error) {
	row := q.db.QueryRowContext(ctx, agentVersionContentHash, agentVersionID)
	var hash string
	err := row.Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return hash, err
}

const insertToolInvocationPending = `
INSERT INTO tool_invocations (
	call_id,
	session_id,
	agent_version_id,
	turn,
	tool,
	version,
	args,
	status
) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)
ON CONFLICT (call_id) DO NOTHING
RETURNING call_id
`

type InsertToolInvocationPendingParams struct {
	CallID         string
	SessionID      string
	AgentVersionID string
	Turn           int
	Tool           string
	Version        string
	Args           json.RawMessage
	Status         string
}

func (q *Queries) InsertToolInvocationPending(ctx context.Context, arg InsertToolInvocationPendingParams) (string, error) {
	if arg.Status == "" {
		arg.Status = "pending"
	}
	args := arg.Args
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	row := q.db.QueryRowContext(ctx, insertToolInvocationPending,
		arg.CallID,
		arg.SessionID,
		arg.AgentVersionID,
		arg.Turn,
		arg.Tool,
		arg.Version,
		args,
		arg.Status,
	)
	var callID string
	err := row.Scan(&callID)
	if errors.Is(err, sql.ErrNoRows) {
		return arg.CallID, nil
	}
	return callID, err
}

const insertToolInvocationDispatched = `
INSERT INTO tool_invocations (
	call_id,
	session_id,
	agent_version_id,
	turn,
	tool,
	version,
	args,
	status,
	worker_identity,
	image_digest,
	descriptor_hash,
	manifest_content_hash,
	attempt,
	dispatched_at
) VALUES (
	$1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11, $12, $13, NOW()
)
ON CONFLICT (call_id) DO UPDATE SET
	status = EXCLUDED.status,
	worker_identity = EXCLUDED.worker_identity,
	image_digest = EXCLUDED.image_digest,
	descriptor_hash = EXCLUDED.descriptor_hash,
	manifest_content_hash = EXCLUDED.manifest_content_hash,
	attempt = tool_invocations.attempt + 1,
	dispatched_at = NOW(),
	updated_at = NOW()
RETURNING call_id
`

type InsertToolInvocationDispatchedParams struct {
	CallID              string
	SessionID           string
	AgentVersionID      string
	Turn                int
	Tool                string
	Version             string
	Args                json.RawMessage
	Status              string
	WorkerIdentity      string
	ImageDigest         string
	DescriptorHash      string
	ManifestContentHash string
	Attempt             int
}

func (q *Queries) InsertToolInvocationDispatched(ctx context.Context, arg InsertToolInvocationDispatchedParams) (string, error) {
	if arg.Status == "" {
		arg.Status = "dispatched"
	}
	if arg.Attempt <= 0 {
		arg.Attempt = 1
	}
	args := arg.Args
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	row := q.db.QueryRowContext(ctx, insertToolInvocationDispatched,
		arg.CallID,
		arg.SessionID,
		arg.AgentVersionID,
		arg.Turn,
		arg.Tool,
		arg.Version,
		args,
		arg.Status,
		arg.WorkerIdentity,
		arg.ImageDigest,
		arg.DescriptorHash,
		arg.ManifestContentHash,
		arg.Attempt,
	)
	var callID string
	err := row.Scan(&callID)
	return callID, err
}

const completeToolInvocation = `
UPDATE tool_invocations
SET status = $2,
	result = $3::jsonb,
	error_code = $4,
	error_message = $5,
	completed_at = NOW(),
	updated_at = NOW()
WHERE call_id = $1
RETURNING updated_at
`

type CompleteToolInvocationParams struct {
	CallID       string
	Status       string
	Result       json.RawMessage
	ErrorCode    *string
	ErrorMessage *string
}

func (q *Queries) CompleteToolInvocation(ctx context.Context, arg CompleteToolInvocationParams) (time.Time, error) {
	var result any
	if arg.Result != nil {
		result = arg.Result
	}
	row := q.db.QueryRowContext(ctx, completeToolInvocation,
		arg.CallID,
		arg.Status,
		result,
		arg.ErrorCode,
		arg.ErrorMessage,
	)
	var updatedAt time.Time
	err := row.Scan(&updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, err
	}
	return updatedAt, err
}

const getToolInvocation = `
SELECT
	call_id,
	session_id,
	agent_version_id,
	turn,
	tool,
	version,
	args,
	result,
	status,
	worker_identity,
	image_digest,
	descriptor_hash,
	manifest_content_hash,
	attempt,
	error_code,
	error_message,
	created_at,
	updated_at,
	dispatched_at,
	completed_at
FROM tool_invocations
WHERE call_id = $1
`

type ToolInvocation struct {
	CallID              string          `json:"call_id"`
	SessionID           string          `json:"session_id"`
	AgentVersionID      string          `json:"agent_version_id"`
	Turn                int             `json:"turn"`
	Tool                string          `json:"tool"`
	Version             string          `json:"version"`
	Args                json.RawMessage `json:"args"`
	Result              json.RawMessage `json:"result"`
	Status              string          `json:"status"`
	WorkerIdentity      string          `json:"worker_identity"`
	ImageDigest         string          `json:"image_digest"`
	DescriptorHash      string          `json:"descriptor_hash"`
	ManifestContentHash string          `json:"manifest_content_hash"`
	Attempt             int             `json:"attempt"`
	ErrorCode           *string         `json:"error_code"`
	ErrorMessage        *string         `json:"error_message"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
	DispatchedAt        *time.Time      `json:"dispatched_at"`
	CompletedAt         *time.Time      `json:"completed_at"`
}

const listUnfinishedInvocationsBySession = `
SELECT
	call_id,
	session_id,
	agent_version_id,
	turn,
	tool,
	version,
	args,
	result,
	status,
	worker_identity,
	image_digest,
	descriptor_hash,
	manifest_content_hash,
	attempt,
	error_code,
	error_message,
	created_at,
	updated_at,
	dispatched_at,
	completed_at
FROM tool_invocations
WHERE session_id = $1
	AND status IN ('pending', 'queued', 'awaiting_approval', 'dispatched')
ORDER BY turn ASC, call_id ASC
`

func scanToolInvocation(row interface {
	Scan(dest ...any) error
}) (ToolInvocation, error) {
	var inv ToolInvocation
	var result sql.NullString
	var errCode, errMsg sql.NullString
	var dispatchedAt, completedAt sql.NullTime
	err := row.Scan(
		&inv.CallID,
		&inv.SessionID,
		&inv.AgentVersionID,
		&inv.Turn,
		&inv.Tool,
		&inv.Version,
		&inv.Args,
		&result,
		&inv.Status,
		&inv.WorkerIdentity,
		&inv.ImageDigest,
		&inv.DescriptorHash,
		&inv.ManifestContentHash,
		&inv.Attempt,
		&errCode,
		&errMsg,
		&inv.CreatedAt,
		&inv.UpdatedAt,
		&dispatchedAt,
		&completedAt,
	)
	if err != nil {
		return ToolInvocation{}, err
	}
	if result.Valid {
		inv.Result = json.RawMessage(result.String)
	}
	if errCode.Valid {
		inv.ErrorCode = &errCode.String
	}
	if errMsg.Valid {
		inv.ErrorMessage = &errMsg.String
	}
	if dispatchedAt.Valid {
		inv.DispatchedAt = &dispatchedAt.Time
	}
	if completedAt.Valid {
		inv.CompletedAt = &completedAt.Time
	}
	return inv, nil
}

func (q *Queries) ListUnfinishedInvocationsBySession(ctx context.Context, sessionID string) ([]ToolInvocation, error) {
	rows, err := q.db.QueryContext(ctx, listUnfinishedInvocationsBySession, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ToolInvocation
	for rows.Next() {
		inv, err := scanToolInvocation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

const listSessionsForRecovery = `
SELECT id, agent_version_id, input, status, output, error, history, created_at, updated_at
FROM sessions
WHERE status IN ('pending', 'running', 'awaiting_tool', 'awaiting_approval')
ORDER BY created_at ASC
`

func (q *Queries) ListSessionsForRecovery(ctx context.Context) ([]Session, error) {
	rows, err := q.db.QueryContext(ctx, listSessionsForRecovery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		var s Session
		var output sql.NullString
		var errText sql.NullString
		if err := rows.Scan(
			&s.ID,
			&s.AgentVersionID,
			&s.Input,
			&s.Status,
			&output,
			&errText,
			&s.History,
			&s.CreatedAt,
			&s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if output.Valid {
			s.Output = json.RawMessage(output.String)
		}
		if errText.Valid {
			s.Error = &errText.String
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

const markToolInvocationIndeterminate = `
UPDATE tool_invocations
SET status = $2,
	error_code = $3,
	error_message = $4,
	updated_at = NOW()
WHERE call_id = $1
	AND status = 'dispatched'
RETURNING call_id
`

func (q *Queries) MarkToolInvocationIndeterminate(ctx context.Context, callID, reason string) error {
	code := "indeterminate"
	row := q.db.QueryRowContext(ctx, markToolInvocationIndeterminate, callID, model.ToolInvocationIndeterminate, code, reason)
	var id string
	err := row.Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func (q *Queries) GetToolInvocation(ctx context.Context, callID string) (ToolInvocation, error) {
	row := q.db.QueryRowContext(ctx, getToolInvocation, callID)
	inv, err := scanToolInvocation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ToolInvocation{}, err
	}
	return inv, err
}
