package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func agentRefFromRequest(ref *runtimev1.AgentRef) (namespace, name string, err error) {
	if ref == nil || ref.GetNamespace() == "" || ref.GetName() == "" {
		return "", "", status.Error(codes.InvalidArgument, "agent_ref requires namespace and name")
	}
	return ref.GetNamespace(), ref.GetName(), nil
}

func resolveAgentByRef(ctx context.Context, q *store.Queries, ref *runtimev1.AgentRef) (store.AgentByIDRow, error) {
	namespace, name, err := agentRefFromRequest(ref)
	if err != nil {
		return store.AgentByIDRow{}, err
	}

	agent, err := q.AgentByNamespaceName(ctx, namespace, name)
	if errors.Is(err, sql.ErrNoRows) {
		return store.AgentByIDRow{}, status.Errorf(codes.NotFound, "agent %s/%s not found", namespace, name)
	}
	if err != nil {
		return store.AgentByIDRow{}, status.Errorf(codes.Internal, "lookup agent: %v", err)
	}
	return agent, nil
}

func formatAgentRef(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}
