package store

import (
	"encoding/json"
	"time"
)

type Agent struct {
	ID        string          `json:"id"`
	Namespace string          `json:"namespace"`
	Name      string          `json:"name"`
	Owner     string          `json:"owner"`
	Labels    json.RawMessage `json:"labels"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type AgentVersion struct {
	ID          string          `json:"id"`
	AgentID     string          `json:"agent_id"`
	Version     string          `json:"version"`
	ContentHash string          `json:"content_hash"`
	Manifest    json.RawMessage `json:"manifest"`
	DeployedAt  time.Time       `json:"deployed_at"`
}

type Session struct {
	ID             string          `json:"id"`
	AgentVersionID string          `json:"agent_version_id"`
	Input          json.RawMessage `json:"input"`
	Status         string          `json:"status"`
	Error          *string         `json:"error"`
	RootSessionID  string          `json:"root_session_id"`
	EventSeq       int             `json:"event_seq"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type Event struct {
	ID             int64           `json:"id"`
	SessionID      string          `json:"session_id"`
	RootSessionID  string          `json:"root_session_id"`
	Seq            int             `json:"seq"`
	TS             time.Time       `json:"ts"`
	Type           string          `json:"type"`
	Turn           *int            `json:"turn"`
	CallID         *string         `json:"call_id"`
	ChildSessionID *string         `json:"child_session_id"`
	Actor          string          `json:"actor"`
	Payload        json.RawMessage `json:"payload"`
}

type RuntimeMetum struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
