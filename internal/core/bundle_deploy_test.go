package core

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"google.golang.org/grpc/codes"
)

func TestRuntime_DeployBundle_success(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM bundles`).
		WithArgs("support", "helpdesk").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name"}).
			AddRow("bundle-1", "support", "helpdesk"))
	mock.ExpectQuery(`FROM bundle_versions bv`).
		WithArgs("support", "helpdesk", "sha256:abc").
		WillReturnRows(sqlmock.NewRows([]string{"id", "root_member_version_id"}).
			AddRow("bv-1", "root-ver"))
	mock.ExpectQuery(`FROM bundle_deployments bd`).
		WithArgs("support", "helpdesk").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO bundle_deployments`).
		WithArgs(sqlmock.AnyArg(), "bundle-1", "bv-1", "deploy", "alice").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("dep-1"))
	mock.ExpectCommit()

	srv := &runtimeServer{db: db}
	resp, err := srv.DeployBundle(context.Background(), &runtimev1.DeployBundleRequest{
		BundleRef: &runtimev1.BundleRef{
			Namespace: "support",
			Name:      "helpdesk",
			Version:   "sha256:abc",
		},
		Actor: "alice",
	})
	if err != nil {
		t.Fatalf("DeployBundle: %v", err)
	}
	if resp.GetVersion() != "sha256:abc" || resp.GetPreviousVersion() != "" {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.GetNamespace() != "support" || resp.GetName() != "helpdesk" {
		t.Fatalf("identity = %s/%s", resp.GetNamespace(), resp.GetName())
	}
	if resp.GetDeployedAt() == "" {
		t.Fatal("deployed_at is empty")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_DeployBundle_withPreviousActive(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM bundles`).
		WithArgs("support", "helpdesk").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name"}).
			AddRow("bundle-1", "support", "helpdesk"))
	mock.ExpectQuery(`FROM bundle_versions bv`).
		WithArgs("support", "helpdesk", "sha256:new").
		WillReturnRows(sqlmock.NewRows([]string{"id", "root_member_version_id"}).
			AddRow("bv-2", "root-ver"))
	mock.ExpectQuery(`FROM bundle_deployments bd`).
		WithArgs("support", "helpdesk").
		WillReturnRows(sqlmock.NewRows([]string{"id", "lock_hash", "root_member_version_id"}).
			AddRow("bv-1", "sha256:old", "root-ver"))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO bundle_deployments`).
		WithArgs(sqlmock.AnyArg(), "bundle-1", "bv-2", "deploy", "bob").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("dep-2"))
	mock.ExpectCommit()

	srv := &runtimeServer{db: db}
	resp, err := srv.DeployBundle(context.Background(), &runtimev1.DeployBundleRequest{
		BundleRef: &runtimev1.BundleRef{
			Namespace: "support",
			Name:      "helpdesk",
			Version:   "sha256:new",
		},
		Actor: "bob",
	})
	if err != nil {
		t.Fatalf("DeployBundle: %v", err)
	}
	if resp.GetPreviousVersion() != "sha256:old" {
		t.Fatalf("previous_version = %q, want sha256:old", resp.GetPreviousVersion())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_DeployBundle_missingBundleRef(t *testing.T) {
	srv := &runtimeServer{db: testServeDB(t)}
	_, err := srv.DeployBundle(context.Background(), &runtimev1.DeployBundleRequest{})
	assertGRPCCode(t, err, codes.InvalidArgument)
}
