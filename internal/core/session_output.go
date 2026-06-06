// Session output and history JSON shapes persisted in the store.
// Domain usage values use provider.TokenUsage; wire types use runtimev1 (see convert_proto.go).
package core

import (
	"context"
	"encoding/json"

	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
)

type sessionOutputUsage struct {
	InputTokens  int  `json:"input_tokens"`
	OutputTokens int  `json:"output_tokens"`
	Estimated    bool `json:"estimated,omitempty"`
}

type sessionTurnRecord struct {
	StopReason      string              `json:"stop_reason,omitempty"`
	TurnUsage       *sessionOutputUsage `json:"turn_usage,omitempty"`
	TurnDurationMs  int64               `json:"turn_duration_ms,omitempty"`
}

type sessionOutput struct {
	Message      string              `json:"message"`
	StopReason   string              `json:"stop_reason"`
	TurnUsage    *sessionOutputUsage `json:"turn_usage,omitempty"`
	SessionUsage *sessionOutputUsage `json:"session_usage,omitempty"`
	// Turns stores per-completed-turn stats for re-attach when history rows lack turn_usage.
	Turns []sessionTurnRecord `json:"turns,omitempty"`
}

func usageToSessionOutput(u provider.TokenUsage) *sessionOutputUsage {
	if u.IsZero() {
		return nil
	}
	return &sessionOutputUsage{
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		Estimated:    u.Estimated,
	}
}

func usageFromSessionOutput(u *sessionOutputUsage) provider.TokenUsage {
	if u == nil {
		return provider.TokenUsage{}
	}
	return provider.TokenUsage{
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		Estimated:    u.Estimated,
	}
}

func turnRecordsFromHistory(messages []provider.Message) []sessionTurnRecord {
	var turns []sessionTurnRecord
	for _, m := range messages {
		if m.Role != provider.RoleAssistant {
			continue
		}
		turns = append(turns, sessionTurnRecord{
			StopReason:     m.StopReason,
			TurnUsage:      usageToSessionOutput(m.TurnUsage),
			TurnDurationMs: m.TurnDurationMs,
		})
	}
	return turns
}

func marshalSessionOutput(message, stopReason string, turnUsage, sessionUsage provider.TokenUsage, history []provider.Message) (json.RawMessage, error) {
	out := sessionOutput{
		Message:      message,
		StopReason:   stopReason,
		TurnUsage:    usageToSessionOutput(turnUsage),
		SessionUsage: usageToSessionOutput(sessionUsage),
		Turns:        turnRecordsFromHistory(history),
	}
	return json.Marshal(out)
}

// buildSessionOutput folds assistant message events into the session output JSON shape.
func buildSessionOutput(events []store.Event) json.RawMessage {
	var lastAssistant string
	var lastStopReason string
	var lastTurnUsage provider.TokenUsage
	var sessionUsage provider.TokenUsage
	var turns []sessionTurnRecord
	for _, ev := range events {
		if ev.Type != EventMessageAssistant {
			continue
		}
		msg, err := conversationMessageFromSessionEvent(ev.Payload)
		if err != nil {
			continue
		}
		turnUsage := tokenUsageFromProto(msg.GetTurnUsage())
		lastAssistant = msg.GetContent()
		lastStopReason = msg.GetStopReason()
		lastTurnUsage = turnUsage
		sessionUsage.Add(turnUsage)
		turns = append(turns, sessionTurnRecord{
			StopReason:     msg.GetStopReason(),
			TurnUsage:      usageToSessionOutput(turnUsage),
			TurnDurationMs: msg.GetTurnDurationMs(),
		})
	}
	out := sessionOutput{
		Message:      lastAssistant,
		StopReason:   lastStopReason,
		TurnUsage:    usageToSessionOutput(lastTurnUsage),
		SessionUsage: usageToSessionOutput(sessionUsage),
		Turns:        turns,
	}
	b, err := json.Marshal(out)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}

// loadSessionOutputJSON returns folded session output for a session.
func loadSessionOutputJSON(ctx context.Context, q *store.Queries, sessionID string) (json.RawMessage, error) {
	events, err := q.ListEventsBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return buildSessionOutput(events), nil
}

func loadFoldedSession(ctx context.Context, q *store.Queries, sessionID string) ([]provider.Message, json.RawMessage, error) {
	events, err := q.ListEventsBySession(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	return buildProviderContext(events), buildSessionOutput(events), nil
}

// syncInteractiveStateFromFold reloads in-memory conversation state from the event log.
func syncInteractiveStateFromFold(ctx context.Context, q *store.Queries, sessionID string, st *interactiveSessionState) error {
	history, output, err := loadFoldedSession(ctx, q, sessionID)
	if err != nil {
		return err
	}
	st.history = history
	st.turnCount = countCompletedTurns(history)
	_, st.sessionUsage = usageFromSessionOutputJSON(output)
	return nil
}

func usageFromSessionOutputJSON(output json.RawMessage) (turnUsage, sessionUsage provider.TokenUsage) {
	if len(output) == 0 {
		return provider.TokenUsage{}, provider.TokenUsage{}
	}
	var obj sessionOutput
	if err := json.Unmarshal(output, &obj); err != nil {
		return provider.TokenUsage{}, provider.TokenUsage{}
	}
	return usageFromSessionOutput(obj.TurnUsage), usageFromSessionOutput(obj.SessionUsage)
}
