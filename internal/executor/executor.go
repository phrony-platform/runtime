package executor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/provider"
	"github.com/phrony-platform/runtime/internal/secrets"
	"github.com/phrony-platform/runtime/internal/store"
)

// EventType classifies executor streaming events.
type EventType string

const (
	EventTextDelta EventType = "text_delta"
	EventCompleted EventType = "completed"
	EventFailed    EventType = "failed"
)

// Event is emitted while streaming a completion.
type Event struct {
	Type       EventType
	TextDelta  string
	StopReason string
	Err        error
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

// LoadVersion loads the ref-only manifest and provider credentials for a deployed version.
func (e *Executor) LoadVersion(ctx context.Context, agentVersionID string) (*Version, error) {
	if e == nil {
		return nil, fmt.Errorf("executor is nil")
	}
	if e.Q == nil {
		return nil, fmt.Errorf("database is not configured")
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

	reg, err := provider.NewForAgentVersion(ctx, e.Enc, e.Q, agentVersionID, agent)
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

// RunParams configures a single completion turn.
type RunParams struct {
	Input   json.RawMessage
	History []provider.Message
}

// StreamCompletion runs one model completion, forwarding provider events to ch.
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
	if err := tracker.beginIteration(); err != nil {
		emitFailed(ch, err)
		return err
	}

	messages, err := buildMessages(v.Agent, params.Input)
	if err != nil {
		emitFailed(ch, err)
		return err
	}
	if len(params.History) > 0 {
		messages = append(append([]provider.Message(nil), params.History...), messages...)
	}

	for _, m := range messages {
		if err := tracker.addTokens(m.Content); err != nil {
			emitFailed(ch, err)
			return err
		}
	}

	runCtx, cancel := tracker.context(ctx)
	defer cancel()

	req := provider.CompletionRequest{
		Model:           v.Agent.Spec.Model.Name,
		Messages:        messages,
		Parameters:      v.Agent.Spec.Model.Parameters,
		Reasoning:       v.Agent.Spec.Model.Reasoning,
		ProviderOptions: v.Agent.Spec.Model.ProviderOptions,
	}

	events := make(chan provider.CompletionEvent)
	providerErrCh := make(chan error, 1)
	go func() {
		providerErrCh <- v.provider.Complete(runCtx, req, events)
	}()

	for ev := range events {
		if err := tracker.checkWallClock(); err != nil {
			emitFailed(ch, err)
			return err
		}

		switch ev.Type {
		case provider.EventTextDelta:
			if ev.TextDelta == "" {
				continue
			}
			if err := tracker.addTokens(ev.TextDelta); err != nil {
				emitFailed(ch, err)
				return err
			}
			ch <- Event{Type: EventTextDelta, TextDelta: ev.TextDelta}

		case provider.EventCompleted:
			ch <- Event{Type: EventCompleted, StopReason: ev.StopReason}
			if err := <-providerErrCh; err != nil {
				return err
			}
			return nil

		case provider.EventFailed:
			err := ev.Err
			if err == nil {
				err = fmt.Errorf("model completion failed")
			}
			emitFailed(ch, err)
			_ = <-providerErrCh
			return err
		}
	}

	if err := <-providerErrCh; err != nil {
		emitFailed(ch, err)
		return err
	}
	if err := tracker.checkWallClock(); err != nil {
		emitFailed(ch, err)
		return err
	}
	err = fmt.Errorf("model completion ended without a terminal event")
	emitFailed(ch, err)
	return err
}

func emitFailed(ch chan<- Event, err error) {
	if err == nil {
		return
	}
	ch <- Event{Type: EventFailed, Err: err}
}
