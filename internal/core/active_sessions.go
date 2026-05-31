package core

import (
	"context"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type activeSessionEntry struct {
	cancel context.CancelFunc
}

func (s *runtimeServer) registerActiveSession(sessionID string, cancel context.CancelFunc) error {
	if s.activeSessions == nil {
		s.activeSessions = &sync.Map{}
	}
	_, loaded := s.activeSessions.LoadOrStore(sessionID, activeSessionEntry{cancel: cancel})
	if loaded {
		return status.Error(codes.FailedPrecondition, "session already active")
	}
	return nil
}

func (s *runtimeServer) unregisterActiveSession(sessionID string) {
	if s.activeSessions == nil {
		return
	}
	s.activeSessions.Delete(sessionID)
}

func (s *runtimeServer) sessionIsActive(sessionID string) bool {
	if s.activeSessions == nil {
		return false
	}
	_, ok := s.activeSessions.Load(sessionID)
	return ok
}
