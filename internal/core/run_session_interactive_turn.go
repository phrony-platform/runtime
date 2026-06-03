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
	liveTextSink       func(cumulative string)
	clientRecvEOF      bool
	wallClockPaused    bool
	pauseStartedAt     time.Time
	totalWallPaused    time.Duration
	hitlWaitAccum      time.Duration
}

func newInteractiveSessionState(
	ctx context.Context,
	s *runtimeServer,
	sessionID, agentVersionID string,
	ver *executor.Version,
	startedAt time.Time,
	events sessionEventSink,
	q *store.Queries,
) (*interactiveSessionState, error) {
	dispatch, err := s.sessionToolDispatch(ctx, q, sessionID, ver)
	if err != nil {
		return nil, err
	}
	gate := newSessionApprovalGate(s.approvalCoord(), sessionID, events, q, agentVersionID)
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
	return st, nil
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
	events sessionEventSink,
	input json.RawMessage,
) (stopReason, assistantText string, turnUsage provider.TokenUsage, err error) {
	runCtx, cancel := st.runContext(ctx)
	defer cancel()

	recorder := newSessionEventRecorder(q)
	turnStart := time.Now()
	if userText, terr := userTextFromSessionInput(input); terr == nil && userText != "" {
		recorder.Record(ctx, st.sessionID, model.SessionEventUserMessage, userMessagePayload(userText))
	}

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
	// flushedLen tracks how much of the cumulative assistant text has already been
	// persisted as assistant_message segments, so tool-call boundaries split text
	// in order without disturbing the live cumulative builder.
	flushedLen := 0
	flushAssistantSegment := func(stopReason string, usage provider.TokenUsage, durationMs int64) {
		full := builder.String()
		if flushedLen > len(full) {
			flushedLen = len(full)
		}
		segment := full[flushedLen:]
		flushedLen = len(full)
		if strings.TrimSpace(segment) == "" {
			return
		}
		recorder.Record(ctx, st.sessionID, model.SessionEventAssistantMessage, assistantMessagePayload(segment, stopReason, usage, durationMs))
	}
	for ev := range ch {
		switch ev.Type {
		case executor.EventTextDelta:
			builder.WriteString(ev.TextDelta)
			if st.liveTextSink != nil {
				st.liveTextSink(builder.String())
			}
			if err := events.Send(&runtimev1.RunSessionInteractiveServerMsg{
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
			flushAssistantSegment("", provider.TokenUsage{}, 0)
			if err := sendToolCall(events, ev.ToolCall); err != nil {
				return "", "", provider.TokenUsage{}, err
			}
			recorder.RecordServerMsg(ctx, st.sessionID, model.SessionEventToolCall, toolCallServerMsg(ev.ToolCall))
		case executor.EventToolResult:
			if q != nil {
				_, _ = q.UpdateSession(ctx, store.UpdateSessionParams{
					ID:     st.sessionID,
					Status: model.SessionStatusRunning,
				})
			}
			if err := sendToolResult(events, ev.ToolResult); err != nil {
				return "", "", provider.TokenUsage{}, err
			}
			resultType := model.SessionEventToolResult
			if ev.ToolResult.Denied {
				resultType = model.SessionEventPolicyDenied
			}
			recorder.RecordServerMsg(ctx, st.sessionID, resultType, toolResultServerMsg(ev.ToolResult))
		case executor.EventApprovalRequired:
			if q != nil {
				_, _ = q.UpdateSession(ctx, store.UpdateSessionParams{
					ID:     st.sessionID,
					Status: model.SessionStatusAwaitingApproval,
				})
			}
			recorder.RecordServerMsg(ctx, st.sessionID, model.SessionEventApprovalRequired, approvalRequiredServerMsg(ev.Approval))
		case executor.EventCompleted:
			if err := <-runErrCh; err != nil {
				return "", "", provider.TokenUsage{}, err
			}
			flushAssistantSegment(ev.StopReason, ev.Usage, time.Since(turnStart).Milliseconds())
			if st.liveTextSink != nil {
				st.liveTextSink("")
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
	events sessionEventSink,
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
	newSessionEventRecorder(q).Record(ctx, sessionID, model.SessionEventSessionFailed, marshalSessionEventJSON(map[string]string{"message": msg}))
	s.finalizeSessionSecrets(ctx, q, sessionID)
	_ = events.Send(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_Failed{
			Failed: &runtimev1.RunSessionInteractiveFailed{Message: msg},
		},
	})
	return nil
}

func (s *runtimeServer) completeInteractiveSession(
	ctx context.Context,
	q *store.Queries,
	events sessionEventSink,
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
	newSessionEventRecorder(q).Record(ctx, sessionID, model.SessionEventSessionCompleted, marshalSessionEventJSON(map[string]string{"stop_reason": stopReason}))
	s.finalizeSessionSecrets(ctx, q, sessionID)
	return events.Send(&runtimev1.RunSessionInteractiveServerMsg{
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

func (s *runtimeServer) loadSessionVersion(ctx context.Context, q *store.Queries, sessionID, agentVersionID string) (*executor.Version, error) {
	if s.loadSessionVersionFn != nil {
		return s.loadSessionVersionFn(ctx, q, sessionID, agentVersionID)
	}
	ex := &executor.Executor{Enc: s.secretsEnc, Q: q}
	return ex.LoadVersionForSession(ctx, sessionID, agentVersionID)
}
