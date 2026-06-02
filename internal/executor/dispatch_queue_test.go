package executor

import (
	"context"
	"testing"
	"time"
)

func TestDispatchQueueContext_appliesDefaultWait(t *testing.T) {
	t.Setenv("RUNTIME_DISPATCH_QUEUE_WAIT", "")
	ctx, cancel := dispatchQueueContext(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected deadline")
	}
	remaining := time.Until(deadline)
	if remaining < defaultDispatchQueueWait-time.Second || remaining > defaultDispatchQueueWait+time.Second {
		t.Fatalf("remaining %v, want ~%v", remaining, defaultDispatchQueueWait)
	}
}

func TestDispatchQueueContext_capsLongParentDeadline(t *testing.T) {
	t.Setenv("RUNTIME_DISPATCH_QUEUE_WAIT", "5s")
	parent, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	ctx, cancelDispatch := dispatchQueueContext(parent)
	defer cancelDispatch()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected deadline")
	}
	remaining := time.Until(deadline)
	if remaining > 6*time.Second || remaining < 3*time.Second {
		t.Fatalf("remaining %v, want ~5s (not the parent's 1h deadline)", remaining)
	}
}
