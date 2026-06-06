package core

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

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

	members := []*runtimev1.BundleMemberPackage{
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
	}
	committedLock := committedLockJSON(t, "orchestrator", members)

	srv := &runtimeServer{db: db}
	resp, err := srv.PublishBundle(context.Background(), &runtimev1.PublishBundleRequest{
		BundleManifest: bundleManifest,
		Members:        members,
		CommittedLock:  committedLock,
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
	parsedLock, err := manifest.ParseLockfileJSON(committedLock)
	if err != nil {
		t.Fatalf("ParseLockfileJSON: %v", err)
	}
	if resp.GetLockHash() != parsedLock.Version {
		t.Fatalf("lock_hash = %q, want committed version %q", resp.GetLockHash(), parsedLock.Version)
	}
	if !bytes.Equal(resp.GetLock(), committedLock) {
		t.Fatalf("stored lock = %q, want committed bytes %q", resp.GetLock(), committedLock)
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

	members := []*runtimev1.BundleMemberPackage{{
		ChildName:        "orchestrator",
		Origin:           manifest.ClosureMemberOriginVendored,
		AuthoringRef:     "./orchestrator.yaml",
		ResolvedManifest: orchestrator,
		IsRoot:           true,
	}}
	committedLock := committedLockJSON(t, "orchestrator", members)

	srv := &runtimeServer{db: db}
	_, err := srv.PublishBundle(context.Background(), &runtimev1.PublishBundleRequest{
		BundleManifest: bundleManifest,
		Members:        members,
		CommittedLock:  committedLock,
	})
	assertGRPCCode(t, err, codes.AlreadyExists)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_PublishBundle_rejectsMissingCommittedLock(t *testing.T) {
	bundleManifest := bundleManifestJSON(t)
	orchestrator := bundleMemberManifestJSON(t, "orchestrator", "support", nil)

	srv := &runtimeServer{}
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
	assertGRPCCode(t, err, codes.InvalidArgument)
	if err != nil && !strings.Contains(err.Error(), "bundle lock") {
		t.Fatalf("error = %v, want bundle lock hint", err)
	}
}

func TestRuntime_PublishBundle_externalClosure(t *testing.T) {
	bundleManifest := bundleManifestJSON(t)
	orchestrator := bundleMemberManifestJSON(t, "orchestrator", "support", nil)

	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM agent_versions av`).
		WithArgs("support", "billing", "1.2.0").
		WillReturnRows(sqlmock.NewRows([]string{"id", "deprecated_at", "retired_at", "archived_at"}).
			AddRow("bill-ext-ver", nil, nil, nil))
	publishedAt := time.Now()
	externalManifest := bundleMemberManifestJSON(t, "billing", "support", nil)
	mock.ExpectQuery(`FROM agent_versions av`).
		WithArgs("support", "billing", "1.2.0").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "version", "content_hash", "manifest", "deployed_at", "deprecated_at", "retired_at",
		}).AddRow("bill-ext-ver", "1.2.0", "ext-hash", externalManifest, publishedAt, nil, nil))

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
	mock.ExpectExec(`INSERT INTO bundle_members`).
		WithArgs(sqlmock.AnyArg(), "billing", "bill-ext-ver", "support.billing@1.2.0", "external", false).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE bundle_versions`).
		WithArgs(sqlmock.AnyArg(), "orch-ver").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	members := []*runtimev1.BundleMemberPackage{
		{
			ChildName:        "orchestrator",
			Origin:           manifest.ClosureMemberOriginVendored,
			AuthoringRef:     "./orchestrator.yaml",
			ResolvedManifest: orchestrator,
			IsRoot:           true,
		},
		{
			ChildName:    "billing",
			Origin:       manifest.ClosureMemberOriginExternal,
			AuthoringRef: "support.billing@1.2.0",
		},
	}
	committedLock := committedLockJSON(t, "orchestrator", members)

	srv := &runtimeServer{db: db}
	resp, err := srv.PublishBundle(context.Background(), &runtimev1.PublishBundleRequest{
		BundleManifest: bundleManifest,
		Members:        members,
		CommittedLock:  committedLock,
	})
	if err != nil {
		t.Fatalf("PublishBundle: %v", err)
	}
	parsedLock, err := manifest.ParseLockfileJSON(committedLock)
	if err != nil {
		t.Fatalf("ParseLockfileJSON: %v", err)
	}
	if resp.GetLockHash() != parsedLock.Version {
		t.Fatalf("lock_hash = %q, want %q", resp.GetLockHash(), parsedLock.Version)
	}
	if !bytes.Equal(resp.GetLock(), committedLock) {
		t.Fatalf("stored lock = %q, want committed bytes %q", resp.GetLock(), committedLock)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_PublishBundle_externalMemberNotFound(t *testing.T) {
	bundleManifest := bundleManifestJSON(t)
	orchestrator := bundleMemberManifestJSON(t, "orchestrator", "support", nil)
	members := []*runtimev1.BundleMemberPackage{
		{
			ChildName:        "orchestrator",
			Origin:           manifest.ClosureMemberOriginVendored,
			AuthoringRef:     "./orchestrator.yaml",
			ResolvedManifest: orchestrator,
			IsRoot:           true,
		},
		{
			ChildName:    "billing",
			Origin:       manifest.ClosureMemberOriginExternal,
			AuthoringRef: "support.billing@9.9.9",
		},
	}
	committedLock := committedLockJSON(t, "orchestrator", members)

	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM agent_versions av`).
		WithArgs("support", "billing", "9.9.9").
		WillReturnError(sql.ErrNoRows)

	srv := &runtimeServer{db: db}
	_, err := srv.PublishBundle(context.Background(), &runtimev1.PublishBundleRequest{
		BundleManifest: bundleManifest,
		Members:        members,
		CommittedLock:  committedLock,
	})
	assertGRPCCode(t, err, codes.NotFound)
	if err != nil && !strings.Contains(err.Error(), "no published version") {
		t.Fatalf("error = %v, want missing external version", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_PublishBundle_externalMemberRetired(t *testing.T) {
	bundleManifest := bundleManifestJSON(t)
	orchestrator := bundleMemberManifestJSON(t, "orchestrator", "support", nil)
	members := []*runtimev1.BundleMemberPackage{
		{
			ChildName:        "orchestrator",
			Origin:           manifest.ClosureMemberOriginVendored,
			AuthoringRef:     "./orchestrator.yaml",
			ResolvedManifest: orchestrator,
			IsRoot:           true,
		},
		{
			ChildName:    "billing",
			Origin:       manifest.ClosureMemberOriginExternal,
			AuthoringRef: "support.billing@1.2.0",
		},
	}
	committedLock := committedLockJSON(t, "orchestrator", members)

	db, mock := testSQLxDB(t)
	retiredAt := time.Now()
	mock.ExpectQuery(`FROM agent_versions av`).
		WithArgs("support", "billing", "1.2.0").
		WillReturnRows(sqlmock.NewRows([]string{"id", "deprecated_at", "retired_at", "archived_at"}).
			AddRow("bill-ext-ver", nil, retiredAt, nil))

	srv := &runtimeServer{db: db}
	_, err := srv.PublishBundle(context.Background(), &runtimev1.PublishBundleRequest{
		BundleManifest: bundleManifest,
		Members:        members,
		CommittedLock:  committedLock,
	})
	assertGRPCCode(t, err, codes.FailedPrecondition)
	if err != nil && !strings.Contains(err.Error(), "retired") {
		t.Fatalf("error = %v, want retired external version", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_PublishBundle_rejectsInvalidCommittedLockVersion(t *testing.T) {
	bundleManifest := bundleManifestJSON(t)
	orchestrator := bundleMemberManifestJSON(t, "orchestrator", "support", nil)
	members := []*runtimev1.BundleMemberPackage{{
		ChildName:        "orchestrator",
		Origin:           manifest.ClosureMemberOriginVendored,
		AuthoringRef:     "./orchestrator.yaml",
		ResolvedManifest: orchestrator,
		IsRoot:           true,
	}}
	committedLock := committedLockJSON(t, "orchestrator", members)
	var lock manifest.Lockfile
	if err := json.Unmarshal(committedLock, &lock); err != nil {
		t.Fatalf("Unmarshal lock: %v", err)
	}
	lock.Version = "sha256:deadbeef"
	tamperedLock, err := json.Marshal(lock)
	if err != nil {
		t.Fatalf("Marshal lock: %v", err)
	}

	srv := &runtimeServer{}
	_, err = srv.PublishBundle(context.Background(), &runtimev1.PublishBundleRequest{
		BundleManifest: bundleManifest,
		Members:        members,
		CommittedLock:  tamperedLock,
	})
	assertGRPCCode(t, err, codes.InvalidArgument)
	if err != nil && !strings.Contains(err.Error(), "does not match body hash") {
		t.Fatalf("error = %v, want version mismatch", err)
	}
}

func TestRuntime_PublishBundle_rejectsLockDrift(t *testing.T) {
	bundleManifest := bundleManifestJSON(t)
	orchestrator := bundleMemberManifestJSON(t, "orchestrator", "support", nil)
	members := []*runtimev1.BundleMemberPackage{{
		ChildName:        "orchestrator",
		Origin:           manifest.ClosureMemberOriginVendored,
		AuthoringRef:     "./orchestrator.yaml",
		ResolvedManifest: orchestrator,
		IsRoot:           true,
	}}
	tamperedLock, err := manifest.MarshalLockfile(manifest.Lockfile{
		RootChildName: "orchestrator",
		Members: []manifest.LockfileMember{{
			ChildName:   "orchestrator",
			Origin:      manifest.ClosureMemberOriginVendored,
			Ref:         "./orchestrator.yaml",
			ContentHash: "tampered",
		}},
	})
	if err != nil {
		t.Fatalf("MarshalLockfile: %v", err)
	}

	db, mock := testSQLxDB(t)
	srv := &runtimeServer{db: db}
	_, err = srv.PublishBundle(context.Background(), &runtimev1.PublishBundleRequest{
		BundleManifest: bundleManifest,
		Members:        members,
		CommittedLock:  tamperedLock,
	})
	assertGRPCCode(t, err, codes.InvalidArgument)
	if err != nil && !strings.Contains(err.Error(), "drift") {
		t.Fatalf("error = %v, want drift", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func committedLockJSON(t *testing.T, rootChildName string, pkgs []*runtimev1.BundleMemberPackage) []byte {
	t.Helper()
	members := make([]manifest.LockfileMember, 0, len(pkgs))
	for _, pkg := range pkgs {
		entry := manifest.LockfileMember{
			ChildName: pkg.GetChildName(),
			Origin:    pkg.GetOrigin(),
			Ref:       pkg.GetAuthoringRef(),
		}
		switch pkg.GetOrigin() {
		case manifest.ClosureMemberOriginVendored:
			entry.ContentHash = hashManifest(pkg.GetResolvedManifest())
		case manifest.ClosureMemberOriginExternal:
			ref := strings.TrimSpace(pkg.GetAuthoringRef())
			edge, err := manifest.ParseAgentEdgeRef(ref, false)
			if err != nil {
				t.Fatalf("ParseAgentEdgeRef %q: %v", ref, err)
			}
			entry.Namespace = edge.External.Namespace
			entry.Name = edge.External.Name
			entry.Version = edge.External.Constraint
		}
		members = append(members, entry)
	}
	lock := manifest.Lockfile{
		RootChildName: rootChildName,
		Members:       members,
	}
	raw, err := manifest.MarshalLockfile(lock)
	if err != nil {
		t.Fatalf("MarshalLockfile: %v", err)
	}
	return raw
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

func TestClosurePackageFromPlans_carriesAgentVersionIDs(t *testing.T) {
	plans := []bundleMemberPlan{
		{
			childName:    "orchestrator",
			origin:       manifest.ClosureMemberOriginVendored,
			authoringRef: "./orchestrator.yaml",
			isRoot:       true,
			versionID:    "orch-ver",
			agent: &manifest.Agent{
				Spec: manifest.AgentSpec{
					Tools: []manifest.ToolBinding{{
						Ref:             "./billing.yaml",
						As:              "ask_billing",
						InputSchema:     &manifest.SchemaSpec{Inline: map[string]any{"type": "object"}},
						SideEffectClass: manifest.SideEffectNonIdempotentWrite,
						Agent:           &manifest.ToolAgentBinding{ChildName: "billing"},
					}},
				},
			},
		},
		{
			childName:    "billing",
			origin:       manifest.ClosureMemberOriginVendored,
			authoringRef: "./billing.yaml",
			versionID:    "bill-ver",
		},
	}
	pkg := closurePackageFromPlans("orchestrator", plans)
	ctx := manifest.NewClosureContext(pkg)
	if err := manifest.ApplyClosurePinning(plans[0].agent, ctx); err != nil {
		t.Fatalf("ApplyClosurePinning: %v", err)
	}
	got := plans[0].agent.Spec.Tools[0].Agent
	if got == nil || got.AgentVersionID != "bill-ver" {
		t.Fatalf("pinned agent_version_id = %+v, want bill-ver", got)
	}
	if got.ChildName != "billing" {
		t.Fatalf("child_name = %q, want billing", got.ChildName)
	}
}

func mustParseAgentJSON(t *testing.T, raw []byte) *manifest.Agent {
	t.Helper()
	agent, err := manifest.ParseJSON(raw)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	return agent
}
