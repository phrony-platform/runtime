package model

const (
	// SessionStatusPending means the session is bound to a deployed agent version
	// but model execution has not started.
	SessionStatusPending = "pending"
	// SessionStatusRunning means the executor is actively streaming a completion.
	SessionStatusRunning = "running"
	// SessionStatusAwaitingInput means the agent is waiting for user input on the stream.
	SessionStatusAwaitingInput = "awaiting_input"
	// SessionStatusAwaitingTool means the session is waiting for tool dispatch to complete.
	SessionStatusAwaitingTool = "awaiting_tool"
	// SessionStatusAwaitingApproval means the session is suspended for human tool approval.
	SessionStatusAwaitingApproval = "awaiting_approval"
	// SessionStatusCompleted means the session finished successfully.
	SessionStatusCompleted = "completed"
	// SessionStatusFailed means the session ended with an error.
	SessionStatusFailed = "failed"
	// SessionStatusCancelled means the session was cancelled by an operator.
	SessionStatusCancelled = "cancelled"
)
