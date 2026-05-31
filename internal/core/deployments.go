package core

import (
	"context"
	"database/sql"
	"errors"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *runtimeServer) GetActiveVersion(ctx context.Context, req *runtimev1.GetActiveVersionRequest) (*runtimev1.GetActiveVersionResponse, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
	}

	ns, name, err := agentref.FromProto(req.GetAgentRef())
	if err != nil {
		return nil, err
	}
	agentRef := agentref.Format(ns, name)

	detail, err := q.ActiveDeploymentDetail(ctx, ns, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Errorf(codes.FailedPrecondition, "no active deployment for agent %s", agentRef)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "resolve active deployment: %v", err)
	}

	return &runtimev1.GetActiveVersionResponse{
		Version:     detail.Version,
		DeployedAt:  formatTime(detail.DeployedAt),
		Actor:       detail.Actor,
	}, nil
}

func (s *runtimeServer) ListDeployments(ctx context.Context, req *runtimev1.ListDeploymentsRequest) (*runtimev1.ListDeploymentsResponse, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
	}

	agent, err := resolveAgentByRef(ctx, q, req.GetAgentRef())
	if err != nil {
		return nil, err
	}

	rows, err := q.ListDeployments(ctx, agent.ID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list deployments: %v", err)
	}

	out := make([]*runtimev1.DeploymentEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, &runtimev1.DeploymentEntry{
			Version:   row.Version,
			Action:    row.Action,
			Actor:     row.Actor,
			CreatedAt: formatTime(row.CreatedAt),
		})
	}
	return &runtimev1.ListDeploymentsResponse{Deployments: out}, nil
}
