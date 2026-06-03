package core

import (
	"context"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// activeSessionEntry tracks a background session driver: one loop owns execution
// until the session is terminal or explicitly cancelled.
type activeSessionEntry struct {
	cancel       context.CancelFunc
	eventHub     *sessionEventHub
	inputMux     *sessionInputMux
	approvalGate *sessionApprovalGate
	streamMu     sync.RWMutex
	// liveAssistant holds in-progress assistant text for the current turn (driver-owned).
	liveAssistant string
}

func (s *runtimeServer) activeSessionEntryFor(sessionID string) (activeSessionEntry, bool) {
	if s.activeSessions == nil {
		return activeSessionEntry{}, false
	}
	v, ok := s.activeSessions.Load(sessionID)
	if !ok {
		return activeSessionEntry{}, false
	}
	entry, ok := v.(activeSessionEntry)
	return entry, ok
}

func (s *runtimeServer) activeSessionEventHub(sessionID string) *sessionEventHub {
	entry, ok := s.activeSessionEntryFor(sessionID)
	if !ok {
		return nil
	}
	return entry.eventHub
}

func (s *runtimeServer) activeSessionInputMux(sessionID string) *sessionInputMux {
	entry, ok := s.activeSessionEntryFor(sessionID)
	if !ok {
		return nil
	}
	return entry.inputMux
}

func (s *runtimeServer) approvalCoord() *approvalCoordinator {
	if s.approvalCoordinator == nil {
		s.approvalCoordinator = newApprovalCoordinator(s)
	}
	return s.approvalCoordinator
}

func (s *runtimeServer) registerActiveSession(sessionID string, entry activeSessionEntry) error {
	if s.activeSessions == nil {
		s.activeSessions = &sync.Map{}
	}
	_, loaded := s.activeSessions.LoadOrStore(sessionID, entry)
	if loaded {
		return status.Error(codes.FailedPrecondition, "session already active")
	}
	if entry.approvalGate != nil {
		s.approvalCoord().registerGate(sessionID, entry.approvalGate)
	}
	return nil
}

func (s *runtimeServer) unregisterActiveSession(sessionID string) {
	if s.activeSessions == nil {
		return
	}
	s.activeSessions.Delete(sessionID)
	s.approvalCoord().unregisterGate(sessionID)
}

func (s *runtimeServer) sessionIsActive(sessionID string) bool {
	if s.activeSessions == nil {
		return false
	}
	_, ok := s.activeSessions.Load(sessionID)
	return ok
}

func (s *runtimeServer) withActiveSession(sessionID string, entry activeSessionEntry, fn func() error) error {
	if err := s.registerActiveSession(sessionID, entry); err != nil {
		return err
	}
	defer s.unregisterActiveSession(sessionID)
	return fn()
}

func (s *runtimeServer) attachActiveSessionGate(sessionID string, gate *sessionApprovalGate) {
	if gate == nil {
		return
	}
	if s.activeSessions != nil {
		if v, ok := s.activeSessions.Load(sessionID); ok {
			entry, _ := v.(activeSessionEntry)
			entry.approvalGate = gate
			s.activeSessions.Store(sessionID, entry)
		}
	}
	s.approvalCoord().registerGate(sessionID, gate)
}

func (s *runtimeServer) activeSessionGate(sessionID string) *sessionApprovalGate {
	if s.activeSessions == nil {
		return nil
	}
	v, ok := s.activeSessions.Load(sessionID)
	if !ok {
		return nil
	}
	entry, ok := v.(activeSessionEntry)
	if !ok {
		return nil
	}
	return entry.approvalGate
}
