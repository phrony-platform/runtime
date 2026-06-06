package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/clierr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	tuiSessionsPaneWidth         = 32
	tuiSessionsPaneMinTermWidth  = 100
	tuiSessionsPaneCollapsedHint = "Ctrl+S sessions"

	tuiChildAttachRetryInterval = 250 * time.Millisecond
	tuiChildAttachInitialDelay  = 300 * time.Millisecond
	tuiChildAttachMaxAttempts   = 120
)

type tuiFocusPane int

const (
	tuiFocusMain tuiFocusPane = iota
	tuiFocusSessions
)

type tuiSessionEntry struct {
	id                string
	label             string
	isParent          bool
	status            string
	transcript        strings.Builder
	streaming         strings.Builder
	delegationCallIDs map[string]struct{}
	sessionInput      []byte
	sessionInputShown bool
}

// delegationChildren maps parent delegation call_id to durable child session id.
type delegationChildren map[string]string

type tuiSessionRegistry struct {
	entries     []*tuiSessionEntry
	selectedIdx int
	byID        map[string]int
}

func newTuiSessionRegistry() tuiSessionRegistry {
	parent := &tuiSessionEntry{
		label:             "Parent",
		isParent:          true,
		status:            "running",
		delegationCallIDs: make(map[string]struct{}),
	}
	return tuiSessionRegistry{
		entries: []*tuiSessionEntry{parent},
		byID:    make(map[string]int),
	}
}

func (r *tuiSessionRegistry) parent() *tuiSessionEntry {
	if len(r.entries) == 0 {
		return nil
	}
	return r.entries[0]
}

func (r *tuiSessionRegistry) selected() *tuiSessionEntry {
	if len(r.entries) == 0 {
		return nil
	}
	if r.selectedIdx < 0 || r.selectedIdx >= len(r.entries) {
		return r.entries[0]
	}
	return r.entries[r.selectedIdx]
}

func (r *tuiSessionRegistry) hasDelegations() bool {
	return len(r.entries) > 1
}

func (r *tuiSessionRegistry) registerChild(id, label string) *tuiSessionEntry {
	if idx, ok := r.byID[id]; ok {
		return r.entries[idx]
	}
	entry := &tuiSessionEntry{
		id:     id,
		label:  label,
		status: "running",
	}
	r.entries = append(r.entries, entry)
	idx := len(r.entries) - 1
	r.byID[id] = idx
	return entry
}

func (r *tuiSessionRegistry) moveSelection(delta int) {
	if len(r.entries) <= 1 {
		return
	}
	next := r.selectedIdx + delta
	if next < 0 {
		next = 0
	}
	if next >= len(r.entries) {
		next = len(r.entries) - 1
	}
	r.selectedIdx = next
}

type childStreamState struct {
	stream         interactiveStream
	recvInFlight   bool
	done           bool
	sendClosed     bool
	attachAttempts int
}

type tuiChildStreamMsg struct {
	childSessionID string
	msg            *runtimev1.RunSessionInteractiveServerMsg
	err            error
}

func (m *runTUI) parentEntry() *tuiSessionEntry {
	return m.sessions.parent()
}

func (m *runTUI) selectedEntry() *tuiSessionEntry {
	return m.sessions.selected()
}

func (m *runTUI) sessionsPaneExpanded() bool {
	return m.delegationPaneVisible && m.width >= tuiSessionsPaneMinTermWidth
}

func (m *runTUI) sessionsPaneWidth() int {
	if !m.delegationPaneVisible {
		return 0
	}
	if m.sessionsPaneExpanded() {
		return tuiSessionsPaneWidth
	}
	return 0
}

func (m *runTUI) registerDelegation(tc *runtimev1.RunSessionInteractiveToolCall) tea.Cmd {
	if tc == nil || !tc.GetAgentDelegation() {
		return nil
	}
	childID := strings.TrimSpace(tc.GetChildSessionId())
	if childID == "" {
		return nil
	}
	label := strings.TrimSpace(tc.GetDelegationTarget())
	if label == "" {
		label = formatToolBindingName(tc.GetTool(), tc.GetVersion())
	}
	entry := m.sessions.registerChild(childID, label)
	if args := tc.GetArgs(); len(args) > 0 {
		entry.sessionInput = append([]byte(nil), args...)
		m.seedChildSessionInput(entry)
	}
	m.delegationPaneVisible = true
	if callID := strings.TrimSpace(tc.GetCallId()); callID != "" {
		parent := m.parentEntry()
		if parent != nil {
			if parent.delegationCallIDs == nil {
				parent.delegationCallIDs = make(map[string]struct{})
			}
			parent.delegationCallIDs[callID] = struct{}{}
		}
		if m.delegationChildren == nil {
			m.delegationChildren = make(delegationChildren)
		}
		m.delegationChildren[callID] = childID
	}
	return m.scheduleChildAttachDelay(childID)
}

func (e *tuiSessionEntry) isDelegationCallID(callID string) bool {
	callID = strings.TrimSpace(callID)
	if callID == "" || e == nil || e.delegationCallIDs == nil {
		return false
	}
	_, ok := e.delegationCallIDs[callID]
	return ok
}

func (m *runTUI) isDelegationCallID(callID string) bool {
	parent := m.parentEntry()
	if parent == nil {
		return false
	}
	return parent.isDelegationCallID(callID)
}

func (m *runTUI) ensureChildStreamState(childSessionID string) *childStreamState {
	if m.childStreams == nil {
		m.childStreams = make(map[string]*childStreamState)
	}
	state := m.childStreams[childSessionID]
	if state == nil {
		state = &childStreamState{}
		m.childStreams[childSessionID] = state
	}
	return state
}

func (m *runTUI) scheduleChildAttachDelay(childSessionID string) tea.Cmd {
	childSessionID = strings.TrimSpace(childSessionID)
	if childSessionID == "" || m.runtimeClient == nil {
		return nil
	}
	m.ensureChildStreamState(childSessionID)
	return tea.Tick(tuiChildAttachInitialDelay, func(time.Time) tea.Msg {
		return tuiChildAttachRetryMsg{childSessionID: childSessionID}
	})
}

func (m *runTUI) startChildWatcher(childSessionID string) tea.Cmd {
	if m.runtimeClient == nil || strings.TrimSpace(childSessionID) == "" {
		return nil
	}
	if state, ok := m.childStreams[childSessionID]; ok {
		if state.done {
			return nil
		}
		if state.stream != nil {
			return m.scheduleChildStreamRecv(childSessionID)
		}
	}
	ctx := m.ctx
	rt := m.runtimeClient
	return func() tea.Msg {
		stream, err := rt.RunSessionInteractive(ctx)
		if err != nil {
			return tuiChildStreamMsg{childSessionID: childSessionID, err: clierr.WrapRPC("attach child session", err)}
		}
		if err := stream.Send(&runtimev1.RunSessionInteractiveClientMsg{
			Body: &runtimev1.RunSessionInteractiveClientMsg_Start{
				Start: &runtimev1.RunSessionInteractiveStart{SessionId: childSessionID},
			},
		}); err != nil {
			_ = stream.CloseSend()
			return tuiChildStreamMsg{childSessionID: childSessionID, err: clierr.WrapRPC("attach child session", err)}
		}
		return tuiChildStreamAttachMsg{childSessionID: childSessionID, stream: stream}
	}
}

type tuiChildStreamAttachMsg struct {
	childSessionID string
	stream         interactiveStream
}

type tuiChildAttachRetryMsg struct {
	childSessionID string
}

func (m *runTUI) attachChildStream(childSessionID string, stream interactiveStream) tea.Cmd {
	state := m.ensureChildStreamState(childSessionID)
	if state.stream != nil && !state.sendClosed {
		_ = state.stream.CloseSend()
	}
	state.stream = stream
	state.recvInFlight = false
	state.done = false
	state.sendClosed = false
	return m.scheduleChildStreamRecv(childSessionID)
}

func (m *runTUI) scheduleChildStreamRecv(childSessionID string) tea.Cmd {
	state := m.childStreams[childSessionID]
	if state == nil || state.stream == nil || state.recvInFlight || state.done || m.quitting {
		return nil
	}
	state.recvInFlight = true
	stream := state.stream
	return func() tea.Msg {
		msg, err := stream.Recv()
		return tuiChildStreamMsg{childSessionID: childSessionID, msg: msg, err: err}
	}
}

func (m *runTUI) closeChildStream(childSessionID string) {
	state := m.childStreams[childSessionID]
	if state == nil || state.sendClosed {
		return
	}
	state.sendClosed = true
	state.done = true
	if state.stream != nil {
		_ = state.stream.CloseSend()
	}
}

func (m *runTUI) handleChildStreamMsg(msg tuiChildStreamMsg) (tea.Cmd, error) {
	childSessionID := msg.childSessionID
	state := m.childStreams[childSessionID]
	if state != nil {
		state.recvInFlight = false
	}
	if msg.err != nil {
		if msg.err == io.EOF {
			m.closeChildStream(childSessionID)
			if entry := m.childEntry(childSessionID); entry != nil && entry.status == "running" {
				entry.status = "done"
			}
			m.layout()
			return nil, nil
		}
		if m.shouldRetryChildAttach(childSessionID, msg.err) {
			return m.scheduleChildAttachRetry(childSessionID), nil
		}
		if entry := m.childEntry(childSessionID); entry != nil {
			entry.status = "failed"
		}
		m.closeChildStream(childSessionID)
		m.layout()
		return nil, nil
	}
	if err := m.handleChildServerMsg(childSessionID, msg.msg); err != nil {
		if entry := m.childEntry(childSessionID); entry != nil {
			entry.status = "failed"
		}
		m.closeChildStream(childSessionID)
		m.layout()
		return nil, nil
	}
	if state != nil && state.done {
		m.layout()
		return nil, nil
	}
	return m.scheduleChildStreamRecv(childSessionID), nil
}

func isChildAttachErrorRetryable(err error) bool {
	if err == nil {
		return false
	}
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.NotFound, codes.Unavailable, codes.FailedPrecondition:
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "unavailable") ||
		strings.Contains(msg, "failedprecondition")
}

func (m *runTUI) shouldRetryChildAttach(childSessionID string, err error) bool {
	if !isChildAttachErrorRetryable(err) {
		return false
	}
	if m.childStreams == nil {
		return true
	}
	state := m.childStreams[childSessionID]
	if state == nil {
		return true
	}
	return state.attachAttempts < tuiChildAttachMaxAttempts
}

func (m *runTUI) scheduleChildAttachRetry(childSessionID string) tea.Cmd {
	state := m.ensureChildStreamState(childSessionID)
	state.attachAttempts++
	if state.stream != nil && !state.sendClosed {
		_ = state.stream.CloseSend()
	}
	state.stream = nil
	state.recvInFlight = false
	state.done = false
	state.sendClosed = false
	return tea.Tick(tuiChildAttachRetryInterval, func(time.Time) tea.Msg {
		return tuiChildAttachRetryMsg{childSessionID: childSessionID}
	})
}

func (m *runTUI) hydrateChildAfterDelegation(callID string) tea.Cmd {
	if m.delegationChildren == nil {
		return nil
	}
	childID := strings.TrimSpace(m.delegationChildren[callID])
	if childID == "" {
		return nil
	}
	state := m.childStreams[childID]
	if state != nil && state.stream != nil && !state.done {
		return nil
	}
	return m.startChildWatcher(childID)
}

func (m *runTUI) childEntry(childSessionID string) *tuiSessionEntry {
	idx, ok := m.sessions.byID[childSessionID]
	if !ok || idx >= len(m.sessions.entries) {
		return nil
	}
	return m.sessions.entries[idx]
}

func (m *runTUI) handleChildServerMsg(childSessionID string, msg *runtimev1.RunSessionInteractiveServerMsg) error {
	entry := m.childEntry(childSessionID)
	if entry == nil {
		return nil
	}
	width := m.messageContentWidth()
	switch {
	case msg.GetSessionStarted() != nil:
		started := msg.GetSessionStarted()
		entry.id = started.GetSessionId()
		if !historyHasUserRole(started.GetHistory()) {
			m.seedChildSessionInput(entry)
		}
		if err := m.appendChildConversationHistory(entry, started.GetHistory()); err != nil {
			return err
		}
		m.layout()
	case msg.GetTextDelta() != nil:
		entry.streaming.WriteString(msg.GetTextDelta().GetDelta())
		m.refreshViewport()
	case msg.GetAwaitingInput() != nil:
		awaiting := msg.GetAwaitingInput()
		meta := turnMeta(awaiting.GetStats(), awaiting.GetStopReason(), 0)
		if _, err := m.flushChildStreamingTurn(entry, meta); err != nil {
			return err
		}
		entry.status = "running"
		m.layout()
	case msg.GetCompleted() != nil:
		completed := msg.GetCompleted()
		meta := turnMeta(completed.GetStats(), completed.GetStopReason(), 0)
		if _, err := m.flushChildStreamingTurn(entry, meta); err != nil {
			return err
		}
		if entry.transcript.Len() == 0 {
			if err := m.appendChildCompletedOutput(entry, completed.GetOutput(), meta); err != nil {
				return err
			}
		}
		entry.status = "done"
		m.closeChildStream(childSessionID)
		m.layout()
	case msg.GetToolCall() != nil:
		tc := msg.GetToolCall()
		if _, err := m.flushChildStreamingTurn(entry, nil); err != nil {
			return err
		}
		if tc.GetAgentDelegation() {
			if entry.delegationCallIDs == nil {
				entry.delegationCallIDs = make(map[string]struct{})
			}
			entry.delegationCallIDs[tc.GetCallId()] = struct{}{}
			entry.transcript.WriteString("\n")
			entry.transcript.WriteString(renderAgentDelegationBlock(width, tc.GetDelegationTarget(), tc.GetCallId(), tc.GetArgs()))
		} else {
			entry.transcript.WriteString("\n")
			entry.transcript.WriteString(renderToolCallBlock(width, tc))
		}
		m.followTail = m.selectedEntry() == entry
		m.layout()
	case msg.GetToolResult() != nil:
		tr := msg.GetToolResult()
		if entry.isDelegationCallID(tr.GetCallId()) {
			m.layout()
			return nil
		}
		entry.transcript.WriteString("\n")
		entry.transcript.WriteString(renderToolResultBlock(width, tr))
		m.followTail = m.selectedEntry() == entry
		m.layout()
	case msg.GetFailed() != nil:
		entry.status = "failed"
		m.closeChildStream(childSessionID)
		m.layout()
	case msg.GetCancelled() != nil:
		entry.status = "cancelled"
		m.closeChildStream(childSessionID)
		m.layout()
	default:
		// Ignore approval and other events on child attach for now.
	}
	return nil
}

func historyHasUserRole(msgs []*runtimev1.InteractiveConversationMessage) bool {
	for _, msg := range msgs {
		if msg.GetRole() == "user" && strings.TrimSpace(msg.GetContent()) != "" {
			return true
		}
	}
	return false
}

func (m *runTUI) seedChildSessionInput(entry *tuiSessionEntry) {
	if entry == nil || entry.sessionInputShown || len(entry.sessionInput) == 0 {
		return
	}
	width := m.messageContentWidth()
	entry.transcript.WriteString("\n")
	if plain := delegationInputPlainText(entry.sessionInput); plain != "" {
		entry.transcript.WriteString(renderUserBlock(width, plain))
	} else {
		entry.transcript.WriteString(renderSubagentSessionInputBlock(width, entry.sessionInput))
	}
	entry.sessionInputShown = true
}

func (m *runTUI) appendChildConversationHistory(entry *tuiSessionEntry, msgs []*runtimev1.InteractiveConversationMessage) error {
	var turn int32
	for _, msg := range msgs {
		role := msg.GetRole()
		if skipInteractiveHistoryRole(role) {
			continue
		}
		content := msg.GetContent()
		switch role {
		case "user":
			if entry.sessionInputShown && delegatedUserHistoryDuplicatesInput(entry.sessionInput, content) {
				continue
			}
			entry.sessionInputShown = true
			entry.transcript.WriteString("\n")
			entry.transcript.WriteString(renderUserBlock(m.messageContentWidth(), content))
		case "assistant":
			turn++
			formatted, err := formatAssistantTranscript(content)
			if err != nil {
				return err
			}
			body := string(formatted)
			if body == "" {
				body = content
			} else if len(content) > 0 && len(body) < len(content) {
				body = content
			}
			meta := turnMeta(statsFromHistoryMessage(msg, turn), msg.GetStopReason(), durationFromHistoryMessage(msg))
			entry.transcript.WriteString("\n")
			entry.transcript.WriteString(renderAgentBlock(m.messageContentWidth(), "AGENT", body, meta))
		default:
			return fmt.Errorf("run session: unknown history role %q", role)
		}
	}
	return nil
}

func (m *runTUI) appendChildCompletedOutput(entry *tuiSessionEntry, raw []byte, meta *turnDisplayMeta) error {
	if len(raw) == 0 {
		return nil
	}
	pretty, err := prettifySessionOutput(raw)
	if err != nil {
		return err
	}
	entry.transcript.WriteString("\n")
	entry.transcript.WriteString(renderAgentBlock(m.messageContentWidth(), "Result", string(pretty), meta))
	return nil
}

func (m *runTUI) flushChildStreamingTurn(entry *tuiSessionEntry, meta *turnDisplayMeta) (bool, error) {
	raw := entry.streaming.String()
	entry.streaming.Reset()
	if strings.TrimSpace(raw) == "" {
		return false, nil
	}
	formatted, err := formatAssistantTranscript(raw)
	if err != nil {
		return false, err
	}
	body := raw
	if len(formatted) > 0 {
		body = string(formatted)
	}
	entry.transcript.WriteString("\n")
	entry.transcript.WriteString(renderAgentBlock(m.messageContentWidth(), "AGENT", body, meta))
	return true, nil
}
