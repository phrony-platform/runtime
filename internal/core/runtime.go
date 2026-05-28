package core

import (
	"context"
	"database/sql"
	"errors"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/jmoiron/sqlx"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type runtimeServer struct {
	runtimev1.UnimplementedRuntimeServer
	db *sqlx.DB
}

func (s *runtimeServer) GetVersion(ctx context.Context, _ *runtimev1.GetVersionRequest) (*runtimev1.GetVersionResponse, error) {
	resp := &runtimev1.GetVersionResponse{Version: RuntimeVersion}
	if schemaVersion, ok := s.lookupSchemaVersion(ctx); ok {
		resp.SchemaVersion = schemaVersion
	}
	return resp, nil
}

func (s *runtimeServer) lookupSchemaVersion(ctx context.Context) (string, bool) {
	if s.db == nil {
		return "", false
	}
	value, err := store.New(s.db.DB).GetRuntimeMetaValue(ctx, SchemaMetaVersionKey)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return "", false
	}
	return value, true
}

func (s *runtimeServer) RunSession(context.Context, *runtimev1.RunSessionRequest) (*runtimev1.RunSessionResponse, error) {
	return nil, status.Error(codes.Unimplemented, "RunSession is not implemented yet")
}
