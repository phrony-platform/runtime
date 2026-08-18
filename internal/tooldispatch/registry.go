package tooldispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/phrony-platform/runtime/internal/telemetry"
)

// WorkSend delivers a server message to a connected worker stream.
type WorkSend func(msg any) error

// IntegrityCheck validates a worker before dispatch. When nil, all registered workers pass.
type IntegrityCheck func(call ToolCall, worker *WorkerInfo) error

// WorkerInfo is the registered identity of a connected worker.
type WorkerInfo struct {
	WorkerID          string
	WorkloadIdentity  string
	ImageDigest       string
	ContractVersions  map[string]string // tool@version -> contract version
}

// WorkerRegistry routes tool calls to connected workers with leases, capacity, and queues.
type WorkerRegistry struct {
	cfg RegistryConfig
	mu  sync.Mutex

	shuttingDown bool

	workers map[string]*workerConn
	// tool@version -> workers that advertise the handler (stable order for fairness).
	toolIndex map[string][]*workerConn

	queues map[string]*toolQueue

	integrity  IntegrityCheck
	recorder   InvocationRecorder

	// callID -> in-flight dispatch state (leased or awaiting worker result ack).
	inflight map[string]*dispatchWaiter

	// sessionID -> call IDs for cancellation.
	bySession map[string]map[string]struct{}
}

// NewWorkerRegistry constructs an empty registry.
func NewWorkerRegistry(cfg RegistryConfig) *WorkerRegistry {
	return &WorkerRegistry{
		cfg:       cfg,
		workers:   make(map[string]*workerConn),
		toolIndex: make(map[string][]*workerConn),
		queues:    make(map[string]*toolQueue),
		inflight:  make(map[string]*dispatchWaiter),
		bySession: make(map[string]map[string]struct{}),
		integrity: cfg.IntegrityCheck,
	}
}

// SetIntegrityCheck replaces the allowlist hook (tests).
func (r *WorkerRegistry) SetIntegrityCheck(fn IntegrityCheck) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.integrity = fn
}

// SetInvocationRecorder attaches durable trace persistence (optional).
func (r *WorkerRegistry) SetInvocationRecorder(rec InvocationRecorder) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recorder = rec
}

type workerConn struct {
	info     WorkerInfo
	handlers map[string]handlerState // tool@version
	send     WorkSend
	lease    time.Time
	closed   chan struct{}
}

type handlerState struct {
	tool, version, contractVersion, descriptorHash string
	maxConcurrency                                   int
	busy                                             int
}

type dispatchWaiter struct {
	call       ToolCall
	ctx        context.Context
	resultCh   chan dispatchOutcome
	workerID   string
	queued     bool
	dispatched bool
}

type dispatchOutcome struct {
	res ToolResult
	err error
}

type toolQueue struct {
	items []*dispatchWaiter
}

// Shutdown drops all worker connections and fails queued or in-flight dispatches.
// Idempotent; safe to call while gRPC is stopping so Work streams unblock.
func (r *WorkerRegistry) Shutdown() {
	r.mu.Lock()
	if r.shuttingDown {
		r.mu.Unlock()
		return
	}
	r.shuttingDown = true

	for key, q := range r.queues {
		for _, dw := range q.items {
			r.finishWaiterLocked(dw.call.CallID, dw, ToolResult{}, context.Canceled)
		}
		delete(r.queues, key)
	}

	workers := make([]*workerConn, 0, len(r.workers))
	for _, w := range r.workers {
		workers = append(workers, w)
	}
	for _, w := range workers {
		r.removeWorkerLocked(w)
	}
	r.mu.Unlock()
}

// RegisterWorker attaches a worker stream. send delivers WorkServerMsg values.
// inFlight lists call IDs the worker is still executing or holding results for.
func (r *WorkerRegistry) RegisterWorker(
	workerID, workloadIdentity, imageDigest string,
	handlers []HandlerAdvertisement,
	inFlight []string,
	send WorkSend,
) (*WorkerInfo, error) {
	if workerID == "" {
		return nil, fmt.Errorf("worker_id is required")
	}
	if send == nil {
		return nil, fmt.Errorf("send callback is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.shuttingDown {
		return nil, fmt.Errorf("tool worker registry is shutting down")
	}

	if old, ok := r.workers[workerID]; ok {
		r.removeWorkerLocked(old)
	}

	w := &workerConn{
		info: WorkerInfo{
			WorkerID:         workerID,
			WorkloadIdentity: workloadIdentity,
			ImageDigest:      imageDigest,
			ContractVersions: make(map[string]string),
		},
		handlers: make(map[string]handlerState),
		send:     send,
		lease:    time.Now().Add(r.cfg.leaseTTL()),
		closed:   make(chan struct{}),
	}
	for _, h := range handlers {
		key := ToolKey(h.Tool, h.Version)
		max := h.MaxConcurrency
		if max <= 0 {
			max = 1
		}
		w.handlers[key] = handlerState{
			tool:             h.Tool,
			version:          h.Version,
			contractVersion:  h.ContractVersion,
			descriptorHash:   h.DescriptorHash,
			maxConcurrency:   max,
		}
		w.info.ContractVersions[key] = h.ContractVersion
		r.indexWorkerLocked(key, w)
	}
	r.workers[workerID] = w

	for _, callID := range inFlight {
		if dw, ok := r.inflight[callID]; ok {
			dw.workerID = workerID
			continue
		}
		if rec := r.recorder; rec != nil {
			if res, ok, err := rec.LookupCompleted(context.Background(), callID); err == nil && ok {
				_ = w.send(resultAckMsg(callID))
				_ = res
			}
		}
	}

	r.drainQueuesLocked()
	return &w.info, nil
}

// HandlerAdvertisement is one tool handler a worker exposes at registration.
type HandlerAdvertisement struct {
	Tool             string
	Version          string
	ContractVersion  string
	DescriptorHash   string
	MaxConcurrency   int
}

func (r *WorkerRegistry) indexWorkerLocked(key string, w *workerConn) {
	for _, existing := range r.toolIndex[key] {
		if existing == w {
			return
		}
	}
	r.toolIndex[key] = append(r.toolIndex[key], w)
}

func (r *WorkerRegistry) removeWorkerLocked(w *workerConn) {
	delete(r.workers, w.info.WorkerID)
	close(w.closed)
	for key := range w.handlers {
		list := r.toolIndex[key]
		filtered := list[:0]
		for _, c := range list {
			if c != w {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) == 0 {
			delete(r.toolIndex, key)
		} else {
			r.toolIndex[key] = filtered
		}
	}
	// Outstanding leases become indeterminate when the worker drops.
	rec := r.recorder
	for callID, dw := range r.inflight {
		if dw.workerID == w.info.WorkerID && !dw.queued {
			if rec != nil && dw.dispatched {
				_ = rec.RecordIndeterminate(dw.ctx, dw.call, ErrIndeterminate.Error())
			}
			r.signalWaiterLocked(callID, dw, ToolResult{}, ErrIndeterminate)
		}
	}
}

// UnregisterWorker removes a worker (stream closed).
func (r *WorkerRegistry) UnregisterWorker(workerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if w, ok := r.workers[workerID]; ok {
		r.removeWorkerLocked(w)
	}
}

// Heartbeat extends the worker lease.
func (r *WorkerRegistry) Heartbeat(workerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.workers[workerID]
	if !ok {
		return fmt.Errorf("unknown worker %q", workerID)
	}
	w.lease = time.Now().Add(r.cfg.leaseTTL())
	return nil
}

// Dispatch routes a tool call to an idle worker or enqueues until capacity is available.
func (r *WorkerRegistry) Dispatch(ctx context.Context, call ToolCall) (ToolResult, error) {
	if call.CallID == "" {
		return ToolResult{}, fmt.Errorf("call_id is required")
	}
	key := call.ToolKey()

	r.mu.Lock()
	if r.shuttingDown {
		r.mu.Unlock()
		return ToolResult{}, context.Canceled
	}
	r.mu.Unlock()

	dw := &dispatchWaiter{
		call:     call,
		ctx:      ctx,
		resultCh: make(chan dispatchOutcome, 1),
	}

	r.mu.Lock()
	if existing, ok := r.inflight[call.CallID]; ok {
		ch := existing.resultCh
		r.mu.Unlock()
		select {
		case out := <-ch:
			return out.res, out.err
		case <-ctx.Done():
			return ToolResult{}, ctx.Err()
		}
	}
	if rec := r.recorder; rec != nil {
		if stored, ok, err := rec.LookupCompleted(ctx, call.CallID); err != nil {
			r.mu.Unlock()
			return ToolResult{}, err
		} else if ok {
			r.mu.Unlock()
			return stored, nil
		}
	}
	if err := ctx.Err(); err != nil {
		// Do not track or invoke; classify like a queue wait give-up so callers
		// still see ErrNoHandler / ErrCapacityExhausted when the wait ends with
		// no (or busy) workers.
		mapped := r.queuedWaitGiveUpErrLocked(key, err)
		r.mu.Unlock()
		return ToolResult{}, mapped
	}
	r.trackSessionLocked(call)
	r.inflight[call.CallID] = dw
	telemetry.Track(telemetry.EventToolDispatched)
	rec := r.recorder
	if rec != nil {
		if err := rec.RecordPending(ctx, call, ""); err != nil {
			delete(r.inflight, call.CallID)
			r.untrackSessionLocked(call.SessionID, call.CallID)
			r.mu.Unlock()
			return ToolResult{}, fmt.Errorf("record tool invocation: %w", err)
		}
	}

	if err := r.tryDispatchOrEnqueueLocked(dw, key); err != nil {
		if rec != nil {
			_ = rec.RecordCompleted(ctx, call, ToolResult{}, err)
		}
		delete(r.inflight, call.CallID)
		r.untrackSessionLocked(call.SessionID, call.CallID)
		r.mu.Unlock()
		return ToolResult{}, err
	}
	r.mu.Unlock()

	select {
	case out := <-dw.resultCh:
		return out.res, out.err
	case <-ctx.Done():
		cause := ctx.Err()
		var mapped error
		r.mu.Lock()
		if dw2, ok := r.inflight[call.CallID]; ok && dw2.queued {
			mapped = r.queuedWaitGiveUpErrLocked(key, cause)
		} else {
			mapped = cause
		}
		r.mu.Unlock()
		r.cancelCall(call.CallID, cause)
		return ToolResult{}, mapped
	}
}

func (r *WorkerRegistry) trackSessionLocked(call ToolCall) {
	if call.SessionID == "" {
		return
	}
	set, ok := r.bySession[call.SessionID]
	if !ok {
		set = make(map[string]struct{})
		r.bySession[call.SessionID] = set
	}
	set[call.CallID] = struct{}{}
}

func (r *WorkerRegistry) untrackSessionLocked(sessionID, callID string) {
	set, ok := r.bySession[sessionID]
	if !ok {
		return
	}
	delete(set, callID)
	if len(set) == 0 {
		delete(r.bySession, sessionID)
	}
}

func (r *WorkerRegistry) tryDispatchOrEnqueueLocked(dw *dispatchWaiter, key string) error {
	if w := r.pickWorkerLocked(key); w != nil {
		return r.leaseAndInvokeLocked(dw, w, key)
	}
	q := r.queues[key]
	if q == nil {
		q = &toolQueue{}
		r.queues[key] = q
	}
	if len(q.items) >= r.cfg.maxQueuePerTool() {
		return ErrQueueFull
	}
	dw.queued = true
	q.items = append(q.items, dw)
	if rec := r.recorder; rec != nil {
		_ = rec.RecordQueued(dw.ctx, dw.call)
	}
	return nil
}

func (r *WorkerRegistry) pickWorkerLocked(key string) *workerConn {
	for _, w := range r.toolIndex[key] {
		if time.Now().After(w.lease) {
			continue
		}
		hs, ok := w.handlers[key]
		if !ok {
			continue
		}
		if hs.busy < hs.maxConcurrency {
			return w
		}
	}
	return nil
}

func (r *WorkerRegistry) leaseAndInvokeLocked(dw *dispatchWaiter, w *workerConn, key string) error {
	if err := dw.ctx.Err(); err != nil {
		return err
	}
	hs := w.handlers[key]
	if r.integrity != nil {
		if err := r.integrity(dw.call, &w.info); err != nil {
			return err
		}
	}
	rec := r.recorder
	if rec != nil {
		if err := rec.RecordDispatched(dw.ctx, DispatchProvenance{
			Call:           dw.call,
			Worker:         w.info,
			DescriptorHash: hs.descriptorHash,
		}); err != nil {
			return fmt.Errorf("record tool dispatch: %w", err)
		}
		dw.dispatched = true
	}
	hs.busy++
	w.handlers[key] = hs
	dw.workerID = w.info.WorkerID
	dw.queued = false

	if err := dw.ctx.Err(); err != nil {
		hs.busy--
		w.handlers[key] = hs
		if dw.dispatched && rec != nil {
			_ = rec.RecordCompleted(dw.ctx, dw.call, ToolResult{}, err)
			dw.dispatched = false
		}
		return err
	}
	if err := w.send(invokeMsg(dw.call)); err != nil {
		hs.busy--
		w.handlers[key] = hs
		sendErr := fmt.Errorf("send invoke: %w", err)
		if dw.dispatched && rec != nil {
			_ = rec.RecordCompleted(dw.ctx, dw.call, ToolResult{}, sendErr)
			dw.dispatched = false
		}
		return sendErr
	}
	return nil
}

func (r *WorkerRegistry) drainQueuesLocked() {
	for key, q := range r.queues {
		remaining := q.items[:0]
		for _, dw := range q.items {
			if w := r.pickWorkerLocked(key); w != nil {
				if err := r.leaseAndInvokeLocked(dw, w, key); err != nil {
					r.finishWaiterLocked(dw.call.CallID, dw, ToolResult{}, err)
				}
			} else {
				remaining = append(remaining, dw)
			}
		}
		if len(remaining) == 0 {
			delete(r.queues, key)
		} else {
			q.items = remaining
		}
	}
}

func (r *WorkerRegistry) releaseSlotLocked(workerID, key string) {
	w, ok := r.workers[workerID]
	if !ok {
		return
	}
	hs, ok := w.handlers[key]
	if !ok {
		return
	}
	if hs.busy > 0 {
		hs.busy--
	}
	w.handlers[key] = hs
	r.drainQueuesLocked()
}

// CompleteResult records a worker result and acks the worker after durable persistence.
func (r *WorkerRegistry) CompleteResult(workerID string, res ToolResult) error {
	r.mu.Lock()
	dw, ok := r.inflight[res.CallID]
	if !ok {
		rec := r.recorder
		r.mu.Unlock()
		if rec != nil {
			if stored, found, err := rec.LookupCompleted(context.Background(), res.CallID); err != nil {
				return err
			} else if found {
				_ = stored
				w := r.workerSend(workerID)
				if w != nil {
					return w(resultAckMsg(res.CallID))
				}
				return nil
			}
		}
		return fmt.Errorf("unknown call_id %q", res.CallID)
	}
	if dw.workerID != "" && dw.workerID != workerID {
		r.mu.Unlock()
		return fmt.Errorf("call_id %q owned by worker %q, not %q", res.CallID, dw.workerID, workerID)
	}
	key := dw.call.ToolKey()
	rec := r.recorder
	call := dw.call
	dispatched := dw.dispatched
	r.mu.Unlock()

	if rec != nil && dispatched {
		if err := rec.RecordCompleted(context.Background(), call, res, nil); err != nil {
			return fmt.Errorf("record tool result: %w", err)
		}
	}

	r.mu.Lock()
	dw, ok = r.inflight[res.CallID]
	if !ok {
		r.mu.Unlock()
		w := r.workerSend(workerID)
		if w != nil {
			return w(resultAckMsg(res.CallID))
		}
		return nil
	}
	r.signalWaiterLocked(res.CallID, dw, res, nil)
	r.releaseSlotLocked(workerID, key)
	w, ok := r.workers[workerID]
	r.mu.Unlock()
	if !ok {
		return nil
	}
	return w.send(resultAckMsg(res.CallID))
}

func (r *WorkerRegistry) workerSend(workerID string) WorkSend {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.workers[workerID]
	if !ok {
		return nil
	}
	return w.send
}

// CompleteNack records a worker-side refusal to execute a call.
func (r *WorkerRegistry) CompleteNack(workerID, callID, code, message string) error {
	r.mu.Lock()
	dw, ok := r.inflight[callID]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("unknown call_id %q", callID)
	}
	key := dw.call.ToolKey()
	toolErr := &ToolError{Code: code, Message: message}
	res := ToolResult{CallID: callID, Err: toolErr}
	call := dw.call
	dispatched := dw.dispatched
	r.mu.Unlock()

	if rec := r.recorder; rec != nil && dispatched {
		if err := rec.RecordCompleted(context.Background(), call, res, nil); err != nil {
			return err
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	dw, ok = r.inflight[callID]
	if !ok {
		return nil
	}
	r.signalWaiterLocked(callID, dw, res, nil)
	r.releaseSlotLocked(workerID, key)
	return nil
}

func (r *WorkerRegistry) signalWaiterLocked(callID string, dw *dispatchWaiter, res ToolResult, err error) {
	delete(r.inflight, callID)
	r.untrackSessionLocked(dw.call.SessionID, callID)
	if res.CallID == "" {
		res.CallID = callID
	}
	select {
	case dw.resultCh <- dispatchOutcome{res: res, err: err}:
	default:
	}
}

func (r *WorkerRegistry) finishWaiterLocked(callID string, dw *dispatchWaiter, res ToolResult, err error) {
	rec := r.recorder
	if rec != nil {
		if err != nil {
			_ = rec.RecordCompleted(dw.ctx, dw.call, res, err)
		} else if dw.dispatched {
			_ = rec.RecordCompleted(dw.ctx, dw.call, res, nil)
		}
	}
	r.signalWaiterLocked(callID, dw, res, err)
}

// CancelSession aborts queued and in-flight tool calls for a session.
func (r *WorkerRegistry) CancelSession(sessionID string) {
	r.mu.Lock()
	callIDs := make([]string, 0)
	if set, ok := r.bySession[sessionID]; ok {
		for id := range set {
			callIDs = append(callIDs, id)
		}
	}
	r.mu.Unlock()

	for _, id := range callIDs {
		r.cancelCall(id, context.Canceled)
	}
}

func (r *WorkerRegistry) cancelCall(callID string, cause error) {
	r.mu.Lock()
	dw, ok := r.inflight[callID]
	if !ok {
		r.mu.Unlock()
		return
	}

	if dw.queued {
		key := dw.call.ToolKey()
		q := r.queues[key]
		if q != nil {
			filtered := q.items[:0]
			for _, item := range q.items {
				if item != dw {
					filtered = append(filtered, item)
				}
			}
			if len(filtered) == 0 {
				delete(r.queues, key)
			} else {
				q.items = filtered
			}
		}
		err := r.queuedWaitGiveUpErrLocked(key, cause)
		r.finishWaiterLocked(callID, dw, ToolResult{}, err)
		r.mu.Unlock()
		return
	}

	workerID := dw.workerID
	key := dw.call.ToolKey()
	r.finishWaiterLocked(callID, dw, ToolResult{}, cause)
	r.releaseSlotLocked(workerID, key)
	w := r.workers[workerID]
	r.mu.Unlock()

	if w != nil {
		_ = w.send(cancelMsg(callID))
	}
}

func (r *WorkerRegistry) queuedWaitGiveUpErrLocked(key string, cause error) error {
	if len(r.toolIndex[key]) == 0 {
		return errors.Join(ErrNoHandler, cause)
	}
	return errors.Join(ErrCapacityExhausted, cause)
}

// invokeMsg and related helpers are implemented in work_proto.go.

// LeaseTTL returns the configured worker lease duration.
func (r *WorkerRegistry) LeaseTTL() time.Duration {
	return r.cfg.leaseTTL()
}

// WaitUntilIdle blocks until the registry has no in-flight or queued calls (tests).
func (r *WorkerRegistry) WaitUntilIdle(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		idle := len(r.inflight) == 0
		for _, q := range r.queues {
			if len(q.items) > 0 {
				idle = false
				break
			}
		}
		r.mu.Unlock()
		if idle {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// QueuedCount returns the number of calls waiting for capacity (tests).
func (r *WorkerRegistry) QueuedCount(tool, version string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	q := r.queues[ToolKey(tool, version)]
	if q == nil {
		return 0
	}
	return len(q.items)
}

// WorkerCount returns connected workers advertising a tool (tests).
func (r *WorkerRegistry) WorkerCount(tool, version string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.toolIndex[ToolKey(tool, version)])
}

// MarshalJSON for ToolCall args in tests/logging.
func cloneArgs(args json.RawMessage) json.RawMessage {
	if len(args) == 0 {
		return json.RawMessage("{}")
	}
	out := make(json.RawMessage, len(args))
	copy(out, args)
	return out
}
