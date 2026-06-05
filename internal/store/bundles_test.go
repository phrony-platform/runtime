package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestQueries_UpsertBundle(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`INSERT INTO bundles`).
		WithArgs("bundle-1", "support", "helpdesk", "team", json.RawMessage(`{}`)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("bundle-1"))

	q := New(sqlDB)
	id, err := q.UpsertBundle(context.Background(), UpsertBundleParams{
		ID:        "bundle-1",
		Namespace: "support",
		Name:      "helpdesk",
		Owner:     "team",
		Labels:    json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("UpsertBundle: %v", err)
	}
	if id != "bundle-1" {
		t.Fatalf("id = %q, want bundle-1", id)
	}
}

func TestQueries_InsertBundleVersion(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	lock := json.RawMessage(`{"version":"sha256:abc"}`)
	mock.ExpectQuery(`INSERT INTO bundle_versions`).
		WithArgs("bv-1", "bundle-1", "sha256:abc", lock, "root-ver").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("bv-1"))

	q := New(sqlDB)
	id, err := q.InsertBundleVersion(context.Background(), InsertBundleVersionParams{
		ID:                  "bv-1",
		BundleID:            "bundle-1",
		LockHash:            "sha256:abc",
		Lock:                lock,
		RootMemberVersionID: "root-ver",
	})
	if err != nil {
		t.Fatalf("InsertBundleVersion: %v", err)
	}
	if id != "bv-1" {
		t.Fatalf("id = %q, want bv-1", id)
	}
}

func TestQueries_InsertBundleMember(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectExec(`INSERT INTO bundle_members`).
		WithArgs("bv-1", "orchestrator", "ver-1", "./orchestrator.yaml", "vendored", true).
		WillReturnResult(sqlmock.NewResult(0, 1))

	q := New(sqlDB)
	err = q.InsertBundleMember(context.Background(), InsertBundleMemberParams{
		BundleVersionID: "bv-1",
		ChildName:       "orchestrator",
		MemberVersionID: "ver-1",
		Ref:             "./orchestrator.yaml",
		Origin:          "vendored",
		IsRoot:          true,
	})
	if err != nil {
		t.Fatalf("InsertBundleMember: %v", err)
	}
}

func TestQueries_InsertVendoredAgentVersion(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	manifest := json.RawMessage(`{"kind":"Agent"}`)
	mock.ExpectQuery(`INSERT INTO agent_versions`).
		WithArgs("ver-1", "sha256:child", "sha256:child", manifest, "bv-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("ver-1"))

	q := New(sqlDB)
	id, err := q.InsertVendoredAgentVersion(context.Background(), InsertVendoredAgentVersionParams{
		ID:              "ver-1",
		Version:         "sha256:child",
		ContentHash:     "sha256:child",
		Manifest:        manifest,
		BundleVersionID: "bv-1",
	})
	if err != nil {
		t.Fatalf("InsertVendoredAgentVersion: %v", err)
	}
	if id != "ver-1" {
		t.Fatalf("id = %q, want ver-1", id)
	}
}

func TestQueries_InsertBundleDeployment(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`INSERT INTO bundle_deployments`).
		WithArgs("dep-1", "bundle-1", "bv-1", "deploy", "alice").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("dep-1"))

	q := New(sqlDB)
	id, err := q.InsertBundleDeployment(context.Background(), "dep-1", "bundle-1", "bv-1", "deploy", "alice")
	if err != nil {
		t.Fatalf("InsertBundleDeployment: %v", err)
	}
	if id != "dep-1" {
		t.Fatalf("id = %q, want dep-1", id)
	}
}

func TestQueries_ActiveBundleVersion(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM bundle_deployments bd`).
		WithArgs("support", "helpdesk").
		WillReturnRows(sqlmock.NewRows([]string{"id", "lock_hash", "root_member_version_id"}).
			AddRow("bv-2", "sha256:def", "root-ver"))

	q := New(sqlDB)
	got, err := q.ActiveBundleVersion(context.Background(), "support", "helpdesk")
	if err != nil {
		t.Fatalf("ActiveBundleVersion: %v", err)
	}
	if got.BundleVersionID != "bv-2" || got.LockHash != "sha256:def" || got.RootMemberVersionID != "root-ver" {
		t.Fatalf("got = %+v", got)
	}
}

func TestQueries_ActiveBundleVersion_notFound(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM bundle_deployments bd`).
		WithArgs("support", "missing").
		WillReturnError(sql.ErrNoRows)

	q := New(sqlDB)
	_, err = q.ActiveBundleVersion(context.Background(), "support", "missing")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestQueries_ActiveBundleDeploymentDetail(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	deployed := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`FROM bundle_deployments bd`).
		WithArgs("support", "helpdesk").
		WillReturnRows(sqlmock.NewRows([]string{"lock_hash", "created_at", "actor"}).
			AddRow("sha256:def", deployed, "alice"))

	q := New(sqlDB)
	got, err := q.ActiveBundleDeploymentDetail(context.Background(), "support", "helpdesk")
	if err != nil {
		t.Fatalf("ActiveBundleDeploymentDetail: %v", err)
	}
	if got.LockHash != "sha256:def" || got.Actor != "alice" {
		t.Fatalf("got = %+v", got)
	}
	if !got.DeployedAt.Equal(deployed) {
		t.Fatalf("deployed_at = %v, want %v", got.DeployedAt, deployed)
	}
}

func TestQueries_BundleVersionByLockHash(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	lock := json.RawMessage(`{"version":"sha256:abc"}`)
	mock.ExpectQuery(`FROM bundle_versions bv`).
		WithArgs("bundle-1", "sha256:abc").
		WillReturnRows(sqlmock.NewRows([]string{"id", "lock"}).AddRow("bv-1", lock))

	q := New(sqlDB)
	got, err := q.BundleVersionByLockHash(context.Background(), "bundle-1", "sha256:abc")
	if err != nil {
		t.Fatalf("BundleVersionByLockHash: %v", err)
	}
	if got.ID != "bv-1" || string(got.Lock) != string(lock) {
		t.Fatalf("got = %+v", got)
	}
}

func TestQueries_ListBundleReferencesForMemberVersion(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM bundle_members bm`).
		WithArgs("ver-1").
		WillReturnRows(sqlmock.NewRows([]string{"namespace", "name", "lock_hash"}).
			AddRow("support", "helpdesk", "sha256:abc").
			AddRow("support", "helpdesk", "sha256:def"))

	q := New(sqlDB)
	refs, err := q.ListBundleReferencesForMemberVersion(context.Background(), "ver-1")
	if err != nil {
		t.Fatalf("ListBundleReferencesForMemberVersion: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("len(refs) = %d, want 2", len(refs))
	}
	if refs[0].Namespace != "support" || refs[0].LockHash != "sha256:abc" {
		t.Fatalf("refs[0] = %+v", refs[0])
	}
}

func TestQueries_BundleVersionIDByLabel(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM bundle_versions bv`).
		WithArgs("support", "helpdesk", "sha256:abc").
		WillReturnRows(sqlmock.NewRows([]string{"id", "root_member_version_id"}).
			AddRow("bv-1", "root-ver"))

	q := New(sqlDB)
	got, err := q.BundleVersionIDByLabel(context.Background(), "support", "helpdesk", "sha256:abc")
	if err != nil {
		t.Fatalf("BundleVersionIDByLabel: %v", err)
	}
	if got.ID != "bv-1" || got.RootMemberVersionID != "root-ver" {
		t.Fatalf("got = %+v", got)
	}
}
