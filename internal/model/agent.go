package model

import "time"

// AgentRecord is a deployed agent identity row (namespace + name).
type AgentRecord struct {
	ID         string     `db:"id"`
	Namespace  string     `db:"namespace"`
	Name       string     `db:"name"`
	Owner      string     `db:"owner"`
	Labels     []byte     `db:"labels"`
	CreatedAt  time.Time  `db:"created_at"`
	UpdatedAt  time.Time  `db:"updated_at"`
	ArchivedAt *time.Time `db:"archived_at"`
}

func (AgentRecord) TableName() string { return "agents" }

// AgentVersionRecord stores one semver-labeled resolved manifest snapshot.
type AgentVersionRecord struct {
	ID           string     `db:"id"`
	AgentID      string     `db:"agent_id"`
	Version      string     `db:"version"`
	ContentHash  string     `db:"content_hash"`
	Manifest     []byte     `db:"manifest"`
	DeployedAt   time.Time  `db:"deployed_at"`
	DeprecatedAt *time.Time `db:"deprecated_at"`
}

func (AgentVersionRecord) TableName() string { return "agent_versions" }
