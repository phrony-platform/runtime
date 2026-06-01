package tooldispatch

import "context"

// DispatchProvenance is recorded when a call is leased to a worker after integrity checks.
type DispatchProvenance struct {
	Call                ToolCall
	Worker              WorkerInfo
	DescriptorHash      string
	ManifestContentHash string
}

// InvocationRecorder persists tool invocation trace rows (optional).
// Methods follow persist-before-act ordering: pending/queued before wait,
// dispatched before invoke, completed before worker ack.
type InvocationRecorder interface {
	RecordPending(ctx context.Context, call ToolCall, status string) error
	RecordQueued(ctx context.Context, call ToolCall) error
	RecordDispatched(ctx context.Context, prov DispatchProvenance) error
	RecordCompleted(ctx context.Context, call ToolCall, res ToolResult, dispatchErr error) error
	RecordIndeterminate(ctx context.Context, call ToolCall, reason string) error
	// LookupCompleted returns a durable result when the call already finished (worker resync).
	LookupCompleted(ctx context.Context, callID string) (ToolResult, bool, error)
}
