package model

// SessionEventType identifies an entry in the ordered session audit log
// (session_events). The log is the displayed timeline for attach replay.
type SessionEventType string

const (
	SessionEventUserMessage      SessionEventType = "user_message"
	SessionEventAssistantMessage SessionEventType = "assistant_message"
	SessionEventToolCall         SessionEventType = "tool_call"
	SessionEventToolResult       SessionEventType = "tool_result"
	SessionEventApprovalRequired SessionEventType = "approval_required"
	SessionEventApprovalDecided  SessionEventType = "approval_decided"
	SessionEventPolicyDenied     SessionEventType = "policy_denied"
	SessionEventSessionCompleted SessionEventType = "session_completed"
	SessionEventSessionFailed    SessionEventType = "session_failed"
	SessionEventSessionCancelled SessionEventType = "session_cancelled"
)
