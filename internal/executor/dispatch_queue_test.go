package executor

import (
	"context"
	"testing"
	"time"
)

func TestDispatchQueueContext_parentDeadlinePassthrough(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	ctx, cancelDispatch := dispatchQueueContext(parent)
	defer cancelDispatch()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected deadline on child context")
	}
	parentDeadline, _ := parent.Deadline()
	if !deadline.Equal(parentDeadline) {
		t.Fatalf("child deadline %v, want %v", deadline, parentDeadline)
	}
}

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
