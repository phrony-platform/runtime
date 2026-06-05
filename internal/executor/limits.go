package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/phrony-platform/runtime/internal/manifest"
)

const (
	OnLimitHalt     = "halt"
	OnLimitEscalate = "escalate"
	defaultOnLimit  = OnLimitHalt
)

const limitErrorPrefix = "run limit "

// IsLimitError reports whether err is a manifest run limit error.
func IsLimitError(err error) bool {
	var lim *LimitError
	return errors.As(err, &lim)
}

// IsLimitErrorMessage reports whether msg is a persisted LimitError string (sessions store errors as text).
func IsLimitErrorMessage(msg string) bool {
	msg = strings.TrimSpace(msg)
	return strings.HasPrefix(msg, limitErrorPrefix) && strings.Contains(msg, " exceeded")
}

// LimitKind identifies which run limit was exceeded.
type LimitKind string

const (
	LimitMaxTokensPerRun     LimitKind = "max_tokens_per_run"
	LimitMaxLoopIterations   LimitKind = "max_loop_iterations"
	LimitMaxWallClockSeconds LimitKind = "max_wall_clock_seconds"
	LimitMaxHITLWaitMinutes  LimitKind = "max_hitl_wait_minutes"
)

// LimitError is returned when a manifest run limit is hit.
type LimitError struct {
	Kind    LimitKind
	OnLimit string
}

// EscalationError signals the run should suspend for human review (HITL) rather
// than fail when spec.limits.on_limit is escalate.
type EscalationError struct {
	Limit *LimitError
}

func (e *EscalationError) Error() string {
	if e == nil || e.Limit == nil {
		return "run limit exceeded; human review required"
	}
	return e.Limit.Error() + "; human review required"
}

// IsEscalationError reports whether err requests HITL suspension for a limit.
func IsEscalationError(err error) bool {
	var esc *EscalationError
	return errors.As(err, &esc)
}

func (e *LimitError) Error() string {
	if e == nil {
		return "run limit exceeded"
	}
	on := e.OnLimit
	if on == "" {
		on = defaultOnLimit
	}
	return fmt.Sprintf("run limit %s exceeded (on_limit=%s)", e.Kind, on)
}

type limitTracker struct {
	limits          *manifest.Limits
	tokensUsed      int
	iteration       int
	startedAt       time.Time
	onLimit         string
	wallClockPaused bool
	pauseStartedAt  time.Time
	totalWallPaused time.Duration
	hitlWaitAccum   time.Duration
}

func newLimitTracker(limits *manifest.Limits) *limitTracker {
	on := defaultOnLimit
	if limits != nil && limits.OnLimit != "" {
		on = limits.OnLimit
	}
	return &limitTracker{
		limits:    limits,
		startedAt: time.Now(),
		onLimit:   on,
	}
}

func (t *limitTracker) beginIteration() error {
	if t == nil {
		return nil
	}
	t.iteration++
	if t.limits == nil || t.limits.MaxLoopIterations == nil {
		return nil
	}
	max := *t.limits.MaxLoopIterations
	if max > 0 && t.iteration > max {
		return &LimitError{Kind: LimitMaxLoopIterations, OnLimit: t.onLimit}
	}
	return nil
}

func (t *limitTracker) addTokens(text string) error {
	if t == nil || t.limits == nil || t.limits.MaxTokensPerRun == nil {
		return nil
	}
	t.tokensUsed += estimateTokens(text)
	max := *t.limits.MaxTokensPerRun
	if max > 0 && t.tokensUsed > max {
		return &LimitError{Kind: LimitMaxTokensPerRun, OnLimit: t.onLimit}
	}
	return nil
}

// addUsageTokens charges externally reported token usage (e.g. a delegated agent
// run returned via a tool result) against the run token budget. It lets
// cross-tree subagent work count toward max_tokens_per_run.
func (t *limitTracker) addUsageTokens(n int) error {
	if t == nil || t.limits == nil || t.limits.MaxTokensPerRun == nil || n <= 0 {
		return nil
	}
	t.tokensUsed += n
	max := *t.limits.MaxTokensPerRun
	if max > 0 && t.tokensUsed > max {
		return &LimitError{Kind: LimitMaxTokensPerRun, OnLimit: t.onLimit}
	}
	return nil
}

func (t *limitTracker) checkWallClock() error {
	if t == nil || t.limits == nil || t.limits.MaxWallClockSeconds == nil {
		return nil
	}
	max := *t.limits.MaxWallClockSeconds
	if max <= 0 {
		return nil
	}
	if t.effectiveWallClockElapsed() > time.Duration(max)*time.Second {
		return &LimitError{Kind: LimitMaxWallClockSeconds, OnLimit: t.onLimit}
	}
	return nil
}

func (t *limitTracker) beginHITLWait() {
	if t == nil {
		return
	}
	if !t.wallClockPaused {
		t.wallClockPaused = true
		t.pauseStartedAt = time.Now()
	}
}

func (t *limitTracker) endHITLWait() error {
	if t == nil {
		return nil
	}
	if t.wallClockPaused {
		waited := time.Since(t.pauseStartedAt)
		t.totalWallPaused += waited
		t.hitlWaitAccum += waited
		t.wallClockPaused = false
	}
	if t.limits == nil || t.limits.MaxHITLWaitMinutes == nil {
		return nil
	}
	max := *t.limits.MaxHITLWaitMinutes
	if max <= 0 {
		return nil
	}
	if t.hitlWaitAccum > time.Duration(max)*time.Minute {
		return &LimitError{Kind: LimitMaxHITLWaitMinutes, OnLimit: t.onLimit}
	}
	return nil
}

func (t *limitTracker) effectiveWallClockElapsed() time.Duration {
	if t == nil {
		return 0
	}
	elapsed := time.Since(t.startedAt) - t.totalWallPaused
	if t.wallClockPaused {
		elapsed -= time.Since(t.pauseStartedAt)
	}
	if elapsed < 0 {
		return 0
	}
	return elapsed
}

func (t *limitTracker) deadline(ctx context.Context) time.Time {
	if t == nil {
		if d, ok := ctx.Deadline(); ok {
			return d
		}
		return time.Time{}
	}
	if d, ok := ctx.Deadline(); ok {
		return d
	}
	if t.limits == nil || t.limits.MaxWallClockSeconds == nil {
		return time.Time{}
	}
	max := *t.limits.MaxWallClockSeconds
	if max <= 0 {
		return time.Time{}
	}
	remaining := time.Duration(max)*time.Second - t.effectiveWallClockElapsed()
	if remaining <= 0 {
		return time.Now()
	}
	return time.Now().Add(remaining)
}

func (t *limitTracker) context(ctx context.Context) (context.Context, context.CancelFunc) {
	if t == nil || t.limits == nil || t.limits.MaxWallClockSeconds == nil {
		return ctx, func() {}
	}
	max := *t.limits.MaxWallClockSeconds
	if max <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, time.Duration(max)*time.Second)
}

func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	n := utf8.RuneCountInString(text)
	return (n + 3) / 4
}
