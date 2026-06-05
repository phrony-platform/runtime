package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/store"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
)

// rootSessionDepth is the delegation depth of a top-level (non-delegated) run.
const rootSessionDepth = 0

// defaultMaxSubagentDepth caps agent-to-agent delegation nesting when the
// manifest does not set spec.limits.max_subagent_depth, preventing unbounded
// recursion (e.g. A->B->A) from exhausting resources.
const defaultMaxSubagentDepth = 5

// agentBinding is a compiled spec.agents entry resolved for dispatch: the target
// agent identity (active deployment when version is empty) and the result shape.
type agentBinding struct {
	namespace string
	name      string
	version   string
	result    string
}

// agentDispatcher backs spec.agents tool bindings by running the target agent in
// an isolated nested child session and returning its final output as the tool
// result. It implements tooldispatch.Routable so it composes with
// RoutingDispatcher exactly like the MCP dispatcher: policy gates, the approval
// flow, the invocation ledger, and recovery all run upstream on the logical ref.
//
// sessionCtx is the driving session's context (session-scoped, matching the
// dispatcher lifetime). Child runs derive from it so parent cancellation
// propagates, while the parent's remaining wall-clock budget (ToolCall.Deadline)
// bounds the child rather than the short worker-queue dispatch timeout.
type agentDispatcher struct {
	server          *runtimeServer
	sessionCtx      context.Context
	parentSessionID string
	depth           int
	maxDepth        int
	bindings        map[string]agentBinding
	recorder        *ToolInvocationRecorder
}

// buildAgentDispatcher returns a dispatcher for the version's agent-backed tool
// bindings, or nil when the version declares none.
func (s *runtimeServer) buildAgentDispatcher(
	ctx context.Context,
	q *store.Queries,
	sessionID string,
	agent *manifest.Agent,
	depth int,
) *agentDispatcher {
	bindings := make(map[string]agentBinding)
	for i := range agent.Spec.Tools {
		tb := &agent.Spec.Tools[i]
		if !tb.IsAgent() {
			continue
		}
		bindings[tb.DispatchRef()] = agentBinding{
			namespace: tb.Agent.Namespace,
			name:      tb.Agent.Name,
			version:   tb.Agent.Version,
			result:    tb.Agent.ResolvedResult(),
		}
	}
	if len(bindings) == 0 {
		return nil
	}
	return &agentDispatcher{
		server:          s,
		sessionCtx:      ctx,
		parentSessionID: sessionID,
		depth:           depth,
		maxDepth:        resolveMaxSubagentDepth(agent),
		bindings:        bindings,
		recorder:        NewToolInvocationRecorder(q),
	}
}

func resolveMaxSubagentDepth(agent *manifest.Agent) int {
	if agent != nil && agent.Spec.Limits != nil && agent.Spec.Limits.MaxSubagentDepth != nil {
		if v := *agent.Spec.Limits.MaxSubagentDepth; v > 0 {
			return v
		}
	}
	return defaultMaxSubagentDepth
}

// Handles reports whether the dispatcher backs the logical tool ref. A routing
// dispatcher uses this to decide between the agent backend and the fallback.
func (d *agentDispatcher) Handles(tool string) bool {
	_, ok := d.bindings[tool]
	return ok
}

// Close releases dispatcher resources. The agent dispatcher holds none (child
// sessions clean themselves up on completion).
func (d *agentDispatcher) Close() error { return nil }

func (d *agentDispatcher) Dispatch(ctx context.Context, call tooldispatch.ToolCall) (tooldispatch.ToolResult, error) {
	if call.CallID == "" {
		return tooldispatch.ToolResult{}, fmt.Errorf("call_id is required")
	}
	binding, ok := d.bindings[call.Tool]
	if !ok {
		return tooldispatch.ToolResult{}, fmt.Errorf("%w: tool %q is not agent-backed", tooldispatch.ErrNoHandler, call.Tool)
	}

	// The child runs synchronously and may outlive the short worker-queue
	// timeout on the incoming dispatch context, so derive a run context from the
	// session lifetime bounded by the parent's wall-clock budget instead.
	runCtx, cancel := d.childRunContext(call)
	defer cancel()

	rec := d.recorder
	if rec != nil {
		if stored, found, err := rec.LookupCompleted(runCtx, call.CallID); err != nil {
			return tooldispatch.ToolResult{}, err
		} else if found {
			return stored, nil
		}
		if err := rec.RecordPending(runCtx, call, ""); err != nil {
			return tooldispatch.ToolResult{}, fmt.Errorf("record tool invocation: %w", err)
		}
		if err := rec.RecordDispatched(runCtx, tooldispatch.DispatchProvenance{Call: call}); err != nil {
			_ = rec.RecordCompleted(runCtx, call, tooldispatch.ToolResult{}, err)
			return tooldispatch.ToolResult{}, fmt.Errorf("record tool dispatch: %w", err)
		}
	}

	res, runErr := d.runChild(runCtx, call, binding)
	if runErr != nil {
		// The child run failed for an unknown reason (the nested session may or
		// may not have had effects), so map to ErrIndeterminate and let the
		// side-effect-class recovery routing apply, as the MCP dispatcher does.
		indErr := fmt.Errorf("%w: %v", tooldispatch.ErrIndeterminate, runErr)
		if rec != nil {
			_ = rec.RecordIndeterminate(runCtx, call, indErr.Error())
		}
		return tooldispatch.ToolResult{}, indErr
	}
	if rec != nil {
		if err := rec.RecordCompleted(runCtx, call, res, nil); err != nil {
			return tooldispatch.ToolResult{}, fmt.Errorf("record tool result: %w", err)
		}
	}
	return res, nil
}

// childRunContext derives the child run context from the session lifetime,
// bounding it by the parent's remaining wall-clock budget when one is set.
func (d *agentDispatcher) childRunContext(call tooldispatch.ToolCall) (context.Context, context.CancelFunc) {
	base := d.sessionCtx
	if base == nil {
		base = context.Background()
	}
	if call.Deadline.IsZero() {
		return context.WithCancel(base)
	}
	return context.WithDeadline(base, call.Deadline)
}

// runChild resolves and runs the target agent as a nested child session and
// returns its final output as the tool result. Expected failures (depth cap,
// unresolved target, missing secret, child failure) are surfaced to the parent
// model as tool errors; only unexpected failures return a non-nil error.
func (d *agentDispatcher) runChild(ctx context.Context, call tooldispatch.ToolCall, binding agentBinding) (tooldispatch.ToolResult, error) {
	childDepth := d.depth + 1
	if childDepth > d.maxDepth {
		return subagentToolError(call.CallID, "subagent_depth_exceeded",
			fmt.Sprintf("delegation depth %d exceeds max_subagent_depth %d", childDepth, d.maxDepth)), nil
	}

	s := d.server
	q, err := s.queries()
	if err != nil {
		return tooldispatch.ToolResult{}, err
	}

	agentVersionID, err := resolveAgentVersionID(ctx, s.db.DB, &runtimev1.AgentRef{
		Namespace: binding.namespace,
		Name:      binding.name,
		Version:   binding.version,
	})
	if err != nil {
		return subagentToolError(call.CallID, "subagent_unresolved", err.Error()), nil
	}

	inputJSON := childInputFromArgs(call.Args)

	childSessionID, err := s.createChildSession(ctx, q, d.parentSessionID, call.CallID, agentVersionID, inputJSON, childDepth)
	if err != nil {
		var missing *missingSecretError
		if errors.As(err, &missing) {
			return subagentToolError(call.CallID, "subagent_missing_secret", err.Error()), nil
		}
		return tooldispatch.ToolResult{}, err
	}

	// On recovery the resumed child may already be terminal; only drive it when
	// it still needs to advance, then read its outcome either way.
	child, err := q.GetSession(ctx, childSessionID)
	if err != nil {
		return tooldispatch.ToolResult{}, fmt.Errorf("load subagent session: %w", err)
	}
	if !sessionStatusTerminal(child.Status) {
		ver, err := s.loadSessionVersion(ctx, q, childSessionID, agentVersionID)
		if err != nil {
			s.failChildSession(ctx, q, childSessionID, err.Error())
			return subagentToolError(call.CallID, "subagent_load_failed", err.Error()), nil
		}
		if err := s.runChildSessionToCompletion(ctx, q, childSessionID, agentVersionID, ver, inputJSON, childDepth); err != nil {
			return tooldispatch.ToolResult{}, fmt.Errorf("run subagent session: %w", err)
		}
	}

	return d.childResult(ctx, q, call.CallID, childSessionID, binding.result)
}

// childSessionNamespace seeds deterministic child session ids derived from the
// originating tool call id, so an interrupted delegation resumes the same
// durable child on recovery instead of spawning a duplicate.
var childSessionNamespace = uuid.MustParse("b3f2a1c0-1d4e-4f6a-9c2b-7e8d9a0b1c2d")

// childSessionID returns the stable session id for the child spawned by a tool
// call. It is a pure function of the call id so recovery can locate the child.
func childSessionID(callID string) string {
	return "run_" + uuid.NewSHA1(childSessionNamespace, []byte(callID)).String()
}

// childResult reads the finished child session and maps it to a tool result:
// completed runs return their output (and usage for parent token accounting),
// failed or non-terminal runs become tool errors for the parent model.
func (d *agentDispatcher) childResult(
	ctx context.Context,
	q *store.Queries,
	callID, childSessionID, resultShape string,
) (tooldispatch.ToolResult, error) {
	session, err := q.GetSession(ctx, childSessionID)
	if err != nil {
		return tooldispatch.ToolResult{}, fmt.Errorf("load subagent session: %w", err)
	}
	switch session.Status {
	case model.SessionStatusCompleted:
		// Build the payload below.
	case model.SessionStatusFailed:
		msg := "subagent run failed"
		if session.Error != nil && *session.Error != "" {
			msg = *session.Error
		}
		return subagentToolError(callID, "subagent_failed", msg), nil
	default:
		return subagentToolError(callID, "subagent_incomplete",
			fmt.Sprintf("subagent ended in non-terminal status %q", session.Status)), nil
	}

	payload, err := subagentResultPayload(session.Output, resultShape)
	if err != nil {
		return tooldispatch.ToolResult{}, err
	}
	res := tooldispatch.ToolResult{CallID: callID, Payload: payload}
	if _, sessionUsage := usageFromSessionOutputJSON(session.Output); !sessionUsage.IsZero() {
		res.Usage = &tooldispatch.ToolUsage{
			InputTokens:  sessionUsage.InputTokens,
			OutputTokens: sessionUsage.OutputTokens,
			Estimated:    sessionUsage.Estimated,
		}
	}
	return res, nil
}

// runChildSessionToCompletion drives a freshly created child session to a
// terminal state using the shared session driver. The child runs autonomously:
// a closed input stream yields no operator messages, so the driver completes the
// session after its first turn rather than parking for input.
func (s *runtimeServer) runChildSessionToCompletion(
	ctx context.Context,
	q *store.Queries,
	childSessionID, agentVersionID string,
	ver *executor.Version,
	inputJSON json.RawMessage,
	depth int,
) error {
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := s.registerActiveSession(childSessionID, activeSessionEntry{cancel: cancel}); err != nil {
		return err
	}
	defer s.unregisterActiveSession(childSessionID)

	stream := newClosedChildInputStream(childCtx)
	return s.driveSessionToCompletion(
		childCtx, q, childSessionID, agentVersionID, ver,
		noopSessionEventSink{}, stream, inputJSON, true, depth,
	)
}

// newClosedChildInputStream returns an interactive server stream that yields no
// client messages (Recv returns io.EOF immediately) so the session driver runs
// the child autonomously to completion.
func newClosedChildInputStream(ctx context.Context) *sessionInputMux {
	m := newSessionInputMux(ctx)
	m.close()
	return m
}

// createChildSession returns the durable child session for a delegation tool
// call, creating it (with inherited parent secrets, parent linkage, and depth)
// when absent. The session id is derived from the call id, so a delegation that
// is replayed during recovery reuses the existing child rather than spawning a
// duplicate.
func (s *runtimeServer) createChildSession(
	ctx context.Context,
	q *store.Queries,
	parentSessionID, callID, agentVersionID string,
	inputJSON json.RawMessage,
	depth int,
) (string, error) {
	childID := childSessionID(callID)
	if existing, err := q.GetSession(ctx, childID); err == nil {
		return existing.ID, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	agent, err := loadAgentManifestForVersion(ctx, q, agentVersionID)
	if err != nil {
		return "", err
	}
	resolved, err := s.inheritSessionSecrets(ctx, q, parentSessionID, agent)
	if err != nil {
		return "", err
	}
	defer zeroSecretValues(resolved)
	return s.createChildRunSession(ctx, childID, parentSessionID, agentVersionID, inputJSON, resolved, depth)
}

// inheritSessionSecrets resolves the secrets the child agent declares by copying
// the matching plaintext values from the parent session's encrypted store. The
// root run must therefore supply every secret name used across the agent tree.
func (s *runtimeServer) inheritSessionSecrets(
	ctx context.Context,
	q *store.Queries,
	parentSessionID string,
	agent *manifest.Agent,
) (map[string][]byte, error) {
	if agent == nil || len(agent.Secrets) == 0 {
		return nil, nil
	}
	resolved := make(map[string][]byte, len(agent.Secrets))
	for name := range agent.Secrets {
		if s.secretsEnc == nil {
			return nil, &missingSecretError{name: name}
		}
		val, err := s.secretsEnc.DecryptForSession(ctx, q, parentSessionID, name)
		if err != nil {
			return nil, &missingSecretError{name: name}
		}
		resolved[name] = val
	}
	return resolved, nil
}

// failChildSession marks a child session failed and purges its secrets after a
// setup error that prevents the session from being driven.
func (s *runtimeServer) failChildSession(ctx context.Context, q *store.Queries, childSessionID, message string) {
	errText := message
	_, _ = q.UpdateSession(ctx, store.UpdateSessionParams{
		ID:     childSessionID,
		Status: model.SessionStatusFailed,
		Error:  &errText,
	})
	s.finalizeSessionSecrets(ctx, q, childSessionID)
}

func zeroSecretValues(resolved map[string][]byte) {
	for _, v := range resolved {
		for i := range v {
			v[i] = 0
		}
	}
}

// childInputFromArgs maps the parent tool-call arguments to the child session
// input, defaulting to an empty JSON object when the model passed no arguments.
func childInputFromArgs(args json.RawMessage) json.RawMessage {
	if len(args) == 0 {
		return json.RawMessage("{}")
	}
	return args
}

// subagentResultPayload shapes the child output for the parent model: summary
// returns just the final assistant message; full returns the whole session
// output (message, usage, and per-turn trace).
func subagentResultPayload(output json.RawMessage, shape string) (json.RawMessage, error) {
	if shape == manifest.SubagentResultFull {
		if len(output) == 0 {
			return json.RawMessage("{}"), nil
		}
		return output, nil
	}
	message := ""
	if len(output) > 0 {
		var obj sessionOutput
		if err := json.Unmarshal(output, &obj); err != nil {
			return nil, fmt.Errorf("decode subagent output: %w", err)
		}
		message = obj.Message
	}
	payload, err := json.Marshal(map[string]string{"output": message})
	if err != nil {
		return nil, fmt.Errorf("encode subagent result: %w", err)
	}
	return payload, nil
}

func subagentToolError(callID, code, message string) tooldispatch.ToolResult {
	return tooldispatch.ToolResult{
		CallID: callID,
		Err:    &tooldispatch.ToolError{Code: code, Message: message},
	}
}

// missingSecretError marks a required child secret name absent from the parent
// session, surfaced to the parent model as a tool error.
type missingSecretError struct {
	name string
}

func (e *missingSecretError) Error() string {
	return fmt.Sprintf(
		"required secret %q is not available in the parent session; the root run must supply every secret used across the agent tree",
		e.name,
	)
}

var _ tooldispatch.Routable = (*agentDispatcher)(nil)
