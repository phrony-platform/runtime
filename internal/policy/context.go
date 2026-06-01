package policy

import "encoding/json"

// EvalContext carries dispatch-time fields for policy condition trees.
type EvalContext struct {
	// DispatchTrigger is set when evaluating policies after a dispatch failure
	// (for example dispatch:indeterminate). Matches phrony.dispatch.trigger conditions.
	DispatchTrigger string
}

// ToolCallContext is the manifest-facing view of a tool invocation before dispatch.
type ToolCallContext struct {
	ToolRef         string
	WireName        string
	Version         string
	Args            json.RawMessage
	SideEffectClass string
}

// ApprovalRequest describes a pending human decision shown on the interactive stream.
type ApprovalRequest struct {
	ApprovalID string
	CallID     string
	SessionID  string
	Tool       string
	Version    string
	Args       json.RawMessage
	Route      string
	Reason     string
}
