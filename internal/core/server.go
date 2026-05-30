package core

import (
	"github.com/jmoiron/sqlx"
	"google.golang.org/grpc"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	grpc_health_v1 "github.com/phrony-platform/runtime/gen/grpc/health/v1"
	"github.com/phrony-platform/runtime/internal/secrets"
)

// Server hosts the runtime gRPC control plane.
type Server struct {
	grpc *grpc.Server
	db   *sqlx.DB
}

// NewServer registers Runtime and Health services on a new gRPC server.
// Returns an error when RUNTIME_SECRETS_ENCRYPTION_KEY is set but invalid.
func NewServer(db *sqlx.DB) (*Server, error) {
	enc, err := secrets.NewEncryptorFromEnv()
	if err != nil {
		return nil, err
	}
	grpcSrv := grpc.NewServer()
	runtimev1.RegisterRuntimeServer(grpcSrv, &runtimeServer{db: db, secretsEnc: enc})
	grpc_health_v1.RegisterHealthServer(grpcSrv, &healthServer{db: db})
	return &Server{grpc: grpcSrv, db: db}, nil
}

// GRPC returns the underlying gRPC server (for tests).
func (s *Server) GRPC() *grpc.Server {
	return s.grpc
}
