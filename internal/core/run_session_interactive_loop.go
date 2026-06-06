package core

import (
	"context"
	"database/sql"
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
	events sessionEventSink,
	q *store.Queries,
	sessionID string,
	state *interactiveSessionState,
	pendingInput json.RawMessage,
	waitForUser bool,
) error {
	var lastStopReason string
	var lastOutput json.RawMessage
	var lastTurnUsage provider.TokenUsage

	defer closeSessionDispatch(state.toolDispatch)

	loopCtx, loopCancel := context.WithCancel(ctx)
	defer loopCancel()

	recvCh := startInteractiveClientRecv(loopCtx, stream)
	wallC := state.wallClockTimerChan()
	var queuedUserInput json.RawMessage

	for {
		if len(pendingInput) > 0 {
			if err := state.sessionLimitErrorBeforeTurn(); err != nil {
				if isWallClockLimitError(err) {
					if err := s.publishWallClockBlockedAndPersist(ctx, q, events, sessionID, state, lastStopReason, lastTurnUsage, lastOutput); err != nil {
						return err
					}
				} else {
					state.blockInput(err)
					if err := state.publishInputBlocked(events, lastStopReason, lastTurnUsage); err != nil {
						return err
					}
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

			s.clearActiveSessionLiveAssistant(sessionID)
			turnStart := time.Now()
			turnCancel, turnDone := runInteractiveTurnAsync(loopCtx, q, state, events, pendingInput)

			var out interactiveTurnOutcome
			wallExpired := false
		turnWait:
			for {
				if wallC != nil {
					select {
					case <-wallC:
						wallC = nil
						wallExpired = true
						turnCancel()
						out = <-turnDone
						break turnWait
					case out = <-turnDone:
						break turnWait
					case r, ok := <-recvCh:
						if !ok {
							recvCh = nil
							continue
						}
						eof, err := handleInteractiveRecvDuringTurn(r, state, &queuedUserInput)
						if err != nil {
							return err
						}
						if eof {
							recvCh = nil
						}
					}
				} else {
					select {
					case out = <-turnDone:
						break turnWait
					case r, ok := <-recvCh:
						if !ok {
							recvCh = nil
							continue
						}
						eof, err := handleInteractiveRecvDuringTurn(r, state, &queuedUserInput)
						if err != nil {
							return err
						}
						if eof {
							recvCh = nil
						}
					}
				}
			}
			turnCancel()

			if errors.Is(loopCtx.Err(), context.Canceled) {
				if done, err := s.completedExternally(ctx, q, events, sessionID, state); done || err != nil {
					return err
				}
			}

			if wallExpired {
				if err := state.sessionWallClockLimitError(); err != nil {
					if err := s.publishWallClockBlockedAndPersist(ctx, q, events, sessionID, state, lastStopReason, lastTurnUsage, lastOutput); err != nil {
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
				if handled, err := s.handleInteractiveTurnError(ctx, q, events, state, lastStopReason, lastTurnUsage, out.err); err != nil {
					return err
				} else if handled {
					if wc := state.sessionWallClockLimitError(); wc != nil {
						if err := s.persistWallClockTerminal(ctx, q, sessionID, state, limitErrorMessage(wc), lastOutput); err != nil {
							return err
						}
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
				return s.failInteractiveSession(ctx, q, events, sessionID, out.err)
			}

			userText, err := userTextFromSessionInput(pendingInput)
			if err != nil {
				return s.failInteractiveSession(ctx, q, events, sessionID, err)
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
			if state.delegationDepth > 0 {
				s.clearActiveSessionLiveAssistant(sessionID)
				return s.completeInteractiveSession(ctx, q, events, sessionID, out.stopReason, outputJSON, state.turnCount, out.turnUsage, state.sessionUsage, historyJSON)
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
			s.clearActiveSessionLiveAssistant(sessionID)
			lastStopReason = out.stopReason
			lastOutput = outputJSON
			lastTurnUsage = out.turnUsage

			if err := sendAwaitingInput(events, out.stopReason, state.turnCount, out.turnUsage, state.sessionUsage, state.inputBlockedReason); err != nil {
				return err
			}
			pendingInput = nil
			if len(queuedUserInput) > 0 {
				pendingInput = queuedUserInput
				queuedUserInput = nil
				continue
			}
			if !waitForUser {
				return nil
			}
			if done, err := s.finishInteractiveIfClientClosed(ctx, q, events, sessionID, state, waitForUser, lastStopReason, lastOutput, lastTurnUsage); done || err != nil {
				return err
			}
			continue
		}

		if done, err := s.finishInteractiveIfClientClosed(ctx, q, events, sessionID, state, waitForUser, lastStopReason, lastOutput, lastTurnUsage); done || err != nil {
			return err
		}

		select {
		case <-loopCtx.Done():
			if errors.Is(loopCtx.Err(), context.Canceled) {
				wasCancelled, cerr := sessionWasCancelled(ctx, q, sessionID)
				if cerr != nil {
					return cerr
				}
				if wasCancelled {
					session, serr := q.GetSession(sessionLookupCtx(ctx), sessionID)
					if serr != nil {
						return serr
					}
					if err := sendSessionCancelled(events, session.UpdatedAt); err != nil {
						return err
					}
					return nil
				}
				if done, err := s.completedExternally(ctx, q, events, sessionID, state); done || err != nil {
					return err
				}
			}
			return loopCtx.Err()
		case <-wallC:
			wallC = nil
			if err := s.publishWallClockBlockedAndPersist(ctx, q, events, sessionID, state, lastStopReason, lastTurnUsage, lastOutput); err != nil {
				return err
			}
		case r, ok := <-recvCh:
			if !ok {
				recvCh = nil
				continue
			}
			if r.err != nil {
				if errors.Is(r.err, io.EOF) {
					state.clientRecvEOF = true
					if done, err := s.finishInteractiveIfClientClosed(ctx, q, events, sessionID, state, waitForUser, lastStopReason, lastOutput, lastTurnUsage); done || err != nil {
						return err
					}
					continue
				}
				return r.err
			}

			if ta := r.msg.GetToolApproval(); ta != nil {
				if state.approvalGate != nil {
					if err := state.approvalGate.deliverApproval(ta); err != nil {
						return status.Error(codes.InvalidArgument, err.Error())
					}
				}
				continue
			}
			um := r.msg.GetUserMessage()
			if um == nil {
				return status.Error(codes.InvalidArgument, "expected user_message after awaiting_input")
			}
			if state.inputBlockedReason != "" {
				if err := state.publishInputBlocked(events, lastStopReason, lastTurnUsage); err != nil {
					return err
				}
				continue
			}
			if err := state.sessionLimitErrorBeforeTurn(); err != nil {
				if isWallClockLimitError(err) {
					if err := s.publishWallClockBlockedAndPersist(ctx, q, events, sessionID, state, lastStopReason, lastTurnUsage, lastOutput); err != nil {
						return err
					}
				} else {
					state.blockInput(err)
					if err := state.publishInputBlocked(events, lastStopReason, lastTurnUsage); err != nil {
						return err
					}
				}
				continue
			}
			if _, err := q.UpdateSession(ctx, store.UpdateSessionParams{
				ID:     sessionID,
				Status: model.SessionStatusRunning,
			}); err != nil {
				return status.Errorf(codes.Internal, "update session: %v", err)
			}
			encoded, err := encodeInteractiveUserMessageText(um.GetText())
			if err != nil {
				return err
			}
			pendingInput = encoded
		}
	}
}

// finishInteractiveIfClientClosed completes or ends the session when the client
// closed its send side and there is no pending turn input.
func (s *runtimeServer) finishInteractiveIfClientClosed(
	ctx context.Context,
	q *store.Queries,
	events sessionEventSink,
	sessionID string,
	state *interactiveSessionState,
	waitForUser bool,
	lastStopReason string,
	lastOutput json.RawMessage,
	lastTurnUsage provider.TokenUsage,
) (done bool, err error) {
	if !waitForUser || !state.clientRecvEOF {
		return false, nil
	}
	if len(lastOutput) == 0 {
		return true, nil
	}
	return true, s.completeInteractiveSession(ctx, q, events, sessionID, lastStopReason, lastOutput, state.turnCount, lastTurnUsage, state.sessionUsage, nil)
}

func encodeInteractiveUserMessageText(text string) (json.RawMessage, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, status.Error(codes.InvalidArgument, "user_message.text must be non-empty")
	}
	encoded, err := json.Marshal(map[string]string{"message": text})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode user message: %v", err)
	}
	return encoded, nil
}

// handleInteractiveRecvDuringTurn delivers tool approvals and queues user messages
// that arrive while a turn is still running.
func handleInteractiveRecvDuringTurn(
	r interactiveClientRecv,
	state *interactiveSessionState,
	queuedUserInput *json.RawMessage,
) (eof bool, err error) {
	if r.err != nil {
		if errors.Is(r.err, io.EOF) {
			state.clientRecvEOF = true
			return true, nil
		}
		return false, r.err
	}
	if ta := r.msg.GetToolApproval(); ta != nil {
		if state.approvalGate == nil {
			return false, nil
		}
		return false, state.approvalGate.deliverApproval(ta)
	}
	if um := r.msg.GetUserMessage(); um != nil {
		encoded, err := encodeInteractiveUserMessageText(um.GetText())
		if err != nil {
			return false, err
		}
		if len(*queuedUserInput) == 0 {
			*queuedUserInput = encoded
		}
		return false, nil
	}
	return false, status.Error(codes.InvalidArgument, "unexpected client message during turn")
}

func sendAwaitingInput(
	events sessionEventSink,
	stopReason string,
	turn int,
	turnUsage, sessionUsage provider.TokenUsage,
	inputBlockedReason string,
) error {
	return events.Send(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_AwaitingInput{
			AwaitingInput: &runtimev1.RunSessionInteractiveAwaitingInput{
				StopReason:         stopReason,
				Stats:              interactiveSessionStats(turn, turnUsage, sessionUsage),
				InputBlockedReason: inputBlockedReason,
			},
		},
	})
}

// sendInteractiveCompletedFromSession emits the terminal Completed event built
// from a session's persisted output. Used both when attaching to an
// already-completed session and when a session is completed out-of-band.
func sendInteractiveCompletedFromSession(events sessionEventSink, session store.Session, history []provider.Message) error {
	output := session.Output
	if len(output) == 0 {
		output = json.RawMessage("null")
	}
	return events.Send(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_Completed{
			Completed: &runtimev1.RunSessionInteractiveCompleted{
				StopReason:           stopReasonFromSessionOutput(session.Output),
				Output:               output,
				Stats:                interactiveStatsFromSessionOutput(history, output),
				SessionEndedAtUnixMs: session.UpdatedAt.UnixMilli(),
			},
		},
	})
}

// completedExternally emits the Completed terminal event when the session was
// marked completed out-of-band (CompleteSession RPC) while a driver/attach loop
// was running. Returns done=true when the event was sent.
func (s *runtimeServer) completedExternally(
	ctx context.Context,
	q *store.Queries,
	events sessionEventSink,
	sessionID string,
	state *interactiveSessionState,
) (done bool, err error) {
	session, gerr := q.GetSession(sessionLookupCtx(ctx), sessionID)
	if errors.Is(gerr, sql.ErrNoRows) {
		return false, status.Errorf(codes.NotFound, "session %s not found", sessionID)
	}
	if gerr != nil {
		return false, status.Errorf(codes.Internal, "load session: %v", gerr)
	}
	if session.Status != model.SessionStatusCompleted {
		return false, nil
	}
	if err := sendInteractiveCompletedFromSession(events, session, state.history); err != nil {
		return false, err
	}
	return true, nil
}

func sendSessionStarted(
	events sessionEventSink,
	sessionID, agentVersionID string,
	ver *executor.Version,
	history []provider.Message,
	sessionStartedAt time.Time,
	sessionEndedAt *time.Time,
	descriptiveMetadata *runtimev1.DescriptiveMetadataEvidence,
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
	return events.Send(&runtimev1.RunSessionInteractiveServerMsg{
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
				DescriptiveMetadata:    descriptiveMetadata,
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
