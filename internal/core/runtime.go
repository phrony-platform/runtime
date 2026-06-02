package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/jmoiron/sqlx"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/secrets"
	"github.com/phrony-platform/runtime/internal/store"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
	"github.com/phrony-platform/runtime/internal/version"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type runtimeServer struct {
	runtimev1.UnimplementedRuntimeServer
	db         *sqlx.DB
	secretsEnc *secrets.Encryptor
	// loadSessionVersionFn overrides version loading for interactive sessions (tests only).
	loadSessionVersionFn func(context.Context, *store.Queries, string) (*executor.Version, error)
	// startRunSessionBackgroundFn replaces detached session startup (tests only).
	startRunSessionBackgroundFn func(sessionID, agentVersionID string, inputJSON json.RawMessage)
	// activeSessions tracks session IDs with an open interactive stream.
	activeSessions *sync.Map
	approvalCoordinator *approvalCoordinator
	// toolRegistry routes Work-stream workers; toolDispatch is the executor-facing facade.
	toolRegistry *tooldispatch.WorkerRegistry
	toolDispatch tooldispatch.Dispatcher
}

// queries returns a store handle backed by the configured database, or a
// FailedPrecondition error when the server was started without one.
func (s *runtimeServer) queries() (*store.Queries, error) {
	if s.db == nil {
		return nil, status.Error(codes.FailedPrecondition, "database is not configured")
	}
	return store.New(s.db.DB), nil
}

func (s *runtimeServer) GetVersion(ctx context.Context, _ *runtimev1.GetVersionRequest) (*runtimev1.GetVersionResponse, error) {
	resp := &runtimev1.GetVersionResponse{Version: version.Version}
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

