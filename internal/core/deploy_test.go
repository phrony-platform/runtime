package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/manifest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRuntime_Deploy_success(t *testing.T) {
	manifestJSON := resolvedDeployManifestJSON(t)

	db, mock := testSQLxDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO agents`).
		WithArgs(sqlmock.AnyArg(), "demo", "echo-agent", "", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("agent-uuid"))
	mock.ExpectQuery(`INSERT INTO agent_versions`).
		WithArgs(sqlmock.AnyArg(), "agent-uuid", "1.2.0", hashManifest(manifestJSON), manifestJSON).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("version-uuid"))
	mock.ExpectCommit()

	srv := &runtimeServer{db: db}
	resp, err := srv.Deploy(context.Background(), &runtimev1.DeployRequest{Manifest: manifestJSON})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if resp.GetAgentId() != "agent-uuid" {
		t.Fatalf("agent_id = %q, want agent-uuid", resp.GetAgentId())
	}
	if resp.GetVersionId() != "version-uuid" {
		t.Fatalf("version_id = %q, want version-uuid", resp.GetVersionId())
	}
	if resp.GetContentHash() != hashManifest(manifestJSON) {
		t.Fatalf("content_hash = %q, want %q", resp.GetContentHash(), hashManifest(manifestJSON))
	}
	if resp.GetNamespace() != "demo" || resp.GetName() != "echo-agent" || resp.GetVersion() != "1.2.0" {
		t.Fatalf("deploy identity = %s/%s@%s, want demo/echo-agent@1.2.0",
			resp.GetNamespace(), resp.GetName(), resp.GetVersion())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_Deploy_emptyManifest(t *testing.T) {
	srv := &runtimeServer{db: testServeDB(t)}
	_, err := srv.Deploy(context.Background(), &runtimev1.DeployRequest{})
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestRuntime_Deploy_invalidManifest(t *testing.T) {
	srv := &runtimeServer{db: testServeDB(t)}
	_, err := srv.Deploy(context.Background(), &runtimev1.DeployRequest{Manifest: []byte(`{"apiVersion":"phrony.dev/v1"}`)})
	assertGRPCCode(t, err, codes.InvalidArgument)
	if !strings.Contains(statusMessage(t, err), "invalid manifest") {
		t.Fatalf("error = %v, want invalid manifest", err)
	}
}

func TestRuntime_Deploy_noDatabase(t *testing.T) {
	srv := &runtimeServer{}
	_, err := srv.Deploy(context.Background(), &runtimev1.DeployRequest{Manifest: []byte(`{}`)})
	assertGRPCCode(t, err, codes.FailedPrecondition)
}

func TestRuntime_Deploy_malformedJSON(t *testing.T) {
	srv := &runtimeServer{db: testServeDB(t)}
	_, err := srv.Deploy(context.Background(), &runtimev1.DeployRequest{Manifest: []byte(`{`)})
	assertGRPCCode(t, err, codes.InvalidArgument)
	if !strings.Contains(statusMessage(t, err), "parse manifest") {
		t.Fatalf("error = %v, want parse manifest", err)
	}
}

func TestRuntime_Deploy_withLabels(t *testing.T) {
	manifestJSON := resolvedDeployManifestJSON(t, deployManifestOpts{
		labels: map[string]string{"team": "platform"},
	})

	db, mock := testSQLxDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO agents`).
		WithArgs(sqlmock.AnyArg(), "demo", "echo-agent", "", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("agent-uuid"))
	mock.ExpectQuery(`INSERT INTO agent_versions`).
		WithArgs(sqlmock.AnyArg(), "agent-uuid", "1.2.0", hashManifest(manifestJSON), manifestJSON).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("version-uuid"))
	mock.ExpectCommit()

	srv := &runtimeServer{db: db}
	if _, err := srv.Deploy(context.Background(), &runtimev1.DeployRequest{Manifest: manifestJSON}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_Deploy_beginTxFailed(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectBegin().WillReturnError(errors.New("begin failed"))

	srv := &runtimeServer{db: db}
	_, err := srv.Deploy(context.Background(), &runtimev1.DeployRequest{
		Manifest: resolvedDeployManifestJSON(t),
	})
	assertGRPCCode(t, err, codes.Internal)
	if !strings.Contains(statusMessage(t, err), "begin transaction") {
		t.Fatalf("error = %v, want begin transaction", err)
	}
}

func TestRuntime_Deploy_upsertAgentFailed(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO agents`).WillReturnError(errors.New("upsert agent failed"))
	mock.ExpectRollback()

	srv := &runtimeServer{db: db}
	_, err := srv.Deploy(context.Background(), &runtimev1.DeployRequest{
		Manifest: resolvedDeployManifestJSON(t),
	})
	assertGRPCCode(t, err, codes.Internal)
	if !strings.Contains(statusMessage(t, err), "persist agent") {
		t.Fatalf("error = %v, want persist agent", err)
	}
}

func TestRuntime_Deploy_upsertVersionFailed(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO agents`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("agent-uuid"))
	mock.ExpectQuery(`INSERT INTO agent_versions`).WillReturnError(errors.New("upsert version failed"))
	mock.ExpectRollback()

	srv := &runtimeServer{db: db}
	_, err := srv.Deploy(context.Background(), &runtimev1.DeployRequest{
		Manifest: resolvedDeployManifestJSON(t),
	})
	assertGRPCCode(t, err, codes.Internal)
	if !strings.Contains(statusMessage(t, err), "persist agent version") {
		t.Fatalf("error = %v, want persist agent version", err)
	}
}

func TestRuntime_Deploy_commitFailed(t *testing.T) {
	manifestJSON := resolvedDeployManifestJSON(t)

	db, mock := testSQLxDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO agents`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("agent-uuid"))
	mock.ExpectQuery(`INSERT INTO agent_versions`).
		WithArgs(sqlmock.AnyArg(), "agent-uuid", "1.2.0", hashManifest(manifestJSON), manifestJSON).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("version-uuid"))
	mock.ExpectCommit().WillReturnError(errors.New("commit failed"))
	mock.ExpectRollback()

	srv := &runtimeServer{db: db}
	_, err := srv.Deploy(context.Background(), &runtimev1.DeployRequest{Manifest: manifestJSON})
	assertGRPCCode(t, err, codes.Internal)
	if !strings.Contains(statusMessage(t, err), "commit") {
		t.Fatalf("error = %v, want commit error", err)
	}
}

func TestDeployValidationStatus_nil(t *testing.T) {
	if err := deployValidationStatus(nil); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestMarshalLabels_empty(t *testing.T) {
	raw, err := marshalLabels(nil)
	if err != nil {
		t.Fatalf("marshalLabels: %v", err)
	}
	if string(raw) != "{}" {
		t.Fatalf("labels = %s, want {}", raw)
	}
}

func TestMarshalLabels_nonEmpty(t *testing.T) {
	raw, err := marshalLabels(map[string]string{"k": "v"})
	if err != nil {
		t.Fatalf("marshalLabels: %v", err)
	}
	if string(raw) != `{"k":"v"}` {
		t.Fatalf("labels = %s, want {\"k\":\"v\"}", raw)
	}
}

type deployManifestOpts struct {
	labels map[string]string
}

func resolvedDeployManifestJSON(t *testing.T, opts ...deployManifestOpts) []byte {
	t.Helper()
	agent := &manifest.Agent{
		APIVersion: manifest.APIVersionV1,
		Kind:       manifest.KindAgent,
		Metadata: func() manifest.AgentMetadata {
			meta := manifest.AgentMetadata{
				Name:      "echo-agent",
				Namespace: "demo",
				Version:   "1.2.0",
			}
			if len(opts) > 0 {
				meta.Labels = opts[0].labels
			}
			return meta
		}(),
		Spec: manifest.AgentSpec{
			Purpose: "Echo user messages.",
			Instructions: manifest.InstructionsSpec{
				Text: "You are an echo agent.",
			},
			Model: manifest.ModelConfig{
				Provider: "anthropic",
				Name:     "claude-sonnet-4-5",
			},
		},
		Output: &manifest.OutputSpec{
			Format: "json",
			Schema: &manifest.SchemaSpec{
				Inline: map[string]any{"type": "object"},
			},
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

func statusMessage(t *testing.T, err error) string {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status, got %v", err)
	}
	return st.Message()
}
