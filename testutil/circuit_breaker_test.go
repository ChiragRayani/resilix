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
