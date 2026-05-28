package core

import (
	"context"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type runtimeServer struct {
	runtimev1.UnimplementedRuntimeServer
}

func (s *runtimeServer) GetVersion(context.Context, *runtimev1.GetVersionRequest) (*runtimev1.GetVersionResponse, error) {
	return &runtimev1.GetVersionResponse{Version: RuntimeVersion}, nil
}

func (s *runtimeServer) RunSession(context.Context, *runtimev1.RunSessionRequest) (*runtimev1.RunSessionResponse, error) {
	return nil, status.Error(codes.Unimplemented, "RunSession is not implemented yet")
}

func (s *runtimeServer) Deploy(context.Context, *runtimev1.DeployRequest) (*runtimev1.DeployResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Deploy is not implemented yet")
}
