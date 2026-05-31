package core

import (
	"context"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *runtimeServer) ListSessions(ctx context.Context, req *runtimev1.ListSessionsRequest) (*runtimev1.ListSessionsResponse, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
	}

	agentVersionID, err := resolveAgentVersionID(ctx, s.db.DB, req.GetAgentRef())
	if err != nil {
		return nil, err
	}

	rows, err := q.ListSessionsByAgentVersionID(ctx, store.ListSessionsByAgentVersionIDParams{
		AgentVersionID: agentVersionID,
		Status:         req.GetStatus(),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list sessions: %v", err)
	}

	out := make([]*runtimev1.SessionSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, &runtimev1.SessionSummary{
			Id:             row.ID,
			AgentVersionId: row.AgentVersionID,
			Status:         row.Status,
			CreatedAt:      formatTime(row.CreatedAt),
			UpdatedAt:      formatTime(row.UpdatedAt),
		})
	}
	return &runtimev1.ListSessionsResponse{Sessions: out}, nil
}
