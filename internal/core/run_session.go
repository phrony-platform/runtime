package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const runSessionStatusPending = model.SessionStatusPending

func (s *runtimeServer) RunSession(ctx context.Context, req *runtimev1.RunSessionRequest) (*runtimev1.RunSessionResponse, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
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
	ns, name, err := agentref.FromProto(ref)
	if err != nil {
		return "", err
	}

	q := store.New(db)
	agentRef := agentref.Format(ns, name)

	if ref.GetVersion() != "" {
		lookup, err := q.AgentVersionIDByLabel(ctx, ns, name, ref.GetVersion())
		if errors.Is(err, sql.ErrNoRows) {
			return "", status.Errorf(codes.NotFound, "no deployed version %q for agent %s", ref.GetVersion(), agentRef)
		}
		if err != nil {
			return "", status.Errorf(codes.Internal, "resolve agent version: %v", err)
		}
		if lookup.AgentArchive {
			return "", status.Errorf(codes.FailedPrecondition, "agent %s is archived and cannot be run", agentRef)
		}
		if lookup.Deprecated {
			return "", status.Errorf(codes.FailedPrecondition, "agent %s version %q is deprecated and cannot be run", agentRef, ref.GetVersion())
		}
		return lookup.ID, nil
	}

	id, err := q.LatestAgentVersionID(ctx, ns, name)
	if errors.Is(err, sql.ErrNoRows) {
		agent, lookupErr := q.AgentByNamespaceName(ctx, ns, name)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			return "", status.Errorf(codes.NotFound, "no deployed version for agent %s", agentRef)
		}
		if lookupErr != nil {
			return "", status.Errorf(codes.Internal, "resolve agent: %v", lookupErr)
		}
		if agent.ArchivedAt.Valid {
			return "", status.Errorf(codes.FailedPrecondition, "agent %s is archived and cannot be run", agentRef)
		}
		return "", status.Errorf(codes.NotFound, "no runnable version for agent %s", agentRef)
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
