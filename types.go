package resilix

import (
	"context"
	"time"
)

// State represents the circuit breaker state.
type State int

const (
	StateClosed   State = iota // normal operation — calls pass through
	StateOpen                  // tripped — calls are rejected immediately
	StateHalfOpen              // recovering — a limited number of probe calls are allowed
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Policy is the common interface for all resilience primitives.
// Policies are composable; a Pipeline implements Policy itself.
type Policy interface {
	// Name returns a human-readable identifier used in metrics and logs.
	Name() string
}

// Observer receives events from any resilience primitive.
// Implement this to wire in OpenTelemetry, Prometheus, structured logs, etc.
type Observer interface {
	// OnStateChange is called when a circuit breaker transitions between states.
	OnStateChange(policy string, from, to State)

	// OnSuccess is called after a successful execution.
	OnSuccess(policy string, latency time.Duration)

	// OnFailure is called after a failed execution (counted against the threshold).
	OnFailure(policy string, err error, latency time.Duration)

	// OnRejected is called when a call is rejected without being attempted.
	// This happens when the circuit is open, the bulkhead is full, etc.
	OnRejected(policy string)

	// OnRetry is called before each retry attempt (attempt starts at 1).
	OnRetry(policy string, attempt int, err error)
}

// NoopObserver is a zero-allocation observer that discards all events.
// It is the default when no observer is configured.
type NoopObserver struct{}

func (NoopObserver) OnStateChange(_ string, _, _ State)              {}
func (NoopObserver) OnSuccess(_ string, _ time.Duration)             {}
func (NoopObserver) OnFailure(_ string, _ error, _ time.Duration)    {}
func (NoopObserver) OnRejected(_ string)                              {}
func (NoopObserver) OnRetry(_ string, _ int, _ error)                {}

// RetryPredicate decides whether a given error on a given attempt is retryable.
type RetryPredicate func(err error, attempt int) bool

// Clock abstracts time so that tests can control it.
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
}

// realClock delegates to the standard library.
type realClock struct{}

func (realClock) Now() time.Time                  { return time.Now() }
func (realClock) Since(t time.Time) time.Duration { return time.Since(t) }

// defaultClock is the singleton real-time clock used by all primitives.
var defaultClock Clock = realClock{}

// FailureWindow tracks the recent failure rate used by the circuit breaker.
// Swap in a different implementation for sliding-window or count-based semantics.
type FailureWindow interface {
	// Record registers one result. success=true on a successful call.
	Record(success bool)
	// FailureRate returns a value in [0, 1].
	FailureRate() float64
	// Reset clears all accumulated data.
	Reset()
	// Total returns the total number of calls recorded.
	Total() int64
}

// ErrCircuitOpen is returned when a call is rejected because the circuit is open.
var ErrCircuitOpen = &policyError{msg: "circuit breaker is open"}

// ErrBulkheadFull is returned when no execution slot is available.
var ErrBulkheadFull = &policyError{msg: "bulkhead is full"}

// ErrMaxRetriesExceeded is returned when all retry attempts are exhausted.
var ErrMaxRetriesExceeded = &policyError{msg: "max retries exceeded", wrapped: nil}

// ErrTimeout is returned when an execution exceeds its deadline.
var ErrTimeout = &policyError{msg: "execution timed out"}

type policyError struct {
	msg     string
	wrapped error
}

func (e *policyError) Error() string {
	if e.wrapped != nil {
		return e.msg + ": " + e.wrapped.Error()
	}
	return e.msg
}

func (e *policyError) Unwrap() error { return e.wrapped }

func (e *policyError) Is(target error) bool {
	t, ok := target.(*policyError)
	if !ok {
		return false
	}
	return e.msg == t.msg
}

func wrapMaxRetries(cause error) error {
	return &policyError{msg: "max retries exceeded", wrapped: cause}
}

// execute is the internal generic runner used by all policy wrappers.
// Public callers use the top-level Execute[T] function.
func execute[T any](ctx context.Context, fn func(context.Context) (T, error)) (T, error) {
	return fn(ctx)
}
