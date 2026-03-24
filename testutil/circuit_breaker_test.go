package testutil

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ChiragRayani/resilix"
)

var errDownstream = errors.New("downstream error")

func makeCB(t *testing.T, clock *FakeClock) *resilix.CircuitBreaker {
	t.Helper()
	return resilix.NewCircuitBreaker(resilix.CBConfig{
		Name:             "test-cb",
		FailureThreshold: 0.5,
		MinRequests:      4,
		OpenTimeout:      30 * time.Second,
		HalfOpenMax:      1,
		Window:           resilix.NewCountWindow(4),
		Clock:            clock,
	})
}

func fail(ctx context.Context) error    { return errDownstream }
func succeed(ctx context.Context) error { return nil }

func TestCircuitBreaker_TripsOnThreshold(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cb := makeCB(t, clock)

	// 2 successes, 2 failures → 50% failure rate at MinRequests (4 total)
	if err := cb.ExecuteCB(context.Background(), succeed); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := cb.ExecuteCB(context.Background(), succeed); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := cb.ExecuteCB(context.Background(), fail); err == nil || errors.Is(err, resilix.ErrCircuitOpen) {
		t.Fatalf("expected downstream error, got: %v", err)
	}
	if err := cb.ExecuteCB(context.Background(), fail); err == nil || errors.Is(err, resilix.ErrCircuitOpen) {
		t.Fatalf("expected downstream error, got: %v", err)
	}

	// circuit should now be open
	if got := cb.State(); got != resilix.StateOpen {
		t.Fatalf("expected StateOpen, got %s", got)
	}

	// next call should be rejected
	if err := cb.ExecuteCB(context.Background(), succeed); !errors.Is(err, resilix.ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got: %v", err)
	}
}

func TestCircuitBreaker_HalfOpenOnTimeout(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cb := makeCB(t, clock)

	// trip it
	for i := 0; i < 4; i++ {
		_ = cb.ExecuteCB(context.Background(), fail)
	}
	if cb.State() != resilix.StateOpen {
		t.Fatal("circuit should be open")
	}

	// advance past the open timeout
	clock.Advance(31 * time.Second)

	if got := cb.State(); got != resilix.StateHalfOpen {
		t.Fatalf("expected StateHalfOpen after timeout, got %s", got)
	}
}

func TestCircuitBreaker_ReclosesOnProbeSuccess(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cb := makeCB(t, clock)

	// trip
	for i := 0; i < 4; i++ {
		_ = cb.ExecuteCB(context.Background(), fail)
	}
	clock.Advance(31 * time.Second)

	// probe succeeds → closed
	if err := cb.ExecuteCB(context.Background(), succeed); err != nil {
		t.Fatalf("probe should succeed: %v", err)
	}
	if cb.State() != resilix.StateClosed {
		t.Fatalf("expected StateClosed after successful probe, got %s", cb.State())
	}
}

func TestCircuitBreaker_ReopensOnProbeFailure(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cb := makeCB(t, clock)

	// trip
	for i := 0; i < 4; i++ {
		_ = cb.ExecuteCB(context.Background(), fail)
	}
	clock.Advance(31 * time.Second)

	// probe fails → back to open
	_ = cb.ExecuteCB(context.Background(), fail)
	if cb.State() != resilix.StateOpen {
		t.Fatalf("expected StateOpen after failed probe, got %s", cb.State())
	}
}

func TestRetry_ExhaustsAttempts(t *testing.T) {
	r := resilix.NewRetry(resilix.RetryConfig{
		MaxAttempts: 3,
		Backoff:     resilix.ConstantBackoff(0), // no sleep in tests
	})

	attempts := 0
	err := r.ExecuteRetry(context.Background(), func(_ context.Context) error {
		attempts++
		return errDownstream
	})

	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
	if !errors.Is(err, resilix.ErrMaxRetriesExceeded) {
		t.Fatalf("expected ErrMaxRetriesExceeded, got: %v", err)
	}
}

func TestRetry_StopsOnPredicateFalse(t *testing.T) {
	nonRetryable := errors.New("non-retryable")
	r := resilix.NewRetry(resilix.RetryConfig{
		MaxAttempts: 5,
		Backoff:     resilix.ConstantBackoff(0),
		RetryIf:     resilix.RetryOn(errDownstream),
	})

	attempts := 0
	err := r.ExecuteRetry(context.Background(), func(_ context.Context) error {
		attempts++
		return nonRetryable
	})

	if attempts != 1 {
		t.Fatalf("expected 1 attempt (predicate false), got %d", attempts)
	}
	if !errors.Is(err, resilix.ErrMaxRetriesExceeded) {
		t.Fatalf("expected ErrMaxRetriesExceeded, got: %v", err)
	}
}

func TestBulkhead_RejectsBeyondLimit(t *testing.T) {
	bh := resilix.NewBulkhead(resilix.BulkheadConfig{
		MaxConcurrent: 2,
		WaitTimeout:   0,
	})

	// occupy both slots
	ready := make(chan struct{})
	done := make(chan struct{})
	blocker := func(_ context.Context) error {
		ready <- struct{}{}
		<-done
		return nil
	}

	go bh.ExecuteBulkhead(context.Background(), blocker) //nolint:errcheck
	go bh.ExecuteBulkhead(context.Background(), blocker) //nolint:errcheck

	<-ready
	<-ready // both slots occupied

	// third call should fail fast
	err := bh.ExecuteBulkhead(context.Background(), succeed)
	if !errors.Is(err, resilix.ErrBulkheadFull) {
		t.Fatalf("expected ErrBulkheadFull, got: %v", err)
	}

	close(done)
}

func TestTimeout_ReturnsErrTimeout(t *testing.T) {
	to := resilix.NewTimeout(resilix.TimeoutConfig{
		Duration: 50 * time.Millisecond,
	})

	err := to.ExecuteTimeout(context.Background(), func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
			return nil
		}
	})

	if !errors.Is(err, resilix.ErrTimeout) {
		t.Fatalf("expected ErrTimeout, got: %v", err)
	}
}

func TestPipeline_GenericExecute(t *testing.T) {
	pipeline := resilix.DefaultPipeline("test")

	result, err := resilix.Execute(context.Background(), pipeline,
		func(_ context.Context) (int, error) { return 42, nil })

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 42 {
		t.Fatalf("expected 42, got %d", result)
	}
}

func TestSlidingWindow_FailureRate(t *testing.T) {
	clock := NewFakeClock(time.Now())
	w := resilix.NewSlidingWindow(60*time.Second, 6)
	w.SetClock(clock) // see note in window.go

	w.Record(false)
	w.Record(false)
	w.Record(true)
	w.Record(true)

	rate := w.FailureRate()
	if rate != 0.5 {
		t.Fatalf("expected 0.5, got %f", rate)
	}

	// advance past window
	clock.Advance(61 * time.Second)
	w.Record(true)

	rate = w.FailureRate()
	if rate != 0.0 {
		t.Fatalf("expected 0.0 after eviction, got %f", rate)
	}
}
