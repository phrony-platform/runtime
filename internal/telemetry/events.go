package telemetry

// Whitelisted event names (server enforces the same set).
const (
	EventDaemonStarted     = "daemon_started"
	EventSessionStarted    = "session_started"
	EventSessionCompleted  = "session_completed"
	EventSessionFailed     = "session_failed"
	EventAgentDeployed     = "agent_deployed"
	EventToolDispatched    = "tool_dispatched"
	EventMigrateRun        = "migrate_run"
	EventCLICommand        = "cli_command"
)

var allowedEvents = map[string]bool{
	EventDaemonStarted:    true,
	EventSessionStarted:   true,
	EventSessionCompleted: true,
	EventSessionFailed:    true,
	EventAgentDeployed:    true,
	EventToolDispatched:   true,
	EventMigrateRun:       true,
	EventCLICommand:       true,
}

func isAllowedEvent(name string) bool {
	return allowedEvents[name]
}
