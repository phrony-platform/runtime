package core

import (
	"context"
	"database/sql"
	"errors"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func resolveAgentByRef(ctx context.Context, q *store.Queries, ref *runtimev1.AgentRef) (store.AgentByIDRow, error) {
	namespace, name, err := agentref.FromProto(ref)
	if err != nil {
		return store.AgentByIDRow{}, err
	}

	agent, err := q.AgentByNamespaceName(ctx, namespace, name)
	if errors.Is(err, sql.ErrNoRows) {
		return store.AgentByIDRow{}, status.Errorf(codes.NotFound, "agent %s not found", agentref.Format(namespace, name))
	}
	if err != nil {
		return store.AgentByIDRow{}, status.Errorf(codes.Internal, "lookup agent: %v", err)
	}
	return agent, nil
}
