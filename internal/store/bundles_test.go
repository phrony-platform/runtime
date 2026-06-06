package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestQueries_ListBundles_allNamespaces(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`FROM bundles`).
		WithArgs("").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "owner", "created_at"}).
			AddRow("bundle-1", "demo", "payment-desk-hitl", "team", now))

	q := New(sqlDB)
	rows, err := q.ListBundles(context.Background(), "")
	if err != nil {
		t.Fatalf("ListBundles: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0].ID != "bundle-1" || rows[0].Namespace != "demo" {
		t.Fatalf("row = %+v", rows[0])
	}
}

func TestQueries_ListBundles_namespaceFilter(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`FROM bundles`).
		WithArgs("demo").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "owner", "created_at"}).
			AddRow("bundle-1", "demo", "payment-desk-hitl", "team", now))

	q := New(sqlDB)
	rows, err := q.ListBundles(context.Background(), "demo")
	if err != nil {
		t.Fatalf("ListBundles: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "payment-desk-hitl" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestQueries_ListBundles_empty(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM bundles`).
		WithArgs("missing").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "owner", "created_at"}))

	q := New(sqlDB)
	rows, err := q.ListBundles(context.Background(), "missing")
	if err != nil {
		t.Fatalf("ListBundles: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}
}

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
		WithArgs("bv-1", "bundle-1", "1.0.0", "sha256:abc", lock, "root-ver").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("bv-1"))

	q := New(sqlDB)
	id, err := q.InsertBundleVersion(context.Background(), InsertBundleVersionParams{
		ID:                  "bv-1",
		BundleID:            "bundle-1",
		Version:             "1.0.0",
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
		WillReturnRows(sqlmock.NewRows([]string{"id", "version", "lock_hash", "root_member_version_id"}).
			AddRow("bv-2", "1.0.1", "sha256:def", "root-ver"))

	q := New(sqlDB)
	got, err := q.ActiveBundleVersion(context.Background(), "support", "helpdesk")
	if err != nil {
		t.Fatalf("ActiveBundleVersion: %v", err)
	}
	if got.BundleVersionID != "bv-2" || got.Version != "1.0.1" || got.LockHash != "sha256:def" || got.RootMemberVersionID != "root-ver" {
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
		WillReturnRows(sqlmock.NewRows([]string{"version", "lock_hash", "created_at", "actor"}).
			AddRow("1.0.1", "sha256:def", deployed, "alice"))

	q := New(sqlDB)
	got, err := q.ActiveBundleDeploymentDetail(context.Background(), "support", "helpdesk")
	if err != nil {
		t.Fatalf("ActiveBundleDeploymentDetail: %v", err)
	}
	if got.Version != "1.0.1" || got.LockHash != "sha256:def" || got.Actor != "alice" {
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
		WillReturnRows(sqlmock.NewRows([]string{"id", "lock", "version"}).AddRow("bv-1", lock, "1.0.0"))

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

func TestQueries_ListBundleVersions_empty(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM bundle_versions`).
		WithArgs("bundle-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "version", "lock_hash", "created_at"}))

	q := New(sqlDB)
	rows, err := q.ListBundleVersions(context.Background(), "bundle-1")
	if err != nil {
		t.Fatalf("ListBundleVersions: %v", err)
	}
	if rows != nil && len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}
}

func TestQueries_ListBundleVersions_queryError(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM bundle_versions`).
		WithArgs("bundle-1").
		WillReturnError(errors.New("query failed"))

	q := New(sqlDB)
	_, err = q.ListBundleVersions(context.Background(), "bundle-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "query failed") {
		t.Fatalf("err = %v, want query failed", err)
	}
}

func TestQueries_ListBundleVersions(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	published := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	earlier := published.Add(-time.Hour)
	mock.ExpectQuery(`FROM bundle_versions`).
		WithArgs("bundle-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "version", "lock_hash", "created_at"}).
			AddRow("bv-2", "1.0.1", "sha256:def", published).
			AddRow("bv-1", "1.0.0", "sha256:abc", earlier))

	q := New(sqlDB)
	rows, err := q.ListBundleVersions(context.Background(), "bundle-1")
	if err != nil {
		t.Fatalf("ListBundleVersions: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].ID != "bv-2" || rows[0].Version != "1.0.1" || rows[0].LockHash != "sha256:def" {
		t.Fatalf("rows[0] = %+v", rows[0])
	}
}

func TestQueries_ListBundleDeployments_empty(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM bundle_deployments bd`).
		WithArgs("bundle-1").
		WillReturnRows(sqlmock.NewRows([]string{"version", "lock_hash", "action", "actor", "created_at"}))

	q := New(sqlDB)
	rows, err := q.ListBundleDeployments(context.Background(), "bundle-1")
	if err != nil {
		t.Fatalf("ListBundleDeployments: %v", err)
	}
	if rows != nil && len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}
}

func TestQueries_ListBundleDeployments_queryError(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM bundle_deployments bd`).
		WithArgs("bundle-1").
		WillReturnError(errors.New("query failed"))

	q := New(sqlDB)
	_, err = q.ListBundleDeployments(context.Background(), "bundle-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "query failed") {
		t.Fatalf("err = %v, want query failed", err)
	}
}

func TestQueries_ActiveBundleDeploymentDetail_notFound(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM bundle_deployments bd`).
		WithArgs("support", "missing").
		WillReturnError(sql.ErrNoRows)

	q := New(sqlDB)
	_, err = q.ActiveBundleDeploymentDetail(context.Background(), "support", "missing")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestQueries_ListBundleDeployments(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	earlier := now.Add(-time.Hour)
	mock.ExpectQuery(`FROM bundle_deployments bd`).
		WithArgs("bundle-1").
		WillReturnRows(sqlmock.NewRows([]string{"version", "lock_hash", "action", "actor", "created_at"}).
			AddRow("1.0.1", "sha256:def", "deploy", "alice", now).
			AddRow("1.0.0", "sha256:abc", "deploy", "bob", earlier))

	q := New(sqlDB)
	rows, err := q.ListBundleDeployments(context.Background(), "bundle-1")
	if err != nil {
		t.Fatalf("ListBundleDeployments: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].Version != "1.0.1" || rows[0].LockHash != "sha256:def" || rows[0].Action != "deploy" || rows[0].Actor != "alice" {
		t.Fatalf("rows[0] = %+v", rows[0])
	}
}

func TestQueries_BundleVersionIDBySemver(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM bundle_versions bv`).
		WithArgs("support", "helpdesk", "1.0.0").
		WillReturnRows(sqlmock.NewRows([]string{"id", "root_member_version_id", "lock_hash", "version"}).
			AddRow("bv-1", "root-ver", "sha256:abc", "1.0.0"))

	q := New(sqlDB)
	got, err := q.BundleVersionIDByLabel(context.Background(), "support", "helpdesk", "1.0.0")
	if err != nil {
		t.Fatalf("BundleVersionIDByLabel: %v", err)
	}
	if got.ID != "bv-1" || got.Version != "1.0.0" || got.LockHash != "sha256:abc" {
		t.Fatalf("got = %+v", got)
	}
}

func TestQueries_BundleVersionBySemver(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	mock.ExpectQuery(`FROM bundle_versions bv`).
		WithArgs("bundle-1", "1.0.0").
		WillReturnRows(sqlmock.NewRows([]string{"id", "lock_hash"}).
			AddRow("bv-1", "sha256:abc"))

	q := New(sqlDB)
	got, err := q.BundleVersionBySemver(context.Background(), "bundle-1", "1.0.0")
	if err != nil {
		t.Fatalf("BundleVersionBySemver: %v", err)
	}
	if got.ID != "bv-1" || got.LockHash != "sha256:abc" {
		t.Fatalf("got = %+v", got)
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
		WillReturnRows(sqlmock.NewRows([]string{"id", "root_member_version_id", "lock_hash", "version"}).
			AddRow("bv-1", "root-ver", "sha256:abc", "1.0.0"))

	q := New(sqlDB)
	got, err := q.BundleVersionIDByLabel(context.Background(), "support", "helpdesk", "sha256:abc")
	if err != nil {
		t.Fatalf("BundleVersionIDByLabel: %v", err)
	}
	if got.ID != "bv-1" || got.RootMemberVersionID != "root-ver" {
		t.Fatalf("got = %+v", got)
	}
}
