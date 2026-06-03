package core

import (
	"encoding/json"
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

// patchHistoryLastAssistantFromOutput uses session output.message when it is longer
// than the last assistant row (covers re-attach before history commit visibility).
func patchHistoryLastAssistantFromOutput(history []provider.Message, output json.RawMessage) []provider.Message {
	if len(output) == 0 || len(history) == 0 {
		return history
	}
	var obj sessionOutput
	if err := json.Unmarshal(output, &obj); err != nil || obj.Message == "" {
		return history
	}
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != provider.RoleAssistant {
			continue
		}
		if len(obj.Message) > len(history[i].Content) {
			history[i].Content = obj.Message
			if obj.StopReason != "" {
				history[i].StopReason = obj.StopReason
			}
			if obj.TurnUsage != nil {
				history[i].TurnUsage = usageFromSessionOutput(obj.TurnUsage)
			}
		}
		break
	}
	return history
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
