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

func (s *runtimeServer) RetireAgentVersion(ctx context.Context, req *runtimev1.RetireAgentVersionRequest) (*runtimev1.RetireAgentVersionResponse, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
	}

	ref := req.GetAgentRef()
	if ref == nil || ref.GetVersion() == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_ref requires namespace, name, and version")
	}

	agent, err := resolveAgentByRef(ctx, q, ref)
	if err != nil {
		return nil, err
	}
	if agent.ArchivedAt.Valid {
		return nil, status.Errorf(codes.FailedPrecondition, "agent %s is archived", agentref.Format(agent.Namespace, agent.Name))
	}

	versionID, err := q.RetireAgentVersion(ctx, agent.ID, ref.GetVersion())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Errorf(codes.NotFound, "no version %q for agent %s",
			ref.GetVersion(), agentref.Format(agent.Namespace, agent.Name))
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "retire version: %v", err)
	}

	return &runtimev1.RetireAgentVersionResponse{VersionId: versionID}, nil
}
