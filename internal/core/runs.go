package core

import (
	"context"
	"database/sql"
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

	s.cancelActiveSession(sessionID)
	if s.toolRegistry != nil {
		s.toolRegistry.CancelSession(sessionID)
	}

	return &runtimev1.CancelSessionResponse{}, nil
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

func sessionWasCancelled(ctx context.Context, q *store.Queries, sessionID string) (bool, error) {
	session, err := q.GetSession(ctx, sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, status.Errorf(codes.NotFound, "session %s not found", sessionID)
	}
	if err != nil {
		return false, status.Errorf(codes.Internal, "load session: %v", err)
	}
	return session.Status == model.SessionStatusCancelled, nil
}
