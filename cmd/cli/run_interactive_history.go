package main

// skipInteractiveHistoryRole reports roles persisted for model context but not shown in the CLI transcript.
func skipInteractiveHistoryRole(role string) bool {
	switch role {
	case "system", "tool":
		return true
	default:
		return false
	}
}
