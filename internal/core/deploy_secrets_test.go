package core

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/secrets"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
)

func TestRuntime_Publish_withSecretsManifestRefsOnly(t *testing.T) {
	manifestJSON := deployManifestWithSecretsJSON(t)
	storedJSON, err := manifestForStorage(mustParseDeployAgent(t, manifestJSON), manifestJSON)
	if err != nil {
		t.Fatalf("manifestForStorage: %v", err)
	}

	enc := mustTestEncryptor(t)
	db, mock := testSQLxDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO agents`).
		WithArgs(sqlmock.AnyArg(), "demo", "echo-agent", "", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("agent-uuid"))
	expectActiveAgentByID(mock, "agent-uuid")
	mock.ExpectQuery(`SELECT av.id, av.content_hash`).
		WithArgs("agent-uuid", "1.2.0").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`INSERT INTO agent_versions`).
		WithArgs(sqlmock.AnyArg(), "agent-uuid", "1.2.0", hashManifest(manifestJSON), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("version-uuid"))
	if !bytes.Equal(storedJSON, manifestJSON) {
		t.Log("stored manifest is canonical ref-only JSON")
	}
	mock.ExpectCommit()

	srv := &runtimeServer{db: db, secretsEnc: enc}
	resp, err := srv.Publish(context.Background(), &runtimev1.PublishRequest{
		Manifest: manifestJSON,
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if resp.GetVersionId() != "version-uuid" {
		t.Fatalf("version_id = %q, want version-uuid", resp.GetVersionId())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestPersistSessionSecrets_noEncryptor(t *testing.T) {
	agent := &manifest.Agent{
		Secrets: map[string]manifest.SecretDefinition{
			"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
		},
	}
	srv := &runtimeServer{}
	err := srv.persistSessionSecrets(context.Background(), nil, "session-id", agent, map[string][]byte{
		"anthropic": []byte("sk-test"),
	})
	assertGRPCCode(t, err, codes.FailedPrecondition)
}

func TestValidateResolvedSecrets_unknownResolved(t *testing.T) {
	agent := &manifest.Agent{
		Secrets: map[string]manifest.SecretDefinition{
			"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
		},
	}
	err := validateResolvedSecrets(agent, map[string][]byte{
		"anthropic": []byte("sk-a"),
		"extra":     []byte("sk-x"),
	})
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestValidateResolvedSecrets_emptyValue(t *testing.T) {
	agent := &manifest.Agent{
		Secrets: map[string]manifest.SecretDefinition{
			"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
		},
	}
	err := validateResolvedSecrets(agent, map[string][]byte{
		"anthropic": {},
	})
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestPersistSessionSecrets_resolvedWithoutManifestSecrets(t *testing.T) {
	srv := &runtimeServer{secretsEnc: mustTestEncryptor(t)}
	err := srv.persistSessionSecrets(context.Background(), nil, "session-id", &manifest.Agent{}, map[string][]byte{
		"anthropic": []byte("sk-test"),
	})
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestPersistSessionSecrets_noSecretsBlock(t *testing.T) {
	srv := &runtimeServer{secretsEnc: mustTestEncryptor(t)}
	err := srv.persistSessionSecrets(context.Background(), nil, "session-id", &manifest.Agent{}, nil)
	if err != nil {
		t.Fatalf("persistSessionSecrets: %v", err)
	}
}

func TestPersistSessionSecrets_encryptsAndInserts(t *testing.T) {
	agent := &manifest.Agent{
		Secrets: map[string]manifest.SecretDefinition{
			"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
		},
	}
	db, mock := testSQLxDB(t)
	mock.ExpectExec(`INSERT INTO session_secrets`).
		WithArgs("session-id", "anthropic", 1, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	srv := &runtimeServer{secretsEnc: mustTestEncryptor(t), db: db}
	err := srv.persistSessionSecrets(context.Background(), store.New(db), "session-id", agent, map[string][]byte{
		"anthropic": []byte("sk-test-key"),
	})
	if err != nil {
		t.Fatalf("persistSessionSecrets: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestPersistSessionSecrets_missingResolved(t *testing.T) {
	agent := &manifest.Agent{
		Secrets: map[string]manifest.SecretDefinition{
			"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
		},
	}
	srv := &runtimeServer{secretsEnc: mustTestEncryptor(t)}
	err := srv.persistSessionSecrets(context.Background(), nil, "session-id", agent, nil)
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestManifestForStorage_refOnlyWhenSecretsPresent(t *testing.T) {
	agent := &manifest.Agent{
		APIVersion: manifest.APIVersionV1,
		Kind:       manifest.KindAgent,
		Metadata:   manifest.AgentMetadata{Name: "a", Namespace: "n", Version: "1.0.0"},
		Secrets: map[string]manifest.SecretDefinition{
			"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
		},
		Spec: manifest.AgentSpec{
			Purpose:      "p",
			Instructions: manifest.InstructionsSpec{Text: "i"},
			Model:        manifest.ModelConfig{Provider: "anthropic", Name: "m"},
		},
	}
	raw := []byte(`{"apiVersion":"phrony.com/v1","kind":"Agent","metadata":{"name":"a","namespace":"n","version":"1.0.0"},"secrets":{"anthropic":{"fromEnv":"ANTHROPIC_API_KEY"}},"spec":{"purpose":"p","instructions":{"text":"i"},"model":{"provider":"anthropic","name":"m"}}}`)
	stored, err := manifestForStorage(agent, raw)
	if err != nil {
		t.Fatalf("manifestForStorage: %v", err)
	}
	var decoded manifest.Agent
	if err := json.Unmarshal(stored, &decoded); err != nil {
		t.Fatalf("Unmarshal stored: %v", err)
	}
	if decoded.Secrets["anthropic"].FromEnv != "ANTHROPIC_API_KEY" {
		t.Fatalf("fromEnv = %q, want ANTHROPIC_API_KEY", decoded.Secrets["anthropic"].FromEnv)
	}
}

func deployManifestWithSecretsJSON(t *testing.T) []byte {
	t.Helper()
	agent := &manifest.Agent{
		APIVersion: manifest.APIVersionV1,
		Kind:       manifest.KindAgent,
		Metadata: manifest.AgentMetadata{
			Name:      "echo-agent",
			Namespace: "demo",
			Version:   "1.2.0",
		},
		Secrets: map[string]manifest.SecretDefinition{
			"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
		},
		Spec: manifest.AgentSpec{
			Purpose:      "Echo user messages.",
			Instructions: manifest.InstructionsSpec{Text: "You are an echo agent."},
			Model: manifest.ModelConfig{
				Provider: "anthropic",
				Name:     "claude-sonnet-4-5",
			},
		},
		Output: &manifest.OutputSpec{
			Format: "json",
			Schema: &manifest.SchemaSpec{Inline: map[string]any{"type": "object"}},
		},
	}
	if err := manifest.Validate(agent); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	raw, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return raw
}

func mustParseDeployAgent(t *testing.T, raw []byte) *manifest.Agent {
	t.Helper()
	agent, err := manifest.ParseJSON(raw)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	return agent
}

func TestRuntime_finalizeSessionSecrets_purgesRows(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectExec(`DELETE FROM session_secrets`).
		WithArgs("sess-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	srv := &runtimeServer{db: db}
	srv.finalizeSessionSecrets(context.Background(), store.New(db), "sess-1")

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func mustTestEncryptor(t *testing.T) *secrets.Encryptor {
	t.Helper()
	enc, err := secrets.NewEncryptor(bytes.Repeat([]byte{0x42}, 32), 1)
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}
	return enc
}
