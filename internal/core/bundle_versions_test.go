package core

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"google.golang.org/grpc/codes"
)

func TestRuntime_GetActiveBundle_success(t *testing.T) {
	db, mock := testSQLxDB(t)
	deployed := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`FROM bundle_deployments bd`).
		WithArgs("demo", "payment-desk-hitl").
		WillReturnRows(sqlmock.NewRows([]string{"version", "lock_hash", "created_at", "actor"}).
			AddRow("1.0.0", "sha256:abc", deployed, "alice"))

	srv := &runtimeServer{db: db}
	resp, err := srv.GetActiveBundle(context.Background(), &runtimev1.GetActiveBundleRequest{
		BundleRef: &runtimev1.BundleRef{Namespace: "demo", Name: "payment-desk-hitl"},
	})
	if err != nil {
		t.Fatalf("GetActiveBundle: %v", err)
	}
	if resp.GetVersion() != "1.0.0" || resp.GetLockHash() != "sha256:abc" || resp.GetActor() != "alice" {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.GetDeployedAt() == "" {
		t.Fatal("deployed_at is empty")
	}
}

func TestRuntime_GetActiveBundle_missingBundleRef(t *testing.T) {
	srv := &runtimeServer{db: testServeDB(t)}
	_, err := srv.GetActiveBundle(context.Background(), &runtimev1.GetActiveBundleRequest{})
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestRuntime_GetActiveBundle_noDeployment(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM bundle_deployments bd`).
		WithArgs("demo", "payment-desk-hitl").
		WillReturnError(sql.ErrNoRows)

	srv := &runtimeServer{db: db}
	_, err := srv.GetActiveBundle(context.Background(), &runtimev1.GetActiveBundleRequest{
		BundleRef: &runtimev1.BundleRef{Namespace: "demo", Name: "payment-desk-hitl"},
	})
	assertGRPCCode(t, err, codes.FailedPrecondition)
	if !strings.Contains(statusMessage(t, err), "no active deployment") {
		t.Fatalf("error = %v, want no active deployment", err)
	}
}

func TestRuntime_ListBundles_success(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM bundles`).
		WithArgs("").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name", "owner", "created_at"}).
			AddRow("bundle-1", "demo", "payment-desk-hitl", "team", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))

	srv := &runtimeServer{db: db}
	resp, err := srv.ListBundles(context.Background(), &runtimev1.ListBundlesRequest{})
	if err != nil {
		t.Fatalf("ListBundles: %v", err)
	}
	if len(resp.GetBundles()) != 1 {
		t.Fatalf("bundles = %d, want 1", len(resp.GetBundles()))
	}
	if resp.GetBundles()[0].GetId() != "bundle-1" {
		t.Fatalf("id = %q, want bundle-1", resp.GetBundles()[0].GetId())
	}
}

func TestRuntime_ListBundleVersions_success(t *testing.T) {
	db, mock := testSQLxDB(t)
	bundleID := "bundle-1"
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	earlier := now.Add(-time.Hour)
	mock.ExpectQuery(`FROM bundles`).
		WithArgs("demo", "payment-desk-hitl").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name"}).
			AddRow(bundleID, "demo", "payment-desk-hitl"))
	mock.ExpectQuery(`FROM bundle_versions`).
		WithArgs(bundleID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "version", "lock_hash", "created_at"}).
			AddRow("bv-2", "1.0.1", "sha256:def", now).
			AddRow("bv-1", "1.0.0", "sha256:abc", earlier))

	srv := &runtimeServer{db: db}
	resp, err := srv.ListBundleVersions(context.Background(), &runtimev1.ListBundleVersionsRequest{
		BundleRef: &runtimev1.BundleRef{Namespace: "demo", Name: "payment-desk-hitl"},
	})
	if err != nil {
		t.Fatalf("ListBundleVersions: %v", err)
	}
	if len(resp.GetVersions()) != 2 {
		t.Fatalf("versions = %d, want 2", len(resp.GetVersions()))
	}
	if resp.GetVersions()[0].GetVersion() != "1.0.1" || resp.GetVersions()[0].GetLockHash() != "sha256:def" || resp.GetVersions()[0].GetId() != "bv-2" {
		t.Fatalf("first = %+v", resp.GetVersions()[0])
	}
}

func TestRuntime_ListBundleVersions_missingBundleRef(t *testing.T) {
	srv := &runtimeServer{db: testServeDB(t)}
	_, err := srv.ListBundleVersions(context.Background(), &runtimev1.ListBundleVersionsRequest{})
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestRuntime_ListBundleVersions_empty(t *testing.T) {
	db, mock := testSQLxDB(t)
	bundleID := "bundle-1"
	mock.ExpectQuery(`FROM bundles`).
		WithArgs("demo", "payment-desk-hitl").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name"}).
			AddRow(bundleID, "demo", "payment-desk-hitl"))
	mock.ExpectQuery(`FROM bundle_versions`).
		WithArgs(bundleID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "version", "lock_hash", "created_at"}))

	srv := &runtimeServer{db: db}
	resp, err := srv.ListBundleVersions(context.Background(), &runtimev1.ListBundleVersionsRequest{
		BundleRef: &runtimev1.BundleRef{Namespace: "demo", Name: "payment-desk-hitl"},
	})
	if err != nil {
		t.Fatalf("ListBundleVersions: %v", err)
	}
	if len(resp.GetVersions()) != 0 {
		t.Fatalf("versions = %d, want 0", len(resp.GetVersions()))
	}
}

func TestRuntime_ListBundleVersions_listQueryFailed(t *testing.T) {
	db, mock := testSQLxDB(t)
	bundleID := "bundle-1"
	mock.ExpectQuery(`FROM bundles`).
		WithArgs("demo", "payment-desk-hitl").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name"}).
			AddRow(bundleID, "demo", "payment-desk-hitl"))
	mock.ExpectQuery(`FROM bundle_versions`).
		WithArgs(bundleID).
		WillReturnError(errors.New("list failed"))

	srv := &runtimeServer{db: db}
	_, err := srv.ListBundleVersions(context.Background(), &runtimev1.ListBundleVersionsRequest{
		BundleRef: &runtimev1.BundleRef{Namespace: "demo", Name: "payment-desk-hitl"},
	})
	assertGRPCCode(t, err, codes.Internal)
	if !strings.Contains(statusMessage(t, err), "list bundle versions") {
		t.Fatalf("error = %v, want list bundle versions", err)
	}
}

func TestRuntime_ListBundleVersions_bundleNotFound(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM bundles`).
		WithArgs("demo", "missing").
		WillReturnError(sql.ErrNoRows)

	srv := &runtimeServer{db: db}
	_, err := srv.ListBundleVersions(context.Background(), &runtimev1.ListBundleVersionsRequest{
		BundleRef: &runtimev1.BundleRef{Namespace: "demo", Name: "missing"},
	})
	assertGRPCCode(t, err, codes.NotFound)
}

func TestRuntime_ListBundleDeployments_success(t *testing.T) {
	db, mock := testSQLxDB(t)
	bundleID := "bundle-1"
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	earlier := now.Add(-time.Hour)
	mock.ExpectQuery(`FROM bundles`).
		WithArgs("demo", "payment-desk-hitl").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name"}).
			AddRow(bundleID, "demo", "payment-desk-hitl"))
	mock.ExpectQuery(`FROM bundle_deployments bd`).
		WithArgs(bundleID).
		WillReturnRows(sqlmock.NewRows([]string{"version", "lock_hash", "action", "actor", "created_at"}).
			AddRow("1.0.1", "sha256:def", "deploy", "alice", now).
			AddRow("1.0.0", "sha256:abc", "deploy", "bob", earlier))

	srv := &runtimeServer{db: db}
	resp, err := srv.ListBundleDeployments(context.Background(), &runtimev1.ListBundleDeploymentsRequest{
		BundleRef: &runtimev1.BundleRef{Namespace: "demo", Name: "payment-desk-hitl"},
	})
	if err != nil {
		t.Fatalf("ListBundleDeployments: %v", err)
	}
	if len(resp.GetDeployments()) != 2 {
		t.Fatalf("deployments = %d, want 2", len(resp.GetDeployments()))
	}
	if resp.GetDeployments()[0].GetVersion() != "1.0.1" || resp.GetDeployments()[0].GetLockHash() != "sha256:def" || resp.GetDeployments()[0].GetAction() != "deploy" {
		t.Fatalf("first = %+v", resp.GetDeployments()[0])
	}
}

func TestRuntime_ListBundleDeployments_missingBundleRef(t *testing.T) {
	srv := &runtimeServer{db: testServeDB(t)}
	_, err := srv.ListBundleDeployments(context.Background(), &runtimev1.ListBundleDeploymentsRequest{})
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestRuntime_ListBundleDeployments_bundleNotFound(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM bundles`).
		WithArgs("demo", "missing").
		WillReturnError(sql.ErrNoRows)

	srv := &runtimeServer{db: db}
	_, err := srv.ListBundleDeployments(context.Background(), &runtimev1.ListBundleDeploymentsRequest{
		BundleRef: &runtimev1.BundleRef{Namespace: "demo", Name: "missing"},
	})
	assertGRPCCode(t, err, codes.NotFound)
}

func TestRuntime_ListBundleDeployments_empty(t *testing.T) {
	db, mock := testSQLxDB(t)
	bundleID := "bundle-1"
	mock.ExpectQuery(`FROM bundles`).
		WithArgs("demo", "payment-desk-hitl").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name"}).
			AddRow(bundleID, "demo", "payment-desk-hitl"))
	mock.ExpectQuery(`FROM bundle_deployments bd`).
		WithArgs(bundleID).
		WillReturnRows(sqlmock.NewRows([]string{"version", "lock_hash", "action", "actor", "created_at"}))

	srv := &runtimeServer{db: db}
	resp, err := srv.ListBundleDeployments(context.Background(), &runtimev1.ListBundleDeploymentsRequest{
		BundleRef: &runtimev1.BundleRef{Namespace: "demo", Name: "payment-desk-hitl"},
	})
	if err != nil {
		t.Fatalf("ListBundleDeployments: %v", err)
	}
	if len(resp.GetDeployments()) != 0 {
		t.Fatalf("deployments = %d, want 0", len(resp.GetDeployments()))
	}
}

func TestRuntime_ListBundleDeployments_listQueryFailed(t *testing.T) {
	db, mock := testSQLxDB(t)
	bundleID := "bundle-1"
	mock.ExpectQuery(`FROM bundles`).
		WithArgs("demo", "payment-desk-hitl").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name"}).
			AddRow(bundleID, "demo", "payment-desk-hitl"))
	mock.ExpectQuery(`FROM bundle_deployments bd`).
		WithArgs(bundleID).
		WillReturnError(errors.New("list failed"))

	srv := &runtimeServer{db: db}
	_, err := srv.ListBundleDeployments(context.Background(), &runtimev1.ListBundleDeploymentsRequest{
		BundleRef: &runtimev1.BundleRef{Namespace: "demo", Name: "payment-desk-hitl"},
	})
	assertGRPCCode(t, err, codes.Internal)
	if !strings.Contains(statusMessage(t, err), "list bundle deployments") {
		t.Fatalf("error = %v, want list bundle deployments", err)
	}
}
