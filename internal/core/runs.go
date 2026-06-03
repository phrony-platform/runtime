package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *runtimeServer) CancelSession(ctx context.Context, req *runtimev1.CancelSessionRequest) (*runtimev1.CancelSessionResponse, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
	}

	sessionID := req.GetSessionId()
	if sessionID == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}

	if _, err := q.CancelSession(ctx, sessionID); errors.Is(err, sql.ErrNoRows) {
		return nil, status.Errorf(codes.NotFound, "session %s not found or already terminal", sessionID)
	} else if err != nil {
		return nil, status.Errorf(codes.Internal, "cancel session: %v", err)
	}

	newSessionEventRecorder(q).Record(ctx, sessionID, model.SessionEventSessionCancelled, json.RawMessage("{}"))
	s.finalizeSessionSecrets(ctx, q, sessionID) // after audit event

	s.cancelActiveSession(sessionID)
	if s.toolRegistry != nil {
		s.toolRegistry.CancelSession(sessionID)
	}

	return &runtimev1.CancelSessionResponse{}, nil
}

func (s *runtimeServer) CompleteSession(ctx context.Context, req *runtimev1.CompleteSessionRequest) (*runtimev1.CompleteSessionResponse, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
	}

	sessionID := req.GetSessionId()
	if sessionID == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}

	if _, err := q.CompleteSession(ctx, sessionID); errors.Is(err, sql.ErrNoRows) {
		return nil, status.Errorf(codes.NotFound, "session %s not found or already terminal", sessionID)
	} else if err != nil {
		return nil, status.Errorf(codes.Internal, "complete session: %v", err)
	}

	newSessionEventRecorder(q).Record(ctx, sessionID, model.SessionEventSessionCompleted, json.RawMessage("{}"))
	s.finalizeSessionSecrets(ctx, q, sessionID) // after audit event

	// Stop any active driver/attach loop. The loop detects the completed status
	// on context cancellation and emits the terminal Completed event to clients.
	s.cancelActiveSession(sessionID)
	if s.toolRegistry != nil {
		s.toolRegistry.CancelSession(sessionID)
	}

	return &runtimev1.CompleteSessionResponse{}, nil
}

func (s *runtimeServer) cancelActiveSession(sessionID string) {
	if s.activeSessions == nil {
		return
	}
	if v, ok := s.activeSessions.LoadAndDelete(sessionID); ok {
		if entry, ok := v.(activeSessionEntry); ok {
			if entry.inputMux != nil {
				entry.inputMux.close()
			}
			entry.cancel()
			s.approvalCoord().unregisterGate(sessionID)
		}
	}
}

// sessionLookupCtx returns a context for reading committed session rows during
// driver shutdown. The driver context is cancelled by CompleteSession/CancelSession
// before the loop emits terminal stream events.
func sessionLookupCtx(ctx context.Context) context.Context {
	if ctx.Err() != nil {
		return context.Background()
	}
	return ctx
}

func sessionWasCancelled(ctx context.Context, q *store.Queries, sessionID string) (bool, error) {
	session, err := q.GetSession(sessionLookupCtx(ctx), sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, status.Errorf(codes.NotFound, "session %s not found", sessionID)
	}
	if err != nil {
		return false, status.Errorf(codes.Internal, "load session: %v", err)
	}
	return session.Status == model.SessionStatusCancelled, nil
}

func sessionWasCompleted(ctx context.Context, q *store.Queries, sessionID string) (bool, error) {
	session, err := q.GetSession(sessionLookupCtx(ctx), sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, status.Errorf(codes.NotFound, "session %s not found", sessionID)
	}
	if err != nil {
		return false, status.Errorf(codes.Internal, "load session: %v", err)
	}
	return session.Status == model.SessionStatusCompleted, nil
}
