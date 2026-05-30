package model

import "time"

const (
	// SessionStatusPending means the session is bound to a deployed agent version
	// but model execution has not started.
	SessionStatusPending = "pending"
	// SessionStatusRunning means the executor is actively streaming a completion.
	SessionStatusRunning = "running"
	// SessionStatusAwaitingInput means the agent is waiting for user input on the stream.
	SessionStatusAwaitingInput = "awaiting_input"
	// SessionStatusCompleted means the session finished successfully.
	SessionStatusCompleted = "completed"
	// SessionStatusFailed means the session ended with an error.
	SessionStatusFailed = "failed"
)

// SessionRecord links a client session id to a deployed agent version snapshot.
type SessionRecord struct {
	ID             string    `db:"id"`
	AgentVersionID string    `db:"agent_version_id"`
	Input          []byte    `db:"input"`
	Status         string    `db:"status"`
	Output         []byte    `db:"output"`
	Error          *string   `db:"error"`
	CreatedAt      time.Time `db:"created_at"`
	UpdatedAt      time.Time `db:"updated_at"`
}

func (SessionRecord) TableName() string { return "sessions" }
