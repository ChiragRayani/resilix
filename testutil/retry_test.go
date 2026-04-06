package testutil

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ChiragRayani/resilix"
)

func TestRetry_ExhaustsAttempts(t *testing.T) {
	r := resilix.NewRetry(resilix.RetryConfig{
		MaxAttempts: 3,
		Backoff:     resilix.ConstantBackoff(0),
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

func TestRetry_SucceedsWhenLaterAttemptSucceeds(t *testing.T) {
	r := resilix.NewRetry(resilix.RetryConfig{
		MaxAttempts: 4,
		Backoff:     resilix.ConstantBackoff(0),
	})

	attempts := 0
	err := r.ExecuteRetry(context.Background(), func(_ context.Context) error {
		attempts++
		if attempts < 3 {
			return errDownstream
		}
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetry_MaxRetriesExceededUnwrapsCause(t *testing.T) {
	r := resilix.NewRetry(resilix.RetryConfig{
		MaxAttempts: 2,
		Backoff:     resilix.ConstantBackoff(0),
	})

	err := r.ExecuteRetry(context.Background(), func(_ context.Context) error {
		return errDownstream
	})

	if !errors.Is(err, resilix.ErrMaxRetriesExceeded) {
		t.Fatalf("expected ErrMaxRetriesExceeded, got: %v", err)
	}
	if !errors.Is(err, errDownstream) {
		t.Fatalf("expected wrapped downstream error: %v", err)
	}
}

func TestRetry_ContextCancelDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var attempts int32
	r := resilix.NewRetry(resilix.RetryConfig{
		MaxAttempts: 5,
		Backoff:     resilix.ConstantBackoff(200 * time.Millisecond),
	})

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := r.ExecuteRetry(ctx, func(_ context.Context) error {
		atomic.AddInt32(&attempts, 1)
		return errDownstream
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
	if atomic.LoadInt32(&attempts) != 1 {
		t.Fatalf("expected 1 completed attempt before cancel, got %d", attempts)
	}
}

func TestRetry_RetryNoneSingleAttempt(t *testing.T) {
	r := resilix.NewRetry(resilix.RetryConfig{
		MaxAttempts: 5,
		Backoff:     resilix.ConstantBackoff(0),
		RetryIf:     resilix.RetryNone,
	})

	attempts := 0
	err := r.ExecuteRetry(context.Background(), func(_ context.Context) error {
		attempts++
		return errDownstream
	})

	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}
	if !errors.Is(err, resilix.ErrMaxRetriesExceeded) {
		t.Fatalf("expected ErrMaxRetriesExceeded, got: %v", err)
	}
}

func TestRetry_RetryIfOR(t *testing.T) {
	e1 := errors.New("e1")
	e2 := errors.New("e2")
	r := resilix.NewRetry(resilix.RetryConfig{
		MaxAttempts: 3,
		Backoff:     resilix.ConstantBackoff(0),
		RetryIf:     resilix.RetryIf(resilix.RetryOn(e1), resilix.RetryOn(e2)),
	})

	attempts := 0
	err := r.ExecuteRetry(context.Background(), func(_ context.Context) error {
		attempts++
		if attempts == 1 {
			return e1
		}
		if attempts == 2 {
			return e2
		}
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}
