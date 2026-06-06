package core

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/provider"
)

const toolTestPostgresDSN = "postgres://phrony_runtime:phrony_runtime@localhost:5432/phrony_runtime?sslmode=disable"

func openToolTestPostgres(t *testing.T) *sqlx.DB {
	t.Helper()
	dsn := os.Getenv("RUNTIME_DATABASE_URL")
	if dsn == "" {
		dsn = toolTestPostgresDSN
	}
	db, err := sqlx.Connect("pgx", dsn)
	if err != nil {
		t.Skipf("postgres not available: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Migrate(db); err != nil {
		t.Skipf("Migrate: %v", err)
	}
	return db
}

func insertToolE2EAgentFixture(t *testing.T, db *sqlx.DB, namespace string) (agentID, agentVersionID string, manifestJSON []byte) {
	t.Helper()
	agentID = uuid.NewString()
	agentVersionID = uuid.NewString()
	agent := e2eWeatherAgent(nil)
	var err error
	manifestJSON, err = json.Marshal(agent)
	if err != nil {
		t.Fatalf("Marshal manifest: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO agents (id, namespace, name, owner, labels)
		VALUES ($1, $2, 'weather-agent', 'e2e', '{}'::jsonb)
	`, agentID, namespace); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO agent_versions (id, agent_id, version, content_hash, manifest)
		VALUES ($1, $2, '1.0.0', 'hash-e2e', $3::jsonb)
	`, agentVersionID, agentID, manifestJSON); err != nil {
		t.Fatalf("insert agent_version: %v", err)
	}
	return agentID, agentVersionID, manifestJSON
}

func insertToolE2ESessionAwaitingTool(
	t *testing.T,
	db *sqlx.DB,
	sessionID, agentVersionID string,
	history []provider.Message,
) {
	t.Helper()
	_ = history
	if _, err := db.Exec(`
		INSERT INTO sessions (id, agent_version_id, input, status, root_session_id)
		VALUES ($1, $2, '{"message":"weather?"}'::jsonb, $3, $1)
	`, sessionID, agentVersionID, model.SessionStatusAwaitingTool); err != nil {
		t.Fatalf("insert session: %v", err)
	}
}

func insertQueuedToolInvocation(
	t *testing.T,
	db *sqlx.DB,
	callID, sessionID, agentVersionID string,
	turn, index int,
) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO tool_invocations (
			call_id, session_id, agent_version_id, turn, tool, version, args, status
		) VALUES ($1, $2, $3, $4, 'weather.get-forecast', '1.0.0', $5::jsonb, $6)
	`, callID, sessionID, agentVersionID, turn,
		json.RawMessage(`{"city":"NYC","i":`+strconv.Itoa(index)+`}`),
		model.ToolInvocationQueued,
	); err != nil {
		t.Fatalf("insert tool_invocation %s: %v", callID, err)
	}
}
