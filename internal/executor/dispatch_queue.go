package executor

import (
	"context"
	"os"
	"strconv"
	"time"
)

const (
	envDispatchQueueWait     = "RUNTIME_DISPATCH_QUEUE_WAIT"
	defaultDispatchQueueWait = 10 * time.Second
)

// dispatchQueueContext bounds how long a tool call may wait in the worker queue.
// Always applied (even when the session has a long wall-clock deadline) so detached runs
// do not park in awaiting_tool until the session limit. When the wait ends with no
// handler registered, the registry returns ErrNoHandler for policy escalation.
func dispatchQueueContext(parent context.Context) (context.Context, context.CancelFunc) {
	wait := dispatchQueueWaitFromEnv()
	if wait <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, wait)
}

// dispatchQueueWaitFromEnv reads RUNTIME_DISPATCH_QUEUE_WAIT.
// Empty or invalid values use defaultDispatchQueueWait (10s).
// A positive integer is seconds; otherwise a Go duration (for example 10s, 500ms).
func dispatchQueueWaitFromEnv() time.Duration {
	raw := os.Getenv(envDispatchQueueWait)
	if raw == "" {
		return defaultDispatchQueueWait
	}
	if sec, err := strconv.Atoi(raw); err == nil && sec > 0 {
		return time.Duration(sec) * time.Second
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return d
	}
	return defaultDispatchQueueWait
}
