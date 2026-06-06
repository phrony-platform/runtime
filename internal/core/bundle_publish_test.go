package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/manifest"
	"google.golang.org/grpc/codes"
)

func TestRuntime_PublishBundle_vendoredClosure(t *testing.T) {
	bundleManifest := bundleManifestJSON(t)
	orchestrator := bundleMemberManifestJSON(t, "orchestrator", "support", []manifest.ToolBinding{{
		Ref:             "./billing.yaml",
		As:              "ask_billing",
		InputSchema:     &manifest.SchemaSpec{Inline: map[string]any{"type": "object"}},
		SideEffectClass: manifest.SideEffectNonIdempotentWrite,
		Agent: &manifest.ToolAgentBinding{
			ChildName: "billing",
			Result:    manifest.SubagentResultSummary,
		},
	}})
	billing := bundleMemberManifestJSON(t, "billing", "support", nil)
	if err := manifest.Validate(mustParseAgentJSON(t, billing)); err != nil {
		t.Fatalf("billing validate: %v", err)
	}

	db, mock := testSQLxDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO bundles`).
		WithArgs(sqlmock.AnyArg(), "support", "helpdesk", "", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("bundle-1"))
	mock.ExpectQuery(`FROM bundle_versions bv`).
		WithArgs("bundle-1", sqlmock.AnyArg()).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`INSERT INTO bundle_versions`).
		WithArgs(sqlmock.AnyArg(), "bundle-1", sqlmock.AnyArg(), sqlmock.AnyArg(), "").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("bv-1"))
	mock.ExpectQuery(`INSERT INTO agent_versions`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("orch-ver"))
	mock.ExpectExec(`INSERT INTO bundle_members`).
		WithArgs(sqlmock.AnyArg(), "orchestrator", "orch-ver", "./orchestrator.yaml", "vendored", true).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`INSERT INTO agent_versions`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("bill-ver"))
	mock.ExpectExec(`INSERT INTO bundle_members`).
		WithArgs(sqlmock.AnyArg(), "billing", "bill-ver", "./billing.yaml", "vendored", false).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE bundle_versions`).
		WithArgs(sqlmock.AnyArg(), "orch-ver").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	srv := &runtimeServer{db: db}
	resp, err := srv.PublishBundle(context.Background(), &runtimev1.PublishBundleRequest{
		BundleManifest: bundleManifest,
		Members: []*runtimev1.BundleMemberPackage{
			{
				ChildName:        "orchestrator",
				Origin:           manifest.ClosureMemberOriginVendored,
				AuthoringRef:     "./orchestrator.yaml",
				ResolvedManifest: orchestrator,
				IsRoot:           true,
			},
			{
				ChildName:        "billing",
				Origin:           manifest.ClosureMemberOriginVendored,
				AuthoringRef:     "./billing.yaml",
				ResolvedManifest: billing,
			},
		},
	})
	if err != nil {
		t.Fatalf("PublishBundle: %v", err)
	}
	if resp.GetBundleId() != "bundle-1" {
		t.Fatalf("bundle_id = %q, want bundle-1", resp.GetBundleId())
	}
	if resp.GetBundleVersionId() == "" {
		t.Fatal("bundle_version_id is empty")
	}
	if resp.GetLockHash() == "" || len(resp.GetLock()) == 0 {
		t.Fatalf("lock = %q / %d bytes", resp.GetLockHash(), len(resp.GetLock()))
	}
	if resp.GetNamespace() != "support" || resp.GetName() != "helpdesk" {
		t.Fatalf("identity = %s/%s", resp.GetNamespace(), resp.GetName())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_PublishBundle_rejectsDuplicateLockHash(t *testing.T) {
	bundleManifest := bundleManifestJSON(t)
	orchestrator := bundleMemberManifestJSON(t, "orchestrator", "support", nil)

	db, mock := testSQLxDB(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO bundles`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("bundle-1"))
	lock := json.RawMessage(`{"version":"sha256:abc"}`)
	mock.ExpectQuery(`FROM bundle_versions bv`).
		WithArgs("bundle-1", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "lock"}).AddRow("bv-old", lock))
	mock.ExpectRollback()

	srv := &runtimeServer{db: db}
	_, err := srv.PublishBundle(context.Background(), &runtimev1.PublishBundleRequest{
		BundleManifest: bundleManifest,
		Members: []*runtimev1.BundleMemberPackage{{
			ChildName:        "orchestrator",
			Origin:           manifest.ClosureMemberOriginVendored,
			AuthoringRef:     "./orchestrator.yaml",
			ResolvedManifest: orchestrator,
			IsRoot:           true,
		}},
	})
	assertGRPCCode(t, err, codes.AlreadyExists)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func bundleManifestJSON(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal(&manifest.BundleManifest{
		APIVersion: manifest.APIVersionV1,
		Kind:       manifest.KindBundle,
		Metadata: manifest.BundleMetadata{
			Name:      "helpdesk",
			Namespace: "support",
		},
		Spec: manifest.BundleManifestSpec{
			Root: "./orchestrator.yaml",
		},
	})
	if err != nil {
		t.Fatalf("Marshal bundle: %v", err)
	}
	return raw
}

func bundleMemberManifestJSON(t *testing.T, name, namespace string, tools []manifest.ToolBinding) []byte {
	t.Helper()
	agent := &manifest.Agent{
		APIVersion: manifest.APIVersionV1,
		Kind:       manifest.KindAgent,
		Metadata: manifest.AgentMetadata{
			Name:      name,
			Namespace: namespace,
			Version:   "1.0.0",
			Annotations: map[string]string{
				manifest.AnnotationPoliciesCompiled: "true",
			},
		},
		Spec: manifest.AgentSpec{
			Purpose: "Test bundle member.",
			Instructions: manifest.InstructionsSpec{
				Text: "Do work.",
			},
			Model: manifest.ModelConfig{
				Provider: "anthropic",
				Name:     "claude-sonnet-4-5",
			},
			Tools: tools,
		},
	}
	raw, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("Marshal %s: %v", name, err)
	}
	return raw
}

func mustParseAgentJSON(t *testing.T, raw []byte) *manifest.Agent {
	t.Helper()
	agent, err := manifest.ParseJSON(raw)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	return agent
}
