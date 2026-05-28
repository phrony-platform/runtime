package core

import (
	"github.com/jmoiron/sqlx"
	"google.golang.org/grpc"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	grpc_health_v1 "github.com/phrony-platform/runtime/gen/grpc/health/v1"
)

// Server hosts the runtime gRPC control plane.
type Server struct {
	grpc *grpc.Server
	db   *sqlx.DB
}

// NewServer registers Runtime and Health services on a new gRPC server.
func NewServer(db *sqlx.DB) *Server {
	grpcSrv := grpc.NewServer()
	runtimev1.RegisterRuntimeServer(grpcSrv, &runtimeServer{})
	grpc_health_v1.RegisterHealthServer(grpcSrv, &healthServer{db: db})
	return &Server{grpc: grpcSrv, db: db}
}

// GRPC returns the underlying gRPC server (for tests).
func (s *Server) GRPC() *grpc.Server {
	return s.grpc
}
