package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"google.golang.org/grpc/codes"
)

func TestRuntime_RunSession_bundleActiveDeployment(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM bundle_deployments bd`).
		WithArgs("playground", "support").
		WillReturnRows(sqlmock.NewRows([]string{"id", "version", "lock_hash", "root_member_version_id"}).
			AddRow("bv-1", "1.0.0", "sha256:abc", "root-ver"))
	manifest := []byte(runSessionTestManifestJSON)
	mock.ExpectQuery(`SELECT manifest`).
		WithArgs("root-ver").
		WillReturnRows(sqlmock.NewRows([]string{"manifest"}).AddRow(manifest))
	expectBundleMemberManifestsEmpty(mock, "bv-1")
	expectInsertSessionWithStartedEvent(mock, "root-ver", []byte(`{"message":"hi"}`), nil, 0, "bv-1", sqlmock.AnyArg())

	srv := &runtimeServer{
		db: db,
		startRunSessionBackgroundFn: func(string, string, json.RawMessage) {},
	}
	resp, err := srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{
		BundleRef: &runtimev1.BundleRef{
			Namespace: "playground",
			Name:      "support",
		},
		Input: []byte(`{"message":"hi"}`),
	})
	if err != nil {
		t.Fatalf("RunSession: %v", err)
	}
	if resp.GetSessionId() == "" || resp.GetAgentVersionId() != "root-ver" {
		t.Fatalf("resp = %+v", resp)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRuntime_RunSession_bundleExplicitSemverActive(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM bundle_versions bv`).
		WithArgs("playground", "support", "1.0.0").
		WillReturnRows(sqlmock.NewRows([]string{"id", "root_member_version_id", "lock_hash", "version"}).
			AddRow("bv-1", "root-ver", "sha256:abc", "1.0.0"))
	mock.ExpectQuery(`FROM bundle_deployments bd`).
		WithArgs("playground", "support").
		WillReturnRows(sqlmock.NewRows([]string{"id", "version", "lock_hash", "root_member_version_id"}).
			AddRow("bv-1", "1.0.0", "sha256:abc", "root-ver"))
	manifest := []byte(runSessionTestManifestJSON)
	mock.ExpectQuery(`SELECT manifest`).
		WithArgs("root-ver").
		WillReturnRows(sqlmock.NewRows([]string{"manifest"}).AddRow(manifest))
	expectBundleMemberManifestsEmpty(mock, "bv-1")
	expectInsertSessionWithStartedEvent(mock, "root-ver", []byte(`{"message":"hi"}`), nil, 0, "bv-1", sqlmock.AnyArg())

	srv := &runtimeServer{
		db: db,
		startRunSessionBackgroundFn: func(string, string, json.RawMessage) {},
	}
	resp, err := srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{
		BundleRef: &runtimev1.BundleRef{
			Namespace: "playground",
			Name:      "support",
			Version:   "1.0.0",
		},
		Input: []byte(`{"message":"hi"}`),
	})
	if err != nil {
		t.Fatalf("RunSession: %v", err)
	}
	if resp.GetSessionId() == "" || resp.GetAgentVersionId() != "root-ver" {
		t.Fatalf("resp = %+v", resp)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func expectBundleMemberManifestsEmpty(mock sqlmock.Sqlmock, bundleVersionID string) {
	mock.ExpectQuery(`FROM bundle_members bm`).
		WithArgs(bundleVersionID).
		WillReturnRows(sqlmock.NewRows([]string{"manifest", "child_name", "origin"}))
}

func TestRuntime_RunSession_rejectsBothRefs(t *testing.T) {
	srv := &runtimeServer{db: testServeDB(t)}
	_, err := srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{
		AgentRef:  &runtimev1.AgentRef{Namespace: "demo", Name: "echo"},
		BundleRef: &runtimev1.BundleRef{Namespace: "playground", Name: "support"},
	})
	assertGRPCCode(t, err, codes.InvalidArgument)
}

func TestRuntime_RunSession_bundleNoActiveDeployment(t *testing.T) {
	db, mock := testSQLxDB(t)
	mock.ExpectQuery(`FROM bundle_deployments bd`).
		WithArgs("support", "helpdesk").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`FROM bundles`).
		WithArgs("support", "helpdesk").
		WillReturnRows(sqlmock.NewRows([]string{"id", "namespace", "name"}).
			AddRow("bundle-1", "support", "helpdesk"))

	srv := &runtimeServer{db: db}
	_, err := srv.RunSession(context.Background(), &runtimev1.RunSessionRequest{
		BundleRef: &runtimev1.BundleRef{
			Namespace: "support",
			Name:      "helpdesk",
		},
	})
	assertGRPCCode(t, err, codes.FailedPrecondition)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
