package executor

import (
	"context"
	"os"
	"strconv"
	"time"
)

const defaultDispatchQueueWait = 15 * time.Second

// dispatchQueueContext bounds how long a tool call may wait in the worker queue when the
// parent context has no deadline (for example detached phrony run). When the wait ends
// with no handler, the registry returns ErrNoHandler so policy can escalate or fail fast.
func dispatchQueueContext(parent context.Context) (context.Context, context.CancelFunc) {
	if _, ok := parent.Deadline(); ok {
		return context.WithCancel(parent)
	}
	wait := dispatchQueueWaitFromEnv()
	if wait <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, wait)
}

func dispatchQueueWaitFromEnv() time.Duration {
	raw := os.Getenv("RUNTIME_DISPATCH_QUEUE_WAIT")
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
