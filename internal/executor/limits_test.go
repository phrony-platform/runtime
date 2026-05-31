package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/phrony-platform/runtime/internal/manifest"
)

func TestLimitTracker_maxTokensPerRun(t *testing.T) {
	max := 5
	tracker := newLimitTracker(&manifest.Limits{MaxTokensPerRun: &max, OnLimit: "halt"})
	if err := tracker.beginIteration(); err != nil {
		t.Fatalf("beginIteration: %v", err)
	}
	if err := tracker.addTokens(strings.Repeat("a", 24)); err == nil {
		t.Fatal("addTokens() = nil, want limit error")
	} else {
		var lim *LimitError
		if !errors.As(err, &lim) || lim.Kind != LimitMaxTokensPerRun {
			t.Fatalf("err = %v", err)
		}
	}
}

func TestLimitTracker_maxLoopIterations(t *testing.T) {
	max := 1
	tracker := newLimitTracker(&manifest.Limits{MaxLoopIterations: &max})
	if err := tracker.beginIteration(); err != nil {
		t.Fatalf("first iteration: %v", err)
	}
	if err := tracker.beginIteration(); err == nil {
		t.Fatal("second iteration: want limit error")
	}
}

func TestIsLimitError(t *testing.T) {
	if IsLimitError(nil) {
		t.Fatal("nil error should not be a limit error")
	}
	if IsLimitError(errors.New("model unavailable")) {
		t.Fatal("generic error should not be a limit error")
	}
	lim := &LimitError{Kind: LimitMaxTokensPerRun, OnLimit: "halt"}
	if !IsLimitError(lim) {
		t.Fatal("LimitError should be detected")
	}
	wrapped := fmt.Errorf("turn failed: %w", lim)
	if !IsLimitError(wrapped) {
		t.Fatal("wrapped LimitError should be detected")
	}
}

func TestIsLimitErrorMessage(t *testing.T) {
	if !IsLimitErrorMessage("run limit max_tokens_per_run exceeded (on_limit=halt)") {
		t.Fatal("expected limit session error message")
	}
	if IsLimitErrorMessage("model unavailable") {
		t.Fatal("unexpected limit match for generic error")
	}
}

func TestLimitError_Error(t *testing.T) {
	var nilErr *LimitError
	if nilErr.Error() != "run limit exceeded" {
		t.Fatalf("nil LimitError = %q", nilErr.Error())
	}
	err := &LimitError{Kind: LimitMaxTokensPerRun, OnLimit: "halt"}
	if !strings.Contains(err.Error(), "max_tokens_per_run") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestLimitTracker_maxWallClockSeconds(t *testing.T) {
	max := 0
	tracker := newLimitTracker(&manifest.Limits{MaxWallClockSeconds: &max})
	tracker.startedAt = time.Now().Add(-2 * time.Second)
	max = 1
	tracker.limits.MaxWallClockSeconds = &max
	if err := tracker.checkWallClock(); err == nil {
		t.Fatal("checkWallClock() = nil, want limit error")
	}
}

func TestLimitTracker_contextTimeout(t *testing.T) {
	max := 1
	tracker := newLimitTracker(&manifest.Limits{MaxWallClockSeconds: &max})
	ctx, cancel := tracker.context(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected deadline on context")
	}
	if time.Until(deadline) > time.Second {
		t.Fatalf("deadline too far: %v", deadline)
	}
}

func TestEstimateTokens(t *testing.T) {
	if estimateTokens("abcd") != 1 {
		t.Fatalf("estimateTokens(abcd) = %d, want 1", estimateTokens("abcd"))
	}
	if estimateTokens("") != 0 {
		t.Fatalf("estimateTokens empty = %d, want 0", estimateTokens(""))
	}
}
