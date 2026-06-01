package policy

import "encoding/json"

// ToolCallContext is the manifest-facing view of a tool invocation before dispatch.
type ToolCallContext struct {
	ToolRef         string
	WireName        string
	Version         string
	Args            json.RawMessage
	SideEffectClass string
	PolicyName      string // from ToolBinding.Policy when set
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
