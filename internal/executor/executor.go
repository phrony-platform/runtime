package executor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/policy"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/secrets"
	"github.com/phrony-platform/runtime/internal/store"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

// EventType classifies executor streaming events.
type EventType string

const (
	EventTextDelta          EventType = "text_delta"
	EventCompleted          EventType = "completed"
	EventFailed             EventType = "failed"
	EventEscalation         EventType = "escalation"
	EventToolCall           EventType = "tool_call"
	EventToolResult         EventType = "tool_result"
	EventApprovalRequired   EventType = "approval_required"
)

// Event is emitted while streaming a completion.
type Event struct {
	Type       EventType
	TextDelta  string
	StopReason string
	Usage      provider.TokenUsage
	Err        error
	ToolCall   ToolCallEvent
	ToolResult ToolResultEvent
	Approval   policy.ApprovalRequest
}

// Executor runs agent sessions against deployed versions.
type Executor struct {
	Enc *secrets.Encryptor
	Q   *store.Queries
}

// Version is a loaded agent version ready for completion.
type Version struct {
	AgentVersionID string
	Agent          *manifest.Agent
	provider       provider.Provider
}

// NewVersionWithProvider constructs a Version for tests that inject a provider implementation.
func NewVersionWithProvider(agentVersionID string, agent *manifest.Agent, p provider.Provider) *Version {
	return &Version{
		AgentVersionID: agentVersionID,
		Agent:          agent,
		provider:       p,
	}
}

// LoadVersionForSession loads the ref-only manifest and provider credentials
// decrypted from session-scoped secrets for the given session.
func (e *Executor) LoadVersionForSession(ctx context.Context, sessionID, agentVersionID string) (*Version, error) {
	if e == nil {
		return nil, fmt.Errorf("executor is nil")
	}
	if e.Q == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if agentVersionID == "" {
		return nil, fmt.Errorf("agent_version_id is required")
	}

	raw, err := e.Q.GetAgentVersionManifest(ctx, agentVersionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("agent version %q not found", agentVersionID)
	}
	if err != nil {
		return nil, fmt.Errorf("load manifest: %w", err)
	}

	agent, err := manifest.ParseJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	reg, err := provider.NewForSession(ctx, e.Enc, e.Q, sessionID, agent)
	if err != nil {
		return nil, err
	}
	p, err := reg.ModelProvider(agent)
	if err != nil {
		return nil, err
	}

	return &Version{
		AgentVersionID: agentVersionID,
		Agent:          agent,
		provider:       p,
	}, nil
}

// RunParams configures a single user turn (which may include multiple model completions in a tool-use loop).
type RunParams struct {
	SessionID      string
	Turn           int
	Input          json.RawMessage
	History        []provider.Message
	Dispatcher     tooldispatch.Dispatcher
	Policies       *policy.Evaluator
	ApprovalGate   policy.ApprovalGate
	NewApprovalID  func() string
	// ResumeFromHistory continues a turn from persisted history (recovery) without new user input.
	ResumeFromHistory bool
	// PriorDelegatedUsage seeds the run token budget with delegated tool usage
	// already charged in the durable ledger (recovery replays tool results without
	// re-dispatching).
	PriorDelegatedUsage int
	// BeforeToolDispatch persists assistant tool_use history before dispatch (durability).
	BeforeToolDispatch func(ctx context.Context, messages []provider.Message) error
}

type completionOutcome struct {
	stopReason string
	usage      provider.TokenUsage
	text       string
	toolCalls  []provider.ToolCall
}

// StreamCompletion runs the tool-use loop for one user turn, forwarding provider events to ch.
// The channel is closed when StreamCompletion returns.
func (v *Version) StreamCompletion(ctx context.Context, params RunParams, ch chan<- Event) error {
	if v == nil || v.Agent == nil {
		return fmt.Errorf("agent version is not loaded")
	}
	if v.provider == nil {
		return fmt.Errorf("model provider is not configured")
	}
	if ch == nil {
		return fmt.Errorf("event channel is required")
	}
	defer close(ch)

	tracker := newLimitTracker(v.Agent.Spec.Limits)
	if err := tracker.addUsageTokens(params.PriorDelegatedUsage); err != nil {
		return reportLimit(ch, err)
	}
	usageEst := newUsageEstimator()

	var messages []provider.Message
	if params.ResumeFromHistory {
		if len(params.History) == 0 {
			err := fmt.Errorf("resume requires non-empty history")
			emitFailed(ch, err)
			return err
		}
		messages = append([]provider.Message(nil), params.History...)
	} else {
		var err error
		messages, err = buildMessages(v.Agent, params.Input)
		if err != nil {
			emitFailed(ch, err)
			return err
		}
		if len(params.History) > 0 {
			messages = append(append([]provider.Message(nil), params.History...), messages...)
		}
	}

	toolDefs, err := buildToolDefinitions(v.Agent)
	if err != nil {
		emitFailed(ch, err)
		return err
	}

	tokensCounted := 0
	countMessages := func(msgs []provider.Message) error {
		for i := tokensCounted; i < len(msgs); i++ {
			text := messageTextForTokens(msgs[i])
			usageEst.addInput(text)
			if err := tracker.addTokens(text); err != nil {
				return err
			}
		}
		tokensCounted = len(msgs)
		return nil
	}
	if err := countMessages(messages); err != nil {
		return reportLimit(ch, err)
	}

	runCtx, cancel := tracker.context(ctx)
	defer cancel()

	turn := params.Turn
	if turn <= 0 {
		turn = 1
	}

	var totalUsage provider.TokenUsage

	for {
		if err := tracker.beginIteration(); err != nil {
			return reportLimit(ch, err)
		}
		if err := tracker.checkWallClock(); err != nil {
			return reportLimit(ch, err)
		}

		outcome, err := v.runOneCompletion(runCtx, messages, toolDefs, ch, tracker, usageEst)
		if err != nil {
			if emitErr := reportLimit(ch, err); emitErr != err {
				return emitErr
			}
			return err
		}

		totalUsage = mergeUsage(totalUsage, outcome.usage)

		switch outcome.stopReason {
		case provider.StopReasonEndTurn, provider.StopReasonMaxTokens:
			ch <- Event{Type: EventCompleted, StopReason: outcome.stopReason, Usage: totalUsage}
			return nil

		case provider.StopReasonToolUse:
			if len(outcome.toolCalls) == 0 {
				err := fmt.Errorf("model stopped for tool_use without tool calls")
				emitFailed(ch, err)
				return err
			}
			if params.Dispatcher == nil {
				err := fmt.Errorf("tool dispatcher is not configured")
				emitFailed(ch, err)
				return err
			}

			assistantBlocks := assistantBlocksFromOutcome(outcome)
			messages = append(messages, provider.Message{
				Role:    provider.RoleAssistant,
				Blocks:  assistantBlocks,
				Content: outcome.text,
			})
			if err := countMessages(messages[len(messages)-1:]); err != nil {
				return reportLimit(ch, err)
			}
			if params.BeforeToolDispatch != nil {
				if err := params.BeforeToolDispatch(runCtx, messages); err != nil {
					emitFailed(ch, err)
					return err
				}
			}

			resultBlocks, err := v.dispatchToolCalls(
				runCtx, params, turn, outcome.toolCalls, params.Dispatcher, tracker, ch,
			)
			if err != nil {
				if emitErr := reportLimit(ch, err); emitErr != err {
					return emitErr
				}
				emitFailed(ch, err)
				return err
			}

			messages = append(messages, provider.Message{
				Role:   provider.RoleUser,
				Blocks: resultBlocks,
			})
			if err := countMessages(messages[len(messages)-1:]); err != nil {
				return reportLimit(ch, err)
			}
			continue

		default:
			ch <- Event{Type: EventCompleted, StopReason: outcome.stopReason, Usage: totalUsage}
			return nil
		}
	}
}

func (v *Version) runOneCompletion(
	ctx context.Context,
	messages []provider.Message,
	toolDefs []provider.ToolDefinition,
	ch chan<- Event,
	tracker *limitTracker,
	usageEst *usageEstimator,
) (completionOutcome, error) {
	req := provider.CompletionRequest{
		Model:           v.Agent.Spec.Model.Name,
		Messages:        messages,
		Tools:           toolDefs,
		Parameters:      v.Agent.Spec.Model.Parameters,
		Reasoning:       v.Agent.Spec.Model.Reasoning,
		ProviderOptions: v.Agent.Spec.Model.ProviderOptions,
	}

	// Buffered so early returns (limits, cancel) do not deadlock the provider goroutine.
	events := make(chan provider.CompletionEvent, 64)
	providerErrCh := make(chan error, 1)
	go func() {
		providerErrCh <- v.provider.Complete(ctx, req, events)
	}()

	var (
		text      strings.Builder
		toolCalls []provider.ToolCall
		usage     provider.TokenUsage
		stopReason string
	)

	for ev := range events {
		if err := tracker.checkWallClock(); err != nil {
			_ = <-providerErrCh
			return completionOutcome{}, err
		}

		switch ev.Type {
		case provider.EventTextDelta:
			if ev.TextDelta == "" {
				continue
			}
			text.WriteString(ev.TextDelta)
			usageEst.addOutput(ev.TextDelta)
			if err := tracker.addTokens(ev.TextDelta); err != nil {
				_ = <-providerErrCh
				return completionOutcome{}, err
			}
			ch <- Event{Type: EventTextDelta, TextDelta: ev.TextDelta}

		case provider.EventToolCall:
			if ev.ToolCall != nil {
				toolCalls = append(toolCalls, *ev.ToolCall)
			}

		case provider.EventCompleted:
			stopReason = ev.StopReason
			usage = ev.Usage
			if usage.IsZero() {
				usage = usageEst.usage()
			}
			if err := <-providerErrCh; err != nil {
				return completionOutcome{}, err
			}
			return completionOutcome{
				stopReason: stopReason,
				usage:      usage,
				text:       text.String(),
				toolCalls:  toolCalls,
			}, nil

		case provider.EventFailed:
			err := ev.Err
			if err == nil {
				err = fmt.Errorf("model completion failed")
			}
			_ = <-providerErrCh
			return completionOutcome{}, err
		}
	}

	if err := <-providerErrCh; err != nil {
		return completionOutcome{}, err
	}
	if err := tracker.checkWallClock(); err != nil {
		return completionOutcome{}, err
	}
	return completionOutcome{}, fmt.Errorf("model completion ended without a terminal event")
}

func assistantBlocksFromOutcome(out completionOutcome) []provider.ContentBlock {
	var blocks []provider.ContentBlock
	if text := strings.TrimSpace(out.text); text != "" {
		blocks = append(blocks, provider.TextBlock(text))
	}
	for _, call := range out.toolCalls {
		args := call.Args
		if len(args) == 0 {
			args = json.RawMessage("{}")
		}
		blocks = append(blocks, provider.ToolUseBlock(call.ID, call.Name, args))
	}
	return blocks
}

func mergeUsage(a, b provider.TokenUsage) provider.TokenUsage {
	if a.IsZero() {
		return b
	}
	if b.IsZero() {
		return a
	}
	return provider.TokenUsage{
		InputTokens:  a.InputTokens + b.InputTokens,
		OutputTokens: a.OutputTokens + b.OutputTokens,
		Estimated:    a.Estimated || b.Estimated,
	}
}

func reportLimit(ch chan<- Event, err error) error {
	var lim *LimitError
	if errors.As(err, &lim) && lim.OnLimit == OnLimitEscalate {
		esc := &EscalationError{Limit: lim}
		ch <- Event{Type: EventEscalation, Err: esc}
		return esc
	}
	emitFailed(ch, err)
	return err
}

func emitFailed(ch chan<- Event, err error) {
	if err == nil {
		return
	}
	ch <- Event{Type: EventFailed, Err: err}
}
