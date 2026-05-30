package executor

import (
	"errors"
	"strings"
	"testing"

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

func TestEstimateTokens(t *testing.T) {
	if estimateTokens("abcd") != 1 {
		t.Fatalf("estimateTokens(abcd) = %d, want 1", estimateTokens("abcd"))
	}
	if estimateTokens("") != 0 {
		t.Fatalf("estimateTokens empty = %d, want 0", estimateTokens(""))
	}
}
