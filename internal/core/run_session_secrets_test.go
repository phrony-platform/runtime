package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/providertest"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
)

func expectCreateRunSessionWithSecretsMocks(t *testing.T, mock sqlmock.Sqlmock, versionID string, input []byte) {
	t.Helper()
	manifest := deployManifestWithSecretsJSON(t)
	mock.ExpectQuery(`SELECT manifest`).
		WithArgs(versionID).
		WillReturnRows(sqlmock.NewRows([]string{"manifest"}).AddRow(manifest))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO sessions`).
		WithArgs(sqlmock.AnyArg(), versionID, input, model.SessionStatusRunning, nil, 0, "").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("generated-session"))
	mock.ExpectExec(`INSERT INTO session_secrets`).
		WithArgs(sqlmock.AnyArg(), "anthropic", 1, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
}

func expectCreateRunSessionMissingSecretsMocks(t *testing.T, mock sqlmock.Sqlmock, versionID string, input []byte) {
	t.Helper()
	manifest := deployManifestWithSecretsJSON(t)
	mock.ExpectQuery(`SELECT manifest`).
		WithArgs(versionID).
		WillReturnRows(sqlmock.NewRows([]string{"manifest"}).AddRow(manifest))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO sessions`).
		WithArgs(sqlmock.AnyArg(), versionID, input, model.SessionStatusRunning, nil, 0, "").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("generated-session"))
	mock.ExpectRollback()
}

func TestValidateResolvedSecrets_missingKey(t *testing.T) {
	agent := &manifest.Agent{
		Secrets: map[string]manifest.SecretDefinition{
			"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
		},
	}
	err := validateResolvedSecrets(agent, map[string][]byte{})
	assertGRPCCode(t, err, codes.InvalidArgument)
	if !strings.Contains(statusMessage(t, err), `missing resolved secret "anthropic"`) {
		t.Fatalf("error = %v, want missing anthropic", err)
	}
}

func TestRuntime_createRunSession_missingResolvedSecrets(t *testing.T) {
	db, mock := testSQLxDB(t)
	expectCreateRunSessionMissingSecretsMocks(t, mock, "version-uuid", []byte("{}"))

	srv := &runtimeServer{db: db, secretsEnc: mustTestEncryptor(t)}
	_, err := srv.createRunSession(context.Background(), "version-uuid", nil, []byte("{}"), nil)
	assertGRPCCode(t, err, codes.InvalidArgument)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_createRunSession_persistsSessionSecrets(t *testing.T) {
	db, mock := testSQLxDB(t)
	expectCreateRunSessionWithSecretsMocks(t, mock, "version-uuid", []byte(`{"message":"hi"}`))

	srv := &runtimeServer{db: db, secretsEnc: mustTestEncryptor(t)}
	sessionID, err := srv.createRunSession(context.Background(), "version-uuid", nil, []byte(`{"message":"hi"}`), map[string][]byte{
		"anthropic": []byte("sk-test-key"),
	})
	if err != nil {
		t.Fatalf("createRunSession: %v", err)
	}
	if !strings.HasPrefix(sessionID, "run_") {
		t.Fatalf("session_id = %q, want run_ prefix", sessionID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_RunSession_missingResolvedSecrets(t *testing.T) {
	db, mock := testSQLxDB(t)
	expectActiveDeployment(mock, "demo", "echo-agent", "version-uuid", "1.2.0")
	expectCreateRunSessionMissingSecretsMocks(t, mock, "version-uuid", []byte("{}"))

	srv := &runtimeServer{db: db, secretsEnc: mustTestEncryptor(t)}
	_, err := srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo-agent"},
	})
	assertGRPCCode(t, err, codes.InvalidArgument)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_RunSession_withResolvedSecrets(t *testing.T) {
	db, mock := testSQLxDB(t)
	expectActiveDeployment(mock, "demo", "echo-agent", "version-uuid", "1.2.0")
	expectCreateRunSessionWithSecretsMocks(t, mock, "version-uuid", []byte("{}"))

	srv := &runtimeServer{
		db:                          db,
		secretsEnc:                  mustTestEncryptor(t),
		startRunSessionBackgroundFn: func(string, string, json.RawMessage) {},
	}
	resp, err := srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: "demo", Name: "echo-agent"},
		ResolvedSecrets: map[string][]byte{
			"anthropic": []byte("sk-test-key"),
		},
	})
	if err != nil {
		t.Fatalf("RunSession: %v", err)
	}
	if resp.GetStatus() != model.SessionStatusRunning {
		t.Fatalf("status = %q, want %q", resp.GetStatus(), model.SessionStatusRunning)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_persistDetachedSessionAfterTurn_retainsSecrets(t *testing.T) {
	db, mock := testSQLxDB(t)
	now := time.Now()
	output := json.RawMessage(`{"message":"ok","stop_reason":"end_turn"}`)
	history := json.RawMessage(`[]`)
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-park", model.SessionStatusAwaitingInput, output, nil, history).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))

	srv := &runtimeServer{db: db}
	state := &interactiveSessionState{sessionID: "sess-park"}
	err := srv.persistDetachedSessionAfterTurn(
		context.Background(),
		store.New(db),
		"sess-park",
		state,
		output,
		history,
	)
	if err != nil {
		t.Fatalf("persistDetachedSessionAfterTurn: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_loadSessionVersion_decryptsSessionSecrets(t *testing.T) {
	manifestJSON := deployManifestWithSecretsJSON(t)
	enc := mustTestEncryptor(t)
	plaintext := []byte("sk-test-key")
	sealed, err := enc.Encrypt("sess-2", "anthropic", plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`SELECT manifest`).
		WithArgs("version-uuid").
		WillReturnRows(sqlmock.NewRows([]string{"manifest"}).AddRow(manifestJSON))
	mock.ExpectQuery(`SELECT key_version, nonce, ciphertext`).
		WithArgs("sess-2", "anthropic").
		WillReturnRows(sqlmock.NewRows([]string{"key_version", "nonce", "ciphertext"}).
			AddRow(sealed.KeyVersion, sealed.Nonce, sealed.Ciphertext))

	srv := &runtimeServer{db: db, secretsEnc: enc}
	ver, err := srv.loadSessionVersion(context.Background(), store.New(db), "sess-2", "version-uuid")
	if err != nil {
		t.Fatalf("loadSessionVersion: %v", err)
	}
	if ver.AgentVersionID != "version-uuid" {
		t.Fatalf("agent_version_id = %q", ver.AgentVersionID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_loadSessionVersion_missingSessionSecrets(t *testing.T) {
	manifestJSON := deployManifestWithSecretsJSON(t)

	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`SELECT manifest`).
		WithArgs("version-uuid").
		WillReturnRows(sqlmock.NewRows([]string{"manifest"}).AddRow(manifestJSON))
	mock.ExpectQuery(`SELECT key_version, nonce, ciphertext`).
		WithArgs("sess-missing", "anthropic").
		WillReturnError(sql.ErrNoRows)

	srv := &runtimeServer{db: db, secretsEnc: mustTestEncryptor(t)}
	_, err := srv.loadSessionVersion(context.Background(), store.New(db), "sess-missing", "version-uuid")
	if err == nil {
		t.Fatal("loadSessionVersion() = nil, want error")
	}
	if !strings.Contains(err.Error(), "decrypt secret") {
		t.Fatalf("error = %v, want decrypt failure", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_reconcileRecoveredSession_awaitingToolUsesSessionID(t *testing.T) {
	db, _ := testSQLxDB(t)
	var loadedSessionID string
	srv := &runtimeServer{
		db: db,
		loadSessionVersionFn: func(_ context.Context, _ *store.Queries, sessionID, agentVersionID string) (*executor.Version, error) {
			loadedSessionID = sessionID
			if agentVersionID != "version-uuid" {
				t.Fatalf("agent_version_id = %q, want version-uuid", agentVersionID)
			}
			return executor.NewVersionWithProvider(agentVersionID, &manifest.Agent{
				Spec: manifest.AgentSpec{Model: manifest.ModelConfig{Provider: provider.IDAnthropic, Name: "m"}},
			}, providertest.DeltaCompleted()), nil
		},
	}
	session := store.Session{
		ID:             "sess-await-tool",
		AgentVersionID: "version-uuid",
		Status:         model.SessionStatusAwaitingTool,
	}
	srv.reconcileRecoveredSession(context.Background(), store.New(db), session)
	if loadedSessionID != "sess-await-tool" {
		t.Fatalf("loaded session_id = %q, want sess-await-tool", loadedSessionID)
	}
}

func TestRuntime_loadSessionVersion_secondTurnAfterAwaitingInput(t *testing.T) {
	manifestJSON := deployManifestWithSecretsJSON(t)
	enc := mustTestEncryptor(t)
	plaintext := []byte("sk-test-key")
	sealed, err := enc.Encrypt("sess-park", "anthropic", plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	db, mock := testSQLxDB(t)
	now := time.Now()
	output := json.RawMessage(`{"message":"ok","stop_reason":"end_turn"}`)
	history := json.RawMessage(`[]`)
	mock.ExpectQuery(`UPDATE sessions`).
		WithArgs("sess-park", model.SessionStatusAwaitingInput, output, nil, history).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))

	srv := &runtimeServer{db: db, secretsEnc: enc}
	state := &interactiveSessionState{sessionID: "sess-park"}
	if err := srv.persistDetachedSessionAfterTurn(context.Background(), store.New(db), "sess-park", state, output, history); err != nil {
		t.Fatalf("persistDetachedSessionAfterTurn: %v", err)
	}

	mock.ExpectQuery(`SELECT manifest`).
		WithArgs("version-uuid").
		WillReturnRows(sqlmock.NewRows([]string{"manifest"}).AddRow(manifestJSON))
	mock.ExpectQuery(`SELECT key_version, nonce, ciphertext`).
		WithArgs("sess-park", "anthropic").
		WillReturnRows(sqlmock.NewRows([]string{"key_version", "nonce", "ciphertext"}).
			AddRow(sealed.KeyVersion, sealed.Nonce, sealed.Ciphertext))

	ver, err := srv.loadSessionVersion(context.Background(), store.New(db), "sess-park", "version-uuid")
	if err != nil {
		t.Fatalf("loadSessionVersion after awaiting_input: %v", err)
	}
	if ver == nil {
		t.Fatal("version is nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
