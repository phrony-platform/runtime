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

type RuntimeMetum struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
