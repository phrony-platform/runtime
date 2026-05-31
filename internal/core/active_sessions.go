package core

import (
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *runtimeServer) registerActiveSession(sessionID string) error {
	if s.activeSessions == nil {
		s.activeSessions = &sync.Map{}
	}
	_, loaded := s.activeSessions.LoadOrStore(sessionID, struct{}{})
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
