package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const runSessionStatusPending = model.SessionStatusPending

func (s *runtimeServer) RunSession(ctx context.Context, req *runtimev1.RunSessionRequest) (*runtimev1.RunSessionResponse, error) {
	if s.db == nil {
		return nil, status.Error(codes.FailedPrecondition, "database is not configured")
	}

	inputJSON, err := normalizeSessionInput(req.GetInput())
	if err != nil {
		return nil, err
	}

	agentVersionID, err := resolveAgentVersionID(ctx, s.db.DB, req.GetAgentRef())
	if err != nil {
		return nil, err
	}

	sessionID := uuid.NewString()

	q := store.New(s.db.DB)
	if _, err := q.InsertSession(ctx, store.InsertSessionParams{
		ID:             sessionID,
		AgentVersionID: agentVersionID,
		Input:          inputJSON,
		Status:         runSessionStatusPending,
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "persist session: %v", err)
	}

	return &runtimev1.RunSessionResponse{
		SessionId:      sessionID,
		AgentVersionId: agentVersionID,
		Status:         runSessionStatusPending,
	}, nil
}

func resolveAgentVersionID(ctx context.Context, db store.DBTX, ref *runtimev1.AgentRef) (string, error) {
	if ref == nil || ref.GetNamespace() == "" || ref.GetName() == "" {
		return "", status.Error(codes.InvalidArgument, "agent_ref requires namespace and name")
	}

	q := store.New(db)
	ns, name := ref.GetNamespace(), ref.GetName()

	if ref.GetVersion() != "" {
		id, err := q.AgentVersionIDByLabel(ctx, ns, name, ref.GetVersion())
		if errors.Is(err, sql.ErrNoRows) {
			return "", status.Errorf(codes.NotFound, "no deployed version %q for agent %s/%s", ref.GetVersion(), ns, name)
		}
		if err != nil {
			return "", status.Errorf(codes.Internal, "resolve agent version: %v", err)
		}
		return id, nil
	}

	id, err := q.LatestAgentVersionID(ctx, ns, name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", status.Errorf(codes.NotFound, "no deployed version for agent %s/%s", ns, name)
	}
	if err != nil {
		return "", status.Errorf(codes.Internal, "resolve agent: %v", err)
	}
	return id, nil
}

func normalizeSessionInput(raw []byte) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage("{}"), nil
	}
	if !json.Valid(raw) {
		return nil, status.Error(codes.InvalidArgument, "input must be valid JSON")
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "input must be valid JSON: %v", err)
	}
	if _, ok := v.(map[string]any); !ok {
		return nil, status.Error(codes.InvalidArgument, "input must be a JSON object")
	}
	return json.RawMessage(raw), nil
}
