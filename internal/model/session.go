package model

import "time"

// SessionStatusPending means the session is bound to a deployed agent version
// but model execution has not started (Phase 4).
const SessionStatusPending = "pending"

// SessionRecord links a client session id to a deployed agent version snapshot.
type SessionRecord struct {
	ID             string    `db:"id"`
	AgentVersionID string    `db:"agent_version_id"`
	Input          []byte    `db:"input"`
	Status         string    `db:"status"`
	CreatedAt      time.Time `db:"created_at"`
	UpdatedAt      time.Time `db:"updated_at"`
}

func (SessionRecord) TableName() string { return "sessions" }
