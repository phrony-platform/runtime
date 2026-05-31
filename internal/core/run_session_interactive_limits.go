package core

import (
	"context"
	"errors"
	"time"

	"github.com/phrony-platform/runtime/internal/executor"
	"github.com/phrony-platform/runtime/internal/manifest"
)

func limitErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	var lim *executor.LimitError
	if errors.As(err, &lim) {
		return lim.Error()
	}
	return err.Error()
}

// sessionLimitErrorBeforeTurn checks limits before starting another model completion.
func (st *interactiveSessionState) sessionLimitErrorBeforeTurn() error {
	if err := st.sessionTokenLimitError(); err != nil {
		return err
	}
	if err := st.sessionLoopIterationLimitError(); err != nil {
		return err
	}
	if err := st.sessionWallClockLimitError(); err != nil {
		return err
	}
	return nil
}

// sessionLimitErrorAfterTurn checks limits that can be exceeded by a completion that just finished.
func (st *interactiveSessionState) sessionLimitErrorAfterTurn() error {
	if err := st.sessionTokenLimitError(); err != nil {
		return err
	}
	if err := st.sessionWallClockLimitError(); err != nil {
		return err
	}
	return nil
}

func (st *interactiveSessionState) limits() *manifest.Limits {
	if st.version == nil || st.version.Agent == nil {
		return nil
	}
	return st.version.Agent.Spec.Limits
}

func (st *interactiveSessionState) limitsOnLimit() string {
	on := "halt"
	if lim := st.limits(); lim != nil && lim.OnLimit != "" {
		on = lim.OnLimit
	}
	return on
}

func (st *interactiveSessionState) maxLoopIterations() int {
	if lim := st.limits(); lim != nil && lim.MaxLoopIterations != nil {
		return *lim.MaxLoopIterations
	}
	return 0
}

func (st *interactiveSessionState) maxWallClockSeconds() int {
	if lim := st.limits(); lim != nil && lim.MaxWallClockSeconds != nil {
		return *lim.MaxWallClockSeconds
	}
	return 0
}

// sessionTokenLimitError reports when cumulative provider session usage exceeds max_tokens_per_run.
func (st *interactiveSessionState) sessionTokenLimitError() error {
	max := st.maxTokensPerRun()
	if max <= 0 || st.sessionUsage.Total() <= max {
		return nil
	}
	return &executor.LimitError{Kind: executor.LimitMaxTokensPerRun, OnLimit: st.limitsOnLimit()}
}

// sessionLoopIterationLimitError counts each interactive turn as one loop iteration (manifest: entire run).
func (st *interactiveSessionState) sessionLoopIterationLimitError() error {
	max := st.maxLoopIterations()
	if max <= 0 || st.turnCount < max {
		return nil
	}
	return &executor.LimitError{Kind: executor.LimitMaxLoopIterations, OnLimit: st.limitsOnLimit()}
}

func (st *interactiveSessionState) sessionWallClockLimitError() error {
	max := st.maxWallClockSeconds()
	if max <= 0 || st.sessionStartedAt.IsZero() {
		return nil
	}
	if time.Since(st.sessionStartedAt) <= time.Duration(max)*time.Second {
		return nil
	}
	return &executor.LimitError{Kind: executor.LimitMaxWallClockSeconds, OnLimit: st.limitsOnLimit()}
}

// runContext applies the session wall-clock deadline so per-completion trackers cannot reset the budget.
func (st *interactiveSessionState) runContext(ctx context.Context) (context.Context, context.CancelFunc) {
	max := st.maxWallClockSeconds()
	if max <= 0 || st.sessionStartedAt.IsZero() {
		return ctx, func() {}
	}
	deadline := st.sessionStartedAt.Add(time.Duration(max) * time.Second)
	return context.WithDeadline(ctx, deadline)
}
