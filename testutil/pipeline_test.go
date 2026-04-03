package testutil

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ChiragRayani/resilix"
)

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

func TestExecute_CircuitOpenSkipsFn(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cb := resilix.NewCircuitBreaker(resilix.CBConfig{
		Name:             "cb",
		FailureThreshold: 0.5,
		MinRequests:      2,
		OpenTimeout:      time.Hour,
		Window:           resilix.NewCountWindow(2),
		Clock:            clock,
	})

	for i := 0; i < 2; i++ {
		_ = cb.ExecuteCB(context.Background(), fail)
	}
	if cb.State() != resilix.StateOpen {
		t.Fatalf("expected open circuit, got %s", cb.State())
	}

	var calls int
	_, err := resilix.Execute(context.Background(), cb, func(_ context.Context) (string, error) {
		calls++
		return "ok", nil
	})

	if !errors.Is(err, resilix.ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got: %v", err)
	}
	if calls != 0 {
		t.Fatalf("expected fn not to run when circuit open, calls=%d", calls)
	}
}

func TestPipeline_OrderingTimeoutRetryCircuit(t *testing.T) {
	clock := NewFakeClock(time.Now())
	cb := resilix.NewCircuitBreaker(resilix.CBConfig{
		Name:             "cb",
		FailureThreshold: 1.0,
		MinRequests:      1,
		OpenTimeout:      time.Hour,
		Window:           resilix.NewCountWindow(10),
		Clock:            clock,
	})

	p := resilix.NewPipeline("ordered",
		resilix.NewTimeout(resilix.TimeoutConfig{
			Duration: 50 * time.Millisecond,
			Clock:    clock,
		}),
		resilix.NewRetry(resilix.RetryConfig{
			MaxAttempts: 2,
			Backoff:     resilix.ConstantBackoff(0),
			Clock:       clock,
		}),
		cb,
	)

	// Failures trip the breaker after inner executions.
	for i := 0; i < 3; i++ {
		_, _ = resilix.Execute(context.Background(), p, func(_ context.Context) (int, error) {
			return 0, errDownstream
		})
	}

	if cb.State() != resilix.StateOpen {
		t.Fatalf("expected circuit open after failures, got %s", cb.State())
	}
}

func TestWithFallback_UsesFallbackOnError(t *testing.T) {
	r := resilix.NewRetry(resilix.RetryConfig{
		MaxAttempts: 2,
		Backoff:     resilix.ConstantBackoff(0),
	})

	s, err := resilix.WithFallback(context.Background(), r,
		func(_ context.Context) (string, error) {
			return "", errDownstream
		},
		func(_ context.Context, _ error) (string, error) {
			return "degraded", nil
		},
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != "degraded" {
		t.Fatalf("expected degraded result, got %q", s)
	}
}

type unsupportedPolicy struct{}

func (unsupportedPolicy) Name() string { return "unsupported" }

func TestExecute_PanicsOnUnsupportedPolicy(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for unsupported policy type in Execute")
		}
	}()

	_, _ = resilix.Execute(context.Background(), unsupportedPolicy{}, func(_ context.Context) (int, error) {
		return 0, nil
	})
}
