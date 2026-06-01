package tooldispatch

import (
	"errors"
	"fmt"
)

// Dispatch-side sentinel errors. Distinct values let policy and persistence
// route no-handler, capacity, lease, and integrity outcomes separately.
var (
	// ErrNoHandler means no worker is registered for the tool@version.
	ErrNoHandler = errors.New("no handler registered for tool")

	// ErrCapacityExhausted means workers exist but none are idle; the call may
	// be queued until capacity frees or the deadline elapses.
	ErrCapacityExhausted = errors.New("tool handler capacity exhausted")

	// ErrQueueFull means the per-tool@version capacity wait queue is full.
	ErrQueueFull = errors.New("tool dispatch wait queue full")

	// ErrLeaseExpired means the worker lease ended before a durable result was acked.
	ErrLeaseExpired = errors.New("tool dispatch lease expired")

	// ErrIndeterminate means execution outcome is unknown (e.g. worker died mid-call).
	ErrIndeterminate = errors.New("tool dispatch outcome indeterminate")
)

// IntegrityViolation identifies which allowlist check failed at dispatch.
type IntegrityViolation string

const (
	IntegrityWorkloadIdentity IntegrityViolation = "workload_identity"
	IntegrityImageDigest      IntegrityViolation = "image_digest"
	IntegrityContractVersion  IntegrityViolation = "contract_version"
)

// IntegrityError is returned when a routed worker fails an allowlist check for
// (agent, tool, version): workload identity, approved image digest, or contract version.
type IntegrityError struct {
	Violation IntegrityViolation
	Tool      string
	Version   string
	Detail    string
}

func (e *IntegrityError) Error() string {
	if e == nil {
		return "tool dispatch integrity violation"
	}
	key := ToolKey(e.Tool, e.Version)
	if e.Detail != "" {
		return fmt.Sprintf("tool dispatch integrity violation (%s) for %s: %s", e.Violation, key, e.Detail)
	}
	return fmt.Sprintf("tool dispatch integrity violation (%s) for %s", e.Violation, key)
}

func (e *IntegrityError) Is(target error) bool {
	_, ok := target.(*IntegrityError)
	return ok
}

func IsNoHandler(err error) bool {
	return errors.Is(err, ErrNoHandler)
}

func IsCapacityExhausted(err error) bool {
	return errors.Is(err, ErrCapacityExhausted)
}

func IsQueueFull(err error) bool {
	return errors.Is(err, ErrQueueFull)
}

func IsLeaseExpired(err error) bool {
	return errors.Is(err, ErrLeaseExpired)
}

func IsIndeterminate(err error) bool {
	return errors.Is(err, ErrIndeterminate)
}

func IsIntegrityError(err error) bool {
	var ie *IntegrityError
	return errors.As(err, &ie)
}
