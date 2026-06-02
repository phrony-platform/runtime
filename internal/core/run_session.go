package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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

	sessionID, err := s.insertRunSession(ctx, q, agentVersionID, inputJSON)
	if err != nil {
		return nil, err
	}

	s.startRunSessionBackground(sessionID, agentVersionID, inputJSON)

	return &runtimev1.RunSessionResponse{
		SessionId:      sessionID,
		AgentVersionId: agentVersionID,
		Status:         model.SessionStatusRunning,
	}, nil
}

func (s *runtimeServer) insertRunSession(ctx context.Context, q *store.Queries, agentVersionID string, inputJSON json.RawMessage) (string, error) {
	sessionID := newRunSessionID()
	if _, err := q.InsertSession(ctx, store.InsertSessionParams{
		ID:             sessionID,
		AgentVersionID: agentVersionID,
		Input:          inputJSON,
		Status:         model.SessionStatusRunning,
	}); err != nil {
		return "", status.Errorf(codes.Internal, "persist session: %v", err)
	}
	return sessionID, nil
}

func resolveAgentVersionID(ctx context.Context, db store.DBTX, ref *runtimev1.AgentRef) (string, error) {
	ns, name, err := agentref.FromProto(ref)
	if err != nil {
		return "", err
	}

	q := store.New(db)
	agentRef := agentref.Format(ns, name)

	active, err := q.ActiveAgentVersion(ctx, ns, name)
	if errors.Is(err, sql.ErrNoRows) {
		agent, lookupErr := q.AgentByNamespaceName(ctx, ns, name)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			return "", status.Errorf(codes.NotFound, "agent %s not found", agentRef)
		}
		if lookupErr != nil {
			return "", status.Errorf(codes.Internal, "resolve agent: %v", lookupErr)
		}
		if agent.ArchivedAt.Valid {
			return "", status.Errorf(codes.FailedPrecondition, "agent %s is archived and cannot be run", agentRef)
		}
		return "", status.Errorf(codes.FailedPrecondition, "no active deployment for agent %s", agentRef)
	}
	if err != nil {
		return "", status.Errorf(codes.Internal, "resolve active deployment: %v", err)
	}

	versionLabel := active.Version
	if ref.GetVersion() != "" {
		versionLabel = ref.GetVersion()
		if active.Version != ref.GetVersion() {
			return "", status.Errorf(codes.FailedPrecondition,
				"version %q is not the active deployment (active: %q)", ref.GetVersion(), active.Version)
		}
	}

	return validateActiveVersionForRun(active, agentRef, versionLabel)
}

func validateActiveVersionForRun(active store.ActiveAgentVersionResult, agentRef, versionLabel string) (string, error) {
	if active.AgentArchived {
		return "", status.Errorf(codes.FailedPrecondition, "agent %s is archived and cannot be run", agentRef)
	}
	if active.Retired {
		return "", status.Errorf(codes.FailedPrecondition, "agent %s version %q is retired and cannot be run", agentRef, versionLabel)
	}
	if active.Deprecated {
		return "", status.Errorf(codes.FailedPrecondition, "agent %s version %q is deprecated and cannot be run", agentRef, versionLabel)
	}
	return active.AgentVersionID, nil
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
