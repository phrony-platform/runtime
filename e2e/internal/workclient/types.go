package workclient

import (
	"context"
	"encoding/json"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

// HandlerAdvertisement describes one tool@version the worker can execute.
type HandlerAdvertisement struct {
	Tool            string
	Version         string
	ContractVersion string
	DescriptorHash  string
	MaxConcurrency  int
}

// Handler executes one WorkInvoke from the runtime.
type Handler func(ctx context.Context, call *runtimev1.WorkInvoke) (json.RawMessage, *ToolError)

// ToolError is returned to the runtime as a structured tool failure.
type ToolError struct {
	Code    string
	Message string
}

// Options configures a Work-stream worker connection.
type Options struct {
	WorkerID         string
	WorkloadIdentity string
	ImageDigest      string
	Handlers         []HandlerAdvertisement
	// Dispatch routes invocations by tool ref and version. When nil, unknown tools get {"ok":true}.
	Dispatch func(ctx context.Context, call *runtimev1.WorkInvoke) (json.RawMessage, *ToolError)
}
