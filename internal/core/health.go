package core

import (
	"context"

	grpc_health_v1 "github.com/phrony-platform/runtime/gen/grpc/health/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

const healthServiceRuntime = "phrony.runtime.v1.Runtime"

type healthServer struct {
	grpc_health_v1.UnimplementedHealthServer
	db *gorm.DB
}

func (h *healthServer) Check(ctx context.Context, req *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	switch req.GetService() {
	case "", healthServiceRuntime:
		return h.checkReady(ctx), nil
	default:
		return &grpc_health_v1.HealthCheckResponse{
			Status: grpc_health_v1.HealthCheckResponse_SERVICE_UNKNOWN,
		}, nil
	}
}

func (h *healthServer) List(ctx context.Context, _ *grpc_health_v1.HealthListRequest) (*grpc_health_v1.HealthListResponse, error) {
	ready := h.checkReady(ctx)
	return &grpc_health_v1.HealthListResponse{
		Statuses: map[string]grpc_health_v1.HealthCheckResponse_ServingStatus{
			"":                      ready.GetStatus(),
			healthServiceRuntime:    ready.GetStatus(),
		},
	}, nil
}

func (h *healthServer) Watch(*grpc_health_v1.HealthCheckRequest, grpc_health_v1.Health_WatchServer) error {
	return status.Error(codes.Unimplemented, "Watch is not implemented yet")
}

// checkReady reports SERVING when the database accepts SELECT 1.
func (h *healthServer) checkReady(ctx context.Context) *grpc_health_v1.HealthCheckResponse {
	if err := pingDB(ctx, h.db); err != nil {
		return &grpc_health_v1.HealthCheckResponse{
			Status: grpc_health_v1.HealthCheckResponse_NOT_SERVING,
		}
	}
	return &grpc_health_v1.HealthCheckResponse{
		Status: grpc_health_v1.HealthCheckResponse_SERVING,
	}
}
