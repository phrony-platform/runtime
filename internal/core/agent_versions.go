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

func (s *runtimeServer) GetAgentVersion(ctx context.Context, req *runtimev1.GetAgentVersionRequest) (*runtimev1.GetAgentVersionResponse, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
	}

	ref := req.GetAgentRef()
	if ref == nil || ref.GetVersion() == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_ref requires namespace, name, and version")
	}

	ns, name, err := agentref.FromProto(ref)
	if err != nil {
		return nil, err
	}
	agentRef := agentref.Format(ns, name)

	row, err := q.GetAgentVersionByLabel(ctx, ns, name, ref.GetVersion())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Errorf(codes.NotFound, "no published version %q for agent %s", ref.GetVersion(), agentRef)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get agent version: %v", err)
	}

	return &runtimev1.GetAgentVersionResponse{
		Manifest:     row.Manifest,
		ContentHash:  row.ContentHash,
		Version:      row.Version,
		PublishedAt:  formatTime(row.PublishedAt),
		DeprecatedAt: formatOptionalTime(row.DeprecatedAt),
		RetiredAt:    formatOptionalTime(row.RetiredAt),
	}, nil
}
