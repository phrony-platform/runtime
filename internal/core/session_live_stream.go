package core

import (
	"strings"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/provider"
)

func (s *runtimeServer) setActiveSessionLiveAssistant(sessionID, text string) {
	if s.activeSessions == nil {
		return
	}
	v, ok := s.activeSessions.Load(sessionID)
	if !ok {
		return
	}
	entry, _ := v.(activeSessionEntry)
	entry.setLiveAssistant(text)
	s.activeSessions.Store(sessionID, entry)
}

func (s *runtimeServer) clearActiveSessionLiveAssistant(sessionID string) {
	s.setActiveSessionLiveAssistant(sessionID, "")
}

func (e *activeSessionEntry) setLiveAssistant(text string) {
	e.streamMu.Lock()
	e.liveAssistant = text
	e.streamMu.Unlock()
}

func (e *activeSessionEntry) liveAssistantText() string {
	e.streamMu.RLock()
	defer e.streamMu.RUnlock()
	return e.liveAssistant
}

func lastAssistantHistoryContent(history []provider.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == provider.RoleAssistant {
			return history[i].Content
		}
	}
	return ""
}

func liveAssistantReplayDelta(history []provider.Message, live string) string {
	live = strings.TrimPrefix(live, "\uFEFF")
	if live == "" {
		return ""
	}
	last := lastAssistantHistoryContent(history)
	if last == "" {
		return live
	}
	if strings.HasPrefix(live, last) {
		return live[len(last):]
	}
	if strings.HasPrefix(last, live) {
		return ""
	}
	return live
}

func sendLiveAssistantReplay(events sessionEventSink, history []provider.Message, live string) error {
	delta := liveAssistantReplayDelta(history, live)
	if delta == "" {
		return nil
	}
	return events.Send(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_TextDelta{
			TextDelta: &runtimev1.RunSessionInteractiveTextDelta{Delta: delta},
		},
	})
}
