package core

import (
	"context"
	"database/sql"
	"errors"
	"io"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// runSessionInteractiveAttachDriver subscribes to the background session driver hub,
// replays persisted state for this attach client, and forwards inbound messages to the
// driver input mux. Closing the attach stream only unsubscribes; it does not cancel
// the driver.
func (s *runtimeServer) runSessionInteractiveAttachDriver(
	ctx context.Context,
	stream runtimev1.Runtime_RunSessionInteractiveServer,
	q *store.Queries,
	sessionID string,
) error {
	entry, ok := s.activeSessionEntryFor(sessionID)
	if !ok || entry.eventHub == nil || entry.inputMux == nil {
		return status.Error(codes.Internal, "active session driver unavailable")
	}

	session, err := q.GetSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return status.Errorf(codes.NotFound, "session %s not found", sessionID)
		}
		return status.Errorf(codes.Internal, "load session: %v", err)
	}

	ver, err := s.loadSessionVersion(ctx, q, sessionID, session.AgentVersionID)
	if err != nil {
		return status.Errorf(codes.Internal, "load agent version: %v", err)
	}

	history, err := decodeHistory(session.History)
	if err != nil {
		return status.Errorf(codes.Internal, "decode session history: %v", err)
	}
	history = enrichHistoryFromSessionOutput(history, session.Output)
	history = patchHistoryLastAssistantFromOutput(history, session.Output)
	endedAt := sessionEndedAtForAttach(&session)

	if session.Status == model.SessionStatusAwaitingInput {
		limitState, err := newInteractiveSessionState(ctx, s, sessionID, session.AgentVersionID, ver, session.CreatedAt, noopSessionEventSink{}, q, rootSessionDepth)
		if err != nil {
			return status.Errorf(codes.Internal, "build tool dispatch: %v", err)
		}
		limitState.history = history
		limitState.turnCount = len(history) / 2
		_, limitState.sessionUsage = usageFromSessionOutputJSON(session.Output)
		if err := limitState.sessionLimitErrorBeforeTurn(); err != nil && isWallClockLimitError(err) {
			return s.attachWallClockTerminal(ctx, q, stream, sessionID, session, limitErrorMessage(err))
		}
	}

	evidenceSnap, err := s.ensureSessionEvidence(ctx, q, sessionID, ver.Agent)
	if err != nil {
		return status.Errorf(codes.Internal, "record session evidence: %v", err)
	}

	events := sessionEventsFromStream(stream)
	if err := sendSessionStarted(events, sessionID, session.AgentVersionID, ver, history, session.CreatedAt, endedAt, evidenceSnapshotToProto(evidenceSnap)); err != nil {
		return err
	}
	if err := sendLiveAssistantReplay(events, history, entry.liveAssistantText()); err != nil {
		return err
	}
	if err := replaySessionEventLog(ctx, q, events, sessionID, pendingApprovalIDForReplay(ctx, q, sessionID)); err != nil {
		return err
	}
	if err := s.replayAttachSessionState(ctx, q, events, sessionID, session, ver, history); err != nil {
		return err
	}

	hubEvents, unsubscribe := entry.eventHub.Subscribe()
	defer unsubscribe()

	return bridgeInteractiveAttachStream(ctx, stream, hubEvents, entry.inputMux)
}

// replayAttachSessionState sends server messages that reflect the current parked
// session status but may have been emitted before this subscriber joined the hub.
func (s *runtimeServer) replayAttachSessionState(
	ctx context.Context,
	q *store.Queries,
	events sessionEventSink,
	sessionID string,
	session store.Session,
	ver *executor.Version,
	history []provider.Message,
) error {
	turnCount := len(history) / 2
	stopReason := stopReasonFromSessionOutput(session.Output)

	switch session.Status {
	case model.SessionStatusAwaitingApproval:
		if pending, err := q.GetPendingApprovalBySession(ctx, sessionID); err == nil {
			req, reqErr := approvalRequestFromStore(ctx, q, pending, sessionID)
			if reqErr == nil {
				_ = events.Send(&runtimev1.RunSessionInteractiveServerMsg{
					Body: &runtimev1.RunSessionInteractiveServerMsg_ApprovalRequired{
						ApprovalRequired: approvalRequiredToProto(req),
					},
				})
			}
		}
		return sendAwaitingInput(events, stopReason, turnCount, provider.TokenUsage{}, provider.TokenUsage{}, "awaiting_approval")

	case model.SessionStatusAwaitingTool:
		return sendAwaitingInput(events, stopReason, turnCount, provider.TokenUsage{}, provider.TokenUsage{}, "awaiting_tool")

	case model.SessionStatusAwaitingInput:
		lastTurnUsage, sessionUsage := usageFromSessionOutputJSON(session.Output)
		state, err := newInteractiveSessionState(ctx, s, sessionID, session.AgentVersionID, ver, session.CreatedAt, events, q, rootSessionDepth)
		if err != nil {
			return status.Errorf(codes.Internal, "build tool dispatch: %v", err)
		}
		state.history = history
		state.turnCount = turnCount
		state.sessionUsage = sessionUsage
		blockedReason := ""
		if err := state.sessionLimitErrorBeforeTurn(); err != nil {
			blockedReason = limitErrorMessage(err)
		}
		return sendAwaitingInput(events, stopReason, turnCount, lastTurnUsage, sessionUsage, blockedReason)

	default:
		return nil
	}
}

// bridgeInteractiveAttachStream forwards hub events to the attach stream and delivers
// inbound user_message / tool_approval messages to the driver input mux until the
// attach stream ends or its context is cancelled.
func bridgeInteractiveAttachStream(
	ctx context.Context,
	stream runtimev1.Runtime_RunSessionInteractiveServer,
	hubEvents <-chan *runtimev1.RunSessionInteractiveServerMsg,
	inputMux *sessionInputMux,
) error {
	recvDone := make(chan struct{})
	recvErr := make(chan error, 1)

	go func() {
		defer close(recvDone)
		for {
			msg, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				select {
				case recvErr <- err:
				case <-ctx.Done():
				}
				return
			}
			if msg.GetStart() != nil {
				select {
				case recvErr <- status.Error(codes.InvalidArgument, "unexpected start after attach"):
				case <-ctx.Done():
				}
				return
			}
			if !isInteractiveAttachInbound(msg) {
				select {
				case recvErr <- status.Error(codes.InvalidArgument, "expected user_message or tool_approval"):
				case <-ctx.Done():
				}
				return
			}
			if !inputMux.deliver(msg) {
				select {
				case recvErr <- status.Error(codes.FailedPrecondition, "session driver is no longer accepting input"):
				case <-ctx.Done():
				}
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-recvDone:
			return nil
		case err := <-recvErr:
			return err
		case msg, ok := <-hubEvents:
			if !ok {
				return nil
			}
			if msg == nil {
				continue
			}
			if err := stream.Send(msg); err != nil {
				return err
			}
		}
	}
}

func isInteractiveAttachInbound(msg *runtimev1.RunSessionInteractiveClientMsg) bool {
	if msg == nil {
		return false
	}
	return msg.GetUserMessage() != nil || msg.GetToolApproval() != nil
}
