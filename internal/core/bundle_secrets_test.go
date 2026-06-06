package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/store"
)

func TestGetBundleSecretRequirements_returnsUnion(t *testing.T) {
	orchestrator := manifest.Agent{
		APIVersion: manifest.APIVersionV1,
		Kind:       manifest.KindAgent,
		Metadata:   manifest.AgentMetadata{Name: "orchestrator", Namespace: "demo", Version: "1.0.0"},
		Secrets: map[string]manifest.SecretDefinition{
			"openai": {FromEnv: "OPENAI_API_KEY"},
		},
		Spec: manifest.AgentSpec{
			Purpose:      "p",
			Instructions: manifest.InstructionsSpec{Text: "i"},
			Model:        manifest.ModelConfig{Provider: "stub", Name: "scripted", Secret: "openai"},
		},
	}
	specialist := manifest.Agent{
		APIVersion: manifest.APIVersionV1,
		Kind:       manifest.KindAgent,
		Metadata:   manifest.AgentMetadata{Name: "specialist", Namespace: "demo", Version: "1.0.0"},
		Secrets: map[string]manifest.SecretDefinition{
			"anthropic": {FromEnv: "ANTHROPIC_API_KEY"},
		},
		Spec: manifest.AgentSpec{
			Purpose:      "p",
			Instructions: manifest.InstructionsSpec{Text: "i"},
			Model:        manifest.ModelConfig{Provider: "stub", Name: "scripted", Secret: "anthropic"},
		},
	}
	rootRaw, _ := json.Marshal(orchestrator)
	childRaw, _ := json.Marshal(specialist)

	db, mock := testSQLxDB(t)
	expectActiveBundleVersion(mock, "demo", "routing", "bundle-ver", "1.0.0")
	mock.ExpectQuery(`FROM bundle_members bm`).
		WithArgs("bundle-ver").
		WillReturnRows(sqlmock.NewRows([]string{"manifest", "child_name", "origin"}).
			AddRow(rootRaw, "orchestrator", manifest.ClosureMemberOriginVendored).
			AddRow(childRaw, "specialist", manifest.ClosureMemberOriginVendored))

	srv := &runtimeServer{db: db}
	resp, err := srv.GetBundleSecretRequirements(context.Background(), &runtimev1.GetBundleSecretRequirementsRequest{
		BundleRef: &runtimev1.BundleRef{Namespace: "demo", Name: "routing"},
	})
	if err != nil {
		t.Fatalf("GetBundleSecretRequirements: %v", err)
	}
	if got := resp.GetSecrets()["openai"].GetFromEnv(); got != "OPENAI_API_KEY" {
		t.Fatalf("openai from_env = %q", got)
	}
	if got := resp.GetSecrets()["anthropic"].GetFromEnv(); got != "ANTHROPIC_API_KEY" {
		t.Fatalf("anthropic from_env = %q", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestResolveBundleRootSessionID_walksToDepthZero(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`SELECT parent_session_id, bundle_version_id, depth`).
		WithArgs("leaf-sess").
		WillReturnRows(sqlmock.NewRows([]string{"parent_session_id", "bundle_version_id", "depth"}).
			AddRow("middle-sess", nil, 2))
	mock.ExpectQuery(`SELECT parent_session_id, bundle_version_id, depth`).
		WithArgs("middle-sess").
		WillReturnRows(sqlmock.NewRows([]string{"parent_session_id", "bundle_version_id", "depth"}).
			AddRow("root-sess", "bundle-ver", 1))
	mock.ExpectQuery(`SELECT parent_session_id, bundle_version_id, depth`).
		WithArgs("root-sess").
		WillReturnRows(sqlmock.NewRows([]string{"parent_session_id", "bundle_version_id", "depth"}).
			AddRow(nil, "bundle-ver", 0))

	got, err := resolveBundleRootSessionID(context.Background(), store.New(db), "leaf-sess")
	if err != nil {
		t.Fatalf("resolveBundleRootSessionID: %v", err)
	}
	if got != "root-sess" {
		t.Fatalf("root session = %q, want root-sess", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func expectActiveBundleVersion(mock sqlmock.Sqlmock, namespace, name, bundleVersionID, version string) {
	mock.ExpectQuery(`FROM bundle_deployments bd`).
		WithArgs(namespace, name).
		WillReturnRows(sqlmock.NewRows([]string{"id", "version", "lock_hash", "root_member_version_id"}).
			AddRow(bundleVersionID, version, "sha256:abc", "root-member-ver"))
}
