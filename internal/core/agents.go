package core

import (
	"context"
	"database/sql"
	"errors"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *runtimeServer) ListAgents(ctx context.Context, req *runtimev1.ListAgentsRequest) (*runtimev1.ListAgentsResponse, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
	}
	rows, err := q.ListAgents(ctx, req.GetNamespace())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list agents: %v", err)
	}

	out := make([]*runtimev1.AgentSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, &runtimev1.AgentSummary{
			Id:         row.ID,
			Namespace:  row.Namespace,
			Name:       row.Name,
			Owner:      row.Owner,
			ArchivedAt: formatOptionalTime(row.ArchivedAt),
		})
	}
	return &runtimev1.ListAgentsResponse{Agents: out}, nil
}

func (s *runtimeServer) ListAgentVersions(ctx context.Context, req *runtimev1.ListAgentVersionsRequest) (*runtimev1.ListAgentVersionsResponse, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
	}

	agent, err := resolveAgentByRef(ctx, q, req.GetAgentRef())
	if err != nil {
		return nil, err
	}

	rows, err := q.ListAgentVersions(ctx, agent.ID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list agent versions: %v", err)
	}

	out := make([]*runtimev1.AgentVersionSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, &runtimev1.AgentVersionSummary{
			Id:           row.ID,
			Version:      row.Version,
			ContentHash:  row.ContentHash,
			DeployedAt:   formatTime(row.DeployedAt),
			DeprecatedAt: formatOptionalTime(row.DeprecatedAt),
		})
	}
	return &runtimev1.ListAgentVersionsResponse{Versions: out}, nil
}

func (s *runtimeServer) DeprecateAgentVersion(ctx context.Context, req *runtimev1.DeprecateAgentVersionRequest) (*runtimev1.DeprecateAgentVersionResponse, error) {
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

	versionID, err := q.DeprecateAgentVersion(ctx, agent.ID, ref.GetVersion())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Errorf(codes.NotFound, "no active version %q for agent %s",
			ref.GetVersion(), agentref.Format(agent.Namespace, agent.Name))
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "deprecate version: %v", err)
	}

	return &runtimev1.DeprecateAgentVersionResponse{VersionId: versionID}, nil
}

func (s *runtimeServer) ArchiveAgent(ctx context.Context, req *runtimev1.ArchiveAgentRequest) (*runtimev1.ArchiveAgentResponse, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
	}

	agent, err := resolveAgentByRef(ctx, q, req.GetAgentRef())
	if err != nil {
		return nil, err
	}
	agentRef := agentref.Format(agent.Namespace, agent.Name)

	tx, err := s.db.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	txQ := q.WithTx(tx)
	if _, err := txQ.ArchiveAgent(ctx, agent.ID); errors.Is(err, sql.ErrNoRows) {
		return nil, status.Errorf(codes.FailedPrecondition, "agent %s is already archived", agentRef)
	} else if err != nil {
		return nil, status.Errorf(codes.Internal, "archive agent: %v", err)
	}

	if err := txQ.DeprecateAllAgentVersions(ctx, agent.ID); err != nil {
		return nil, status.Errorf(codes.Internal, "deprecate agent versions: %v", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, status.Errorf(codes.Internal, "commit transaction: %v", err)
	}
	return &runtimev1.ArchiveAgentResponse{}, nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func formatOptionalTime(t sql.NullTime) string {
	if !t.Valid {
		return ""
	}
	return formatTime(t.Time)
}
