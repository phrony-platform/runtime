package core

import (
	"fmt"
	"sync"

	"github.com/jmoiron/sqlx"
	"google.golang.org/grpc"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	grpc_health_v1 "github.com/phrony-platform/runtime/gen/grpc/health/v1"
	"github.com/phrony-platform/runtime/internal/secrets"
	"github.com/phrony-platform/runtime/internal/store"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

// Server hosts the runtime gRPC control plane.
type Server struct {
	grpc    *grpc.Server
	db      *sqlx.DB
	runtime *runtimeServer
}

// NewServer registers Runtime and Health services on a new gRPC server.
// Returns an error when RUNTIME_SECRETS_ENCRYPTION_KEY is set but invalid.
func NewServer(db *sqlx.DB) (*Server, error) {
	enc, err := secrets.NewEncryptorFromEnv()
	if err != nil {
		return nil, err
	}
	grpcSrv := grpc.NewServer()
	toolCfg := tooldispatch.DefaultRegistryConfig()
	if allowlist, err := tooldispatch.LoadAllowlistFromEnv(); err != nil {
		return nil, fmt.Errorf("load tool allowlist: %w", err)
	} else if allowlist != nil {
		toolCfg.IntegrityCheck = allowlist.Checker()
	}
	toolReg := tooldispatch.NewWorkerRegistry(toolCfg)
	toolReg.SetInvocationRecorder(NewToolInvocationRecorder(store.New(db)))
	rs := &runtimeServer{
		db:             db,
		secretsEnc:     enc,
		activeSessions: &sync.Map{},
		toolRegistry:   toolReg,
		toolDispatch:   &tooldispatch.StreamDispatcher{Registry: toolReg},
	}
	runtimev1.RegisterRuntimeServer(grpcSrv, rs)
	grpc_health_v1.RegisterHealthServer(grpcSrv, &healthServer{db: db})
	return &Server{grpc: grpcSrv, db: db, runtime: rs}, nil
}

// GRPC returns the underlying gRPC server (for tests).
func (s *Server) GRPC() *grpc.Server {
	return s.grpc
}
