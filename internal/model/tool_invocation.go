package model

// Tool invocation ledger statuses (durable dispatch trace).
const (
	ToolInvocationPending          = "pending"
	ToolInvocationQueued           = "queued"
	ToolInvocationAwaitingApproval = "awaiting_approval"
	ToolInvocationDispatched       = "dispatched"
	ToolInvocationSucceeded        = "succeeded"
	ToolInvocationFailed           = "failed"
	ToolInvocationIndeterminate    = "indeterminate"
)
