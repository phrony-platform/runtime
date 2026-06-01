package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/policy"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type interactiveSessionState struct {
	sessionID          string
	agentVersionID     string
	turnCount          int
	sessionUsage       provider.TokenUsage
	history            []provider.Message
	version            *executor.Version
	sessionStartedAt   time.Time
	inputBlockedReason string
	toolDispatch       tooldispatch.Dispatcher
	policies           *policy.Evaluator
	approvalGate       *sessionApprovalGate
	clientRecvEOF      bool
	wallClockPaused    bool
	pauseStartedAt     time.Time
	totalWallPaused    time.Duration
	hitlWaitAccum      time.Duration
}

func newInteractiveSessionState(
	sessionID, agentVersionID string,
	ver *executor.Version,
	startedAt time.Time,
	dispatch tooldispatch.Dispatcher,
	stream runtimev1.Runtime_RunSessionInteractiveServer,
	q *store.Queries,
) *interactiveSessionState {
	gate := newSessionApprovalGate(stream, q, agentVersionID)
	st := &interactiveSessionState{
		sessionID:        sessionID,
		agentVersionID:   agentVersionID,
		version:          ver,
		sessionStartedAt: startedAt,
		toolDispatch:     dispatch,
		policies:         policy.NewEvaluator(ver.Agent),
		approvalGate:     gate,
	}
	gate.hitl = st
	return st
}

func (st *interactiveSessionState) maxTokensPerRun() int {
	if st.version == nil || st.version.Agent == nil {
		return 0
	}
	lim := st.version.Agent.Spec.Limits
	if lim == nil || lim.MaxTokensPerRun == nil {
		return 0
	}
	return *lim.MaxTokensPerRun
}

func (st *interactiveSessionState) runTurn(
	ctx context.Context,
	q *store.Queries,
	stream runtimev1.Runtime_RunSessionInteractiveServer,
	input json.RawMessage,
) (stopReason, assistantText string, turnUsage provider.TokenUsage, err error) {
	runCtx, cancel := st.runContext(ctx)
	defer cancel()

	ch := make(chan executor.Event, 32)
	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- st.version.StreamCompletion(runCtx, executor.RunParams{
			SessionID:     st.sessionID,
			Turn:          st.turnCount + 1,
			Input:         input,
			History:       st.history,
			Dispatcher:    st.toolDispatch,
			Policies:      st.policies,
			ApprovalGate:  st.approvalGate,
			NewApprovalID: newApprovalID,
			BeforeToolDispatch: func(ctx context.Context, messages []provider.Message) error {
				return st.persistBeforeToolDispatch(ctx, q, messages)
			},
		}, ch)
	}()

	var builder strings.Builder
	for ev := range ch {
		switch ev.Type {
		case executor.EventTextDelta:
			builder.WriteString(ev.TextDelta)
			if err := stream.Send(&runtimev1.RunSessionInteractiveServerMsg{
				Body: &runtimev1.RunSessionInteractiveServerMsg_TextDelta{
					TextDelta: &runtimev1.RunSessionInteractiveTextDelta{Delta: ev.TextDelta},
				},
			}); err != nil {
				return "", "", provider.TokenUsage{}, err
			}
		case executor.EventToolCall:
			if q != nil {
				_, _ = q.UpdateSession(ctx, store.UpdateSessionParams{
					ID:     st.sessionID,
					Status: model.SessionStatusAwaitingTool,
				})
			}
			if err := sendToolCall(stream, ev.ToolCall); err != nil {
				return "", "", provider.TokenUsage{}, err
			}
		case executor.EventToolResult:
			if q != nil {
				_, _ = q.UpdateSession(ctx, store.UpdateSessionParams{
					ID:     st.sessionID,
					Status: model.SessionStatusRunning,
				})
			}
			if err := sendToolResult(stream, ev.ToolResult); err != nil {
				return "", "", provider.TokenUsage{}, err
			}
		case executor.EventApprovalRequired:
			if q != nil {
				_, _ = q.UpdateSession(ctx, store.UpdateSessionParams{
					ID:     st.sessionID,
					Status: model.SessionStatusAwaitingApproval,
				})
			}
		case executor.EventCompleted:
			if err := <-runErrCh; err != nil {
				return "", "", provider.TokenUsage{}, err
			}
			return ev.StopReason, builder.String(), ev.Usage, nil
		case executor.EventFailed:
			if ev.Err != nil {
				return "", "", provider.TokenUsage{}, ev.Err
			}
			return "", "", provider.TokenUsage{}, fmt.Errorf("model completion failed")
		case executor.EventEscalation:
			if err := <-runErrCh; err != nil {
				return "", "", provider.TokenUsage{}, err
			}
			return "", "", provider.TokenUsage{}, ev.Err
		}
	}

	if err := <-runErrCh; err != nil {
		return "", "", provider.TokenUsage{}, err
	}
	return "", "", provider.TokenUsage{}, fmt.Errorf("model completion ended without a terminal event")
}

func (s *runtimeServer) failInteractiveSession(
	ctx context.Context,
	q *store.Queries,
	stream runtimev1.Runtime_RunSessionInteractiveServer,
	sessionID string,
	runErr error,
) error {
	msg := runErr.Error()
	errText := msg
	if _, err := q.UpdateSession(ctx, store.UpdateSessionParams{
		ID:     sessionID,
		Status: model.SessionStatusFailed,
		Error:  &errText,
	}); err != nil {
		return status.Errorf(codes.Internal, "update session: %v", err)
	}
	_ = stream.Send(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_Failed{
			Failed: &runtimev1.RunSessionInteractiveFailed{Message: msg},
		},
	})
	return nil
}

func (s *runtimeServer) completeInteractiveSession(
	ctx context.Context,
	q *store.Queries,
	stream runtimev1.Runtime_RunSessionInteractiveServer,
	sessionID, stopReason string,
	output json.RawMessage,
	turn int,
	turnUsage, sessionUsage provider.TokenUsage,
) error {
	endedAt := time.Now()
	if _, err := q.UpdateSession(ctx, store.UpdateSessionParams{
		ID:     sessionID,
		Status: model.SessionStatusCompleted,
		Output: output,
	}); err != nil {
		return status.Errorf(codes.Internal, "update session: %v", err)
	}
	return stream.Send(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_Completed{
			Completed: &runtimev1.RunSessionInteractiveCompleted{
				StopReason:           stopReason,
				Output:               output,
				Stats:                interactiveSessionStats(turn, turnUsage, sessionUsage),
				SessionEndedAtUnixMs: endedAt.UnixMilli(),
			},
		},
	})
}

func appendTurnHistory(history []provider.Message, userText, assistantText, stopReason string, turnUsage provider.TokenUsage, turnDuration time.Duration) []provider.Message {
	if userText != "" {
		history = append(history, provider.Message{Role: provider.RoleUser, Content: userText})
	}
	if assistantText != "" {
		history = append(history, provider.Message{
			Role:           provider.RoleAssistant,
			Content:        assistantText,
			StopReason:     stopReason,
			TurnUsage:      turnUsage,
			TurnDurationMs: turnDuration.Milliseconds(),
		})
	}
	return history
}

func userTextFromSessionInput(input json.RawMessage) (string, error) {
	if len(input) == 0 {
		return "", nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(input, &obj); err != nil {
		return "", fmt.Errorf("session input must be a JSON object: %w", err)
	}
	if raw, ok := obj["message"]; ok {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", fmt.Errorf("session input.message must be a string")
		}
		return strings.TrimSpace(s), nil
	}
	if raw, ok := obj["text"]; ok {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", fmt.Errorf("session input.text must be a string")
		}
		return strings.TrimSpace(s), nil
	}
	return strings.TrimSpace(string(input)), nil
}

func (s *runtimeServer) loadSessionVersion(ctx context.Context, q *store.Queries, agentVersionID string) (*executor.Version, error) {
	if s.loadSessionVersionFn != nil {
		return s.loadSessionVersionFn(ctx, q, agentVersionID)
	}
	ex := &executor.Executor{Enc: s.secretsEnc, Q: q}
	return ex.LoadVersion(ctx, agentVersionID)
}
