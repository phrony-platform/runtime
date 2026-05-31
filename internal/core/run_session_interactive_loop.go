package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *runtimeServer) runSessionInteractiveLoop(
	ctx context.Context,
	stream runtimev1.Runtime_RunSessionInteractiveServer,
	q *store.Queries,
	sessionID string,
	state *interactiveSessionState,
	pendingInput json.RawMessage,
	waitForUser bool,
) error {
	var lastStopReason string
	var lastOutput json.RawMessage
	var lastTurnUsage provider.TokenUsage

	loopCtx, loopCancel := context.WithCancel(ctx)
	defer loopCancel()

	recvCh := startInteractiveClientRecv(loopCtx, stream)
	wallC := state.wallClockTimerChan()

	for {
		if len(pendingInput) > 0 {
			if err := state.sessionLimitErrorBeforeTurn(); err != nil {
				state.blockInput(err)
				if err := state.publishInputBlocked(stream, lastStopReason, lastTurnUsage); err != nil {
					return err
				}
				pendingInput = nil
				if !waitForUser {
					if err := s.persistDetachedSessionOutcome(ctx, q, sessionID, state, lastOutput); err != nil {
						return err
					}
					return nil
				}
				continue
			}

			turnStart := time.Now()
			turnCancel, turnDone := runInteractiveTurnAsync(loopCtx, state, stream, pendingInput)

			var out interactiveTurnOutcome
			wallExpired := false
			if wallC != nil {
				select {
				case <-wallC:
					wallC = nil
					wallExpired = true
					turnCancel()
					out = <-turnDone
				case out = <-turnDone:
				}
			} else {
				out = <-turnDone
			}
			turnCancel()

			if wallExpired {
				if err := state.sessionWallClockLimitError(); err != nil {
					state.blockInput(err)
					if err := state.publishInputBlocked(stream, lastStopReason, lastTurnUsage); err != nil {
						return err
					}
					pendingInput = nil
					if !waitForUser {
						if err := s.persistDetachedSessionOutcome(ctx, q, sessionID, state, lastOutput); err != nil {
							return err
						}
						return nil
					}
					continue
				}
			}

			if out.err != nil {
				if handled, err := s.handleInteractiveTurnError(stream, state, lastStopReason, lastTurnUsage, out.err); err != nil {
					return err
				} else if handled {
					pendingInput = nil
					if !waitForUser {
						if err := s.persistDetachedSessionOutcome(ctx, q, sessionID, state, lastOutput); err != nil {
							return err
						}
						return nil
					}
					continue
				}
				return s.failInteractiveSession(ctx, q, stream, sessionID, out.err)
			}

			userText, err := userTextFromSessionInput(pendingInput)
			if err != nil {
				return s.failInteractiveSession(ctx, q, stream, sessionID, err)
			}
			turnDuration := time.Since(turnStart)
			state.history = appendTurnHistory(state.history, userText, out.assistantText, out.stopReason, out.turnUsage, turnDuration)
			state.turnCount++
			state.sessionUsage.Add(out.turnUsage)
			if err := state.sessionLimitErrorAfterTurn(); err != nil {
				state.blockInput(err)
			}

			outputJSON, err := marshalSessionOutput(out.assistantText, out.stopReason, out.turnUsage, state.sessionUsage, state.history)
			if err != nil {
				return status.Errorf(codes.Internal, "encode session output: %v", err)
			}

			historyJSON, err := encodeHistory(state.history)
			if err != nil {
				return status.Errorf(codes.Internal, "encode session history: %v", err)
			}
			if waitForUser {
				if _, err := q.UpdateSession(ctx, store.UpdateSessionParams{
					ID:      sessionID,
					Status:  model.SessionStatusAwaitingInput,
					Output:  outputJSON,
					History: historyJSON,
				}); err != nil {
					return status.Errorf(codes.Internal, "update session: %v", err)
				}
			} else if err := s.persistDetachedSessionAfterTurn(ctx, q, sessionID, state, outputJSON, historyJSON); err != nil {
				return err
			}
			lastStopReason = out.stopReason
			lastOutput = outputJSON
			lastTurnUsage = out.turnUsage

			if err := sendAwaitingInput(stream, out.stopReason, state.turnCount, out.turnUsage, state.sessionUsage, state.inputBlockedReason); err != nil {
				return err
			}
			pendingInput = nil
			if !waitForUser {
				return nil
			}
			continue
		}

		select {
		case <-loopCtx.Done():
			if errors.Is(loopCtx.Err(), context.Canceled) {
				wasCancelled, cerr := sessionWasCancelled(ctx, q, sessionID)
				if cerr != nil {
					return cerr
				}
				if wasCancelled {
					return nil
				}
			}
			return loopCtx.Err()
		case <-wallC:
			wallC = nil
			if err := state.notifyWallClockLimit(stream, lastStopReason, lastTurnUsage); err != nil {
				return err
			}
		case r, ok := <-recvCh:
			if !ok {
				recvCh = nil
				continue
			}
			if r.err != nil {
				if errors.Is(r.err, io.EOF) {
					if !waitForUser {
						return nil
					}
					if len(lastOutput) == 0 {
						return nil
					}
					return s.completeInteractiveSession(ctx, q, stream, sessionID, lastStopReason, lastOutput, state.turnCount, lastTurnUsage, state.sessionUsage)
				}
				return r.err
			}

			um := r.msg.GetUserMessage()
			if um == nil {
				return status.Error(codes.InvalidArgument, "expected user_message after awaiting_input")
			}
			if state.inputBlockedReason != "" {
				if err := state.publishInputBlocked(stream, lastStopReason, lastTurnUsage); err != nil {
					return err
				}
				continue
			}
			if err := state.sessionLimitErrorBeforeTurn(); err != nil {
				state.blockInput(err)
				if err := state.publishInputBlocked(stream, lastStopReason, lastTurnUsage); err != nil {
					return err
				}
				continue
			}
			text := strings.TrimSpace(um.GetText())
			if text == "" {
				return status.Error(codes.InvalidArgument, "user_message.text must be non-empty")
			}

			if _, err := q.UpdateSession(ctx, store.UpdateSessionParams{
				ID:     sessionID,
				Status: model.SessionStatusRunning,
			}); err != nil {
				return status.Errorf(codes.Internal, "update session: %v", err)
			}
			encoded, err := json.Marshal(map[string]string{"message": text})
			if err != nil {
				return status.Errorf(codes.Internal, "encode user message: %v", err)
			}
			pendingInput = encoded
		}
	}
}

func sendAwaitingInput(
	stream runtimev1.Runtime_RunSessionInteractiveServer,
	stopReason string,
	turn int,
	turnUsage, sessionUsage provider.TokenUsage,
	inputBlockedReason string,
) error {
	return stream.Send(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_AwaitingInput{
			AwaitingInput: &runtimev1.RunSessionInteractiveAwaitingInput{
				StopReason:         stopReason,
				Stats:              interactiveSessionStats(turn, turnUsage, sessionUsage),
				InputBlockedReason: inputBlockedReason,
			},
		},
	})
}

func sendSessionStarted(
	stream runtimev1.Runtime_RunSessionInteractiveServer,
	sessionID, agentVersionID string,
	ver *executor.Version,
	history []provider.Message,
	sessionStartedAt time.Time,
	sessionEndedAt *time.Time,
) error {
	modelProvider := ""
	modelName := ""
	var maxTokensPerRun int32
	var maxWallClockSeconds int32
	if ver.Agent != nil {
		modelProvider = ver.Agent.Spec.Model.Provider
		modelName = ver.Agent.Spec.Model.Name
		if lim := ver.Agent.Spec.Limits; lim != nil {
			if lim.MaxTokensPerRun != nil && *lim.MaxTokensPerRun > 0 {
				maxTokensPerRun = int32(*lim.MaxTokensPerRun)
			}
			if lim.MaxWallClockSeconds != nil && *lim.MaxWallClockSeconds > 0 {
				maxWallClockSeconds = int32(*lim.MaxWallClockSeconds)
			}
		}
	}
	if sessionStartedAt.IsZero() {
		sessionStartedAt = time.Now()
	}
	var sessionEndedAtUnixMs int64
	if sessionEndedAt != nil && !sessionEndedAt.IsZero() {
		sessionEndedAtUnixMs = sessionEndedAt.UnixMilli()
	}
	return stream.Send(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_SessionStarted{
			SessionStarted: &runtimev1.RunSessionInteractiveSessionStarted{
				SessionId:              sessionID,
				AgentVersionId:         agentVersionID,
				ModelProvider:          modelProvider,
				ModelName:              modelName,
				History:                historyToProto(history),
				MaxTokensPerRun:        maxTokensPerRun,
				MaxWallClockSeconds:    maxWallClockSeconds,
				SessionStartedAtUnixMs: sessionStartedAt.UnixMilli(),
				SessionEndedAtUnixMs:   sessionEndedAtUnixMs,
			},
		},
	})
}

func rejectInteractiveUserMessage(stream runtimev1.Runtime_RunSessionInteractiveServer) error {
	msg, err := stream.Recv()
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	if msg.GetUserMessage() != nil {
		return status.Error(codes.InvalidArgument, "user_message is not allowed for this session")
	}
	return status.Error(codes.InvalidArgument, "expected no further client messages")
}

func stopReasonFromSessionOutput(output json.RawMessage) string {
	if len(output) == 0 {
		return ""
	}
	var obj struct {
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(output, &obj); err != nil {
		return ""
	}
	return obj.StopReason
}
