//go:build integration

package core

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/manifest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const defaultIntegrationDSN = "postgres://phrony_runtime:phrony_runtime@localhost:5432/phrony_runtime?sslmode=disable"

func TestIntegration_MigrateAndDeploy(t *testing.T) {
	dsn := os.Getenv("RUNTIME_DATABASE_URL")
	if dsn == "" {
		dsn = defaultIntegrationDSN
	}

	db, err := sqlx.Connect("pgx", dsn)
	if err != nil {
		t.Skipf("postgres not available: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	namespace := "itest-" + uuid.NewString()[:8]
	t.Cleanup(func() { cleanupIntegrationAgents(t, db, namespace) })

	manifestJSON := integrationManifestJSON(t, namespace, "Integration test agent.", "1.0.0")
	srv := &runtimeServer{db: db}

	resp, err := srv.Publish(context.Background(), &runtimev1.PublishRequest{Manifest: manifestJSON})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if resp.GetAgentId() == "" || resp.GetVersionId() == "" {
		t.Fatalf("Publish returned empty ids: %+v", resp)
	}
	if resp.GetContentHash() != hashManifest(manifestJSON) {
		t.Fatalf("content_hash = %q, want %q", resp.GetContentHash(), hashManifest(manifestJSON))
	}

	versionV1ID := resp.GetVersionId()
	integrationActivateVersion(t, srv, namespace, "echo-agent", "1.0.0")

	updatedJSON := integrationManifestJSON(t, namespace, "Updated integration purpose.", "1.0.0")
	_, err = srv.Publish(context.Background(), &runtimev1.PublishRequest{Manifest: updatedJSON})
	if err == nil {
		t.Fatal("Publish redeploy same version: want error")
	}
	if st, ok := status.FromError(err); !ok || st.Code() != codes.AlreadyExists {
		t.Fatalf("redeploy same version: err = %v, want AlreadyExists", err)
	}
	if !strings.Contains(err.Error(), `version "1.0.0"`) || !strings.Contains(err.Error(), "cannot be changed") {
		t.Fatalf("redeploy error = %v, want immutable version message", err)
	}
	if !strings.Contains(err.Error(), hashManifest(updatedJSON)) {
		t.Fatalf("redeploy error = %v, want manifest content hash in message", err)
	}
	runV1, err := srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{
		AgentRef: &runtimev1.AgentRef{
			Namespace: namespace,
			Name:      "echo-agent",
			Version:   "1.0.0",
		},
		Input: []byte(`{"message":"hello"}`),
	})
	if err != nil {
		t.Fatalf("RunSession v1.0.0: %v", err)
	}
	if runV1.GetSessionId() == "" {
		t.Fatal("RunSession returned empty session_id")
	}
	if runV1.GetStatus() != runSessionStatusPending {
		t.Fatalf("status = %q, want %q", runV1.GetStatus(), runSessionStatusPending)
	}
	if runV1.GetAgentVersionId() != versionV1ID {
		t.Fatalf("agent_version_id = %q, want %q", runV1.GetAgentVersionId(), versionV1ID)
	}
	t.Cleanup(func() { cleanupIntegrationSessions(t, db, runV1.GetSessionId()) })

	manifestV2 := integrationManifestJSON(t, namespace, "Version two.", "2.0.0")
	respV2, err := srv.Publish(context.Background(), &runtimev1.PublishRequest{Manifest: manifestV2})
	if err != nil {
		t.Fatalf("Publish v2: %v", err)
	}
	if respV2.GetVersionId() == versionV1ID {
		t.Fatalf("v2 version_id = %q, want different from v1", respV2.GetVersionId())
	}
	integrationActivateVersion(t, srv, namespace, "echo-agent", "2.0.0")

	runLatest, err := srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{
		AgentRef: &runtimev1.AgentRef{
			Namespace: namespace,
			Name:      "echo-agent",
		},
	})
	if err != nil {
		t.Fatalf("RunSession latest: %v", err)
	}

	if runLatest.GetAgentVersionId() != respV2.GetVersionId() {
		t.Fatalf("latest agent_version_id = %q, want %q", runLatest.GetAgentVersionId(), respV2.GetVersionId())
	}
	t.Cleanup(func() { cleanupIntegrationSessions(t, db, runLatest.GetSessionId()) })
}

func TestIntegration_AgentLifecycle(t *testing.T) {
	dsn := os.Getenv("RUNTIME_DATABASE_URL")
	if dsn == "" {
		dsn = defaultIntegrationDSN
	}

	db, err := sqlx.Connect("pgx", dsn)
	if err != nil {
		t.Skipf("postgres not available: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	namespace := "itest-" + uuid.NewString()[:8]
	t.Cleanup(func() { cleanupIntegrationAgents(t, db, namespace) })

	manifestJSON := integrationManifestJSON(t, namespace, "Lifecycle agent.", "1.0.0")
	srv := &runtimeServer{db: db}

	publishResp, err := srv.Publish(context.Background(), &runtimev1.PublishRequest{Manifest: manifestJSON})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if publishResp.GetAgentId() == "" {
		t.Fatal("Publish returned empty agent_id")
	}
	integrationActivateVersion(t, srv, namespace, "echo-agent", "1.0.0")

	listAgents, err := srv.ListAgents(context.Background(), &runtimev1.ListAgentsRequest{Namespace: namespace})
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}

	if len(listAgents.GetAgents()) != 1 {
		t.Fatalf("agents = %d, want 1", len(listAgents.GetAgents()))
	}

	agentRef := &runtimev1.AgentRef{Namespace: namespace, Name: "echo-agent"}
	versions, err := srv.ListAgentVersions(context.Background(), &runtimev1.ListAgentVersionsRequest{AgentRef: agentRef})
	if err != nil {
		t.Fatalf("ListAgentVersions: %v", err)
	}

	if len(versions.GetVersions()) != 1 {
		t.Fatalf("versions = %d, want 1", len(versions.GetVersions()))
	}

	_, err = srv.DeprecateAgentVersion(context.Background(), &runtimev1.DeprecateAgentVersionRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: namespace, Name: "echo-agent", Version: "1.0.0"},
	})
	if err != nil {
		t.Fatalf("DeprecateAgentVersion: %v", err)
	}

	_, err = srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: namespace, Name: "echo-agent", Version: "1.0.0"},
	})

	if err == nil {
		t.Fatal("RunSession deprecated version: want error")
	}

	if st, ok := status.FromError(err); !ok || st.Code() != codes.FailedPrecondition {
		t.Fatalf("RunSession deprecated: err = %v, want FailedPrecondition", err)
	}

	manifestV2 := integrationManifestJSON(t, namespace, "Lifecycle v2.", "2.0.0")
	if _, err := srv.Publish(context.Background(), &runtimev1.PublishRequest{Manifest: manifestV2}); err != nil {
		t.Fatalf("Publish v2: %v", err)
	}

	if _, err := srv.ArchiveAgent(context.Background(), &runtimev1.ArchiveAgentRequest{AgentRef: agentRef}); err != nil {
		t.Fatalf("ArchiveAgent: %v", err)
	}

	_, err = srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: namespace, Name: "echo-agent"},
	})

	if err == nil {
		t.Fatal("RunSession archived agent: want error")
	}

	if st, ok := status.FromError(err); !ok || st.Code() != codes.FailedPrecondition {
		t.Fatalf("RunSession archived: err = %v, want FailedPrecondition", err)
	}

	if _, err := srv.Publish(context.Background(), &runtimev1.PublishRequest{Manifest: manifestV2}); err == nil {
		t.Fatal("Publish to archived agent: want error")
	}
}

func TestIntegration_PublishWithoutDeployCannotRun(t *testing.T) {
	dsn := os.Getenv("RUNTIME_DATABASE_URL")
	if dsn == "" {
		dsn = defaultIntegrationDSN
	}

	db, err := sqlx.Connect("pgx", dsn)
	if err != nil {
		t.Skipf("postgres not available: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	namespace := "itest-" + uuid.NewString()[:8]
	t.Cleanup(func() { cleanupIntegrationAgents(t, db, namespace) })

	manifestJSON := integrationManifestJSON(t, namespace, "Published but not deployed.", "3.0.0")
	srv := &runtimeServer{db: db}

	if _, err := srv.Publish(context.Background(), &runtimev1.PublishRequest{Manifest: manifestJSON}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	_, err = srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: namespace, Name: "echo-agent"},
	})
	if err == nil {
		t.Fatal("RunSession without deploy: want error")
	}
	if st, ok := status.FromError(err); !ok || st.Code() != codes.FailedPrecondition {
		t.Fatalf("RunSession without deploy: err = %v, want FailedPrecondition", err)
	}
	if !strings.Contains(err.Error(), "no active deployment") {
		t.Fatalf("RunSession without deploy: err = %v, want no active deployment", err)
	}
}

func integrationActivateVersion(t *testing.T, srv *runtimeServer, namespace, name, version string) {
	t.Helper()
	_, err := srv.Deploy(context.Background(), &runtimev1.DeployRequest{
		AgentRef: &runtimev1.AgentRef{Namespace: namespace, Name: name, Version: version},
	})
	if err != nil {
		t.Fatalf("Deploy %s/%s@%s: %v", namespace, name, version, err)
	}
}

func integrationManifestJSON(t *testing.T, namespace, purpose, version string) []byte {
	t.Helper()
	agent := &manifest.Agent{
		APIVersion: manifest.APIVersionV1,
		Kind:       manifest.KindAgent,
		Metadata: manifest.AgentMetadata{
			Name:      "echo-agent",
			Namespace: namespace,
			Version:   version,
			Owner:     "integration",
			Labels:    map[string]string{"suite": "integration"},
		},
		Spec: manifest.AgentSpec{
			Purpose: purpose,
			Instructions: manifest.InstructionsSpec{
				Text: "You are a test agent.",
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

func cleanupIntegrationSessions(t *testing.T, db *sqlx.DB, sessionID string) {
	t.Helper()
	if _, err := db.Exec(`DELETE FROM sessions WHERE id = $1`, sessionID); err != nil {
		t.Logf("cleanup sessions: %v", err)
	}
}

func cleanupIntegrationAgents(t *testing.T, db *sqlx.DB, namespace string) {
	t.Helper()
	if _, err := db.Exec(`DELETE FROM sessions WHERE agent_version_id IN (
		SELECT av.id FROM agent_versions av
		INNER JOIN agents a ON a.id = av.agent_id
		WHERE a.namespace = $1
	)`, namespace); err != nil {
		t.Logf("cleanup sessions: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM deployments WHERE agent_id IN (SELECT id FROM agents WHERE namespace = $1)`, namespace); err != nil {
		t.Logf("cleanup deployments: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM agent_versions WHERE agent_id IN (SELECT id FROM agents WHERE namespace = $1)`, namespace); err != nil {
		t.Logf("cleanup agent_versions: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM agents WHERE namespace = $1`, namespace); err != nil {
		t.Logf("cleanup agents: %v", err)
	}
}
