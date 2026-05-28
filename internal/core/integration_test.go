//go:build integration

package core

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/manifest"
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

	manifestJSON := integrationManifestJSON(t, namespace, "Integration test agent.")
	srv := &runtimeServer{db: db}

	resp, err := srv.Deploy(context.Background(), &runtimev1.DeployRequest{Manifest: manifestJSON})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if resp.GetAgentId() == "" || resp.GetVersionId() == "" {
		t.Fatalf("Deploy returned empty ids: %+v", resp)
	}
	if resp.GetContentHash() != hashManifest(manifestJSON) {
		t.Fatalf("content_hash = %q, want %q", resp.GetContentHash(), hashManifest(manifestJSON))
	}

	firstAgentID := resp.GetAgentId()
	firstVersionID := resp.GetVersionId()

	updatedJSON := integrationManifestJSON(t, namespace, "Updated integration purpose.")
	resp2, err := srv.Deploy(context.Background(), &runtimev1.DeployRequest{Manifest: updatedJSON})
	if err != nil {
		t.Fatalf("Deploy redeploy: %v", err)
	}
	if resp2.GetAgentId() != firstAgentID {
		t.Fatalf("redeploy agent_id = %q, want %q (upsert same agent)", resp2.GetAgentId(), firstAgentID)
	}
	if resp2.GetVersionId() != firstVersionID {
		t.Fatalf("redeploy version_id = %q, want %q (upsert same version)", resp2.GetVersionId(), firstVersionID)
	}
	if resp2.GetContentHash() == resp.GetContentHash() {
		t.Fatal("expected content hash to change after manifest update")
	}
	if resp2.GetContentHash() != hashManifest(updatedJSON) {
		t.Fatalf("content_hash = %q, want %q", resp2.GetContentHash(), hashManifest(updatedJSON))
	}
}

func integrationManifestJSON(t *testing.T, namespace, purpose string) []byte {
	t.Helper()
	agent := &manifest.Agent{
		APIVersion: manifest.APIVersionV1,
		Kind:       manifest.KindAgent,
		Metadata: manifest.AgentMetadata{
			Name:      "echo-agent",
			Namespace: namespace,
			Version:   "1.0.0",
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

func cleanupIntegrationAgents(t *testing.T, db *sqlx.DB, namespace string) {
	t.Helper()
	if _, err := db.Exec(`DELETE FROM agent_versions WHERE agent_id IN (SELECT id FROM agents WHERE namespace = $1)`, namespace); err != nil {
		t.Logf("cleanup agent_versions: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM agents WHERE namespace = $1`, namespace); err != nil {
		t.Logf("cleanup agents: %v", err)
	}
}
