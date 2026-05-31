package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *runtimeServer) RunSessionInteractive(stream runtimev1.Runtime_RunSessionInteractiveServer) error {
	ctx := stream.Context()

	q, err := s.queries()
	if err != nil {
		return err
	}

	first, err := stream.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return status.Error(codes.InvalidArgument, "first message must be start")
		}
		return err
	}
	start := first.GetStart()
	if start == nil {
		return status.Error(codes.InvalidArgument, "first message must be start")
	}

	inputJSON, err := normalizeSessionInput(start.GetInput())
	if err != nil {
		return err
	}

	agentVersionID, err := resolveAgentVersionID(ctx, s.db.DB, start.GetAgentRef())
	if err != nil {
		return err
	}

	sessionID := uuid.NewString()
	if _, err := q.InsertSession(ctx, store.InsertSessionParams{
		ID:             sessionID,
		AgentVersionID: agentVersionID,
		Input:          inputJSON,
		Status:         model.SessionStatusRunning,
	}); err != nil {
		return status.Errorf(codes.Internal, "persist session: %v", err)
	}

	ver, err := s.loadSessionVersion(ctx, q, agentVersionID)
	if err != nil {
		return s.failInteractiveSession(ctx, q, stream, sessionID, err)
	}

	modelProvider := ""
	modelName := ""
	if ver.Agent != nil {
		modelProvider = ver.Agent.Spec.Model.Provider
		modelName = ver.Agent.Spec.Model.Name
	}

	if err := stream.Send(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_SessionStarted{
			SessionStarted: &runtimev1.RunSessionInteractiveSessionStarted{
				SessionId:      sessionID,
				AgentVersionId: agentVersionID,
				ModelProvider:  modelProvider,
				ModelName:      modelName,
			},
		},
	}); err != nil {
		return err
	}

	state := &interactiveSessionState{
		sessionID: sessionID,
		version:   ver,
	}

	turnInput := inputJSON
	for {
		stopReason, assistantText, turnUsage, err := state.runTurn(ctx, stream, turnInput)
		if err != nil {
			return s.failInteractiveSession(ctx, q, stream, sessionID, err)
		}

		outputJSON, err := json.Marshal(map[string]string{
			"message":     assistantText,
			"stop_reason": stopReason,
		})
		if err != nil {
			return status.Errorf(codes.Internal, "encode session output: %v", err)
		}

		userText, err := userTextFromSessionInput(turnInput)
		if err != nil {
			return s.failInteractiveSession(ctx, q, stream, sessionID, err)
		}
		state.history = appendTurnHistory(state.history, userText, assistantText)
		state.turnCount++
		state.sessionUsage.Add(turnUsage)

		historyJSON, err := encodeHistory(state.history)
		if err != nil {
			return status.Errorf(codes.Internal, "encode session history: %v", err)
		}
		if _, err := q.UpdateSession(ctx, store.UpdateSessionParams{
			ID:      sessionID,
			Status:  model.SessionStatusAwaitingInput,
			Output:  outputJSON,
			History: historyJSON,
		}); err != nil {
			return status.Errorf(codes.Internal, "update session: %v", err)
		}

		if err := stream.Send(&runtimev1.RunSessionInteractiveServerMsg{
			Body: &runtimev1.RunSessionInteractiveServerMsg_AwaitingInput{
				AwaitingInput: &runtimev1.RunSessionInteractiveAwaitingInput{
					StopReason: stopReason,
					Stats:      interactiveSessionStats(state.turnCount, turnUsage, state.sessionUsage),
				},
			},
		}); err != nil {
			return err
		}

		msg, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return s.completeInteractiveSession(ctx, q, stream, sessionID, stopReason, outputJSON, state.turnCount, turnUsage, state.sessionUsage)
			}
			return err
		}

		um := msg.GetUserMessage()
		if um == nil {
			return status.Error(codes.InvalidArgument, "expected user_message after awaiting_input")
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
		turnInput = encoded
	}
}

type interactiveSessionState struct {
	sessionID    string
	turnCount    int
	sessionUsage provider.TokenUsage
	history      []provider.Message
	version      *executor.Version
}

func (st *interactiveSessionState) runTurn(
	ctx context.Context,
	stream runtimev1.Runtime_RunSessionInteractiveServer,
	input json.RawMessage,
) (stopReason, assistantText string, turnUsage provider.TokenUsage, err error) {
	ch := make(chan executor.Event, 32)
	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- st.version.StreamCompletion(ctx, executor.RunParams{
			Input:   input,
			History: st.history,
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
				StopReason: stopReason,
				Output:     output,
				Stats:      interactiveSessionStats(turn, turnUsage, sessionUsage),
			},
		},
	})
}

func appendTurnHistory(history []provider.Message, userText, assistantText string) []provider.Message {
	if userText != "" {
		history = append(history, provider.Message{Role: provider.RoleUser, Content: userText})
	}
	if assistantText != "" {
		history = append(history, provider.Message{Role: provider.RoleAssistant, Content: assistantText})
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
