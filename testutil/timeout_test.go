package testutil

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ChiragRayani/resilix"
)

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

func TestTimeout_SuccessWhenFnReturnsQuickly(t *testing.T) {
	to := resilix.NewTimeout(resilix.TimeoutConfig{
		Duration: 200 * time.Millisecond,
	})

	err := to.ExecuteTimeout(context.Background(), func(_ context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTimeout_ParentContextAlreadyCanceled(t *testing.T) {
	to := resilix.NewTimeout(resilix.TimeoutConfig{
		Duration: time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := to.ExecuteTimeout(ctx, func(_ context.Context) error {
		return nil
	})

	// Child context from WithTimeout is done; Timeout maps ctx.Done() to ErrTimeout
	// (same branch as deadline exceeded).
	if !errors.Is(err, resilix.ErrTimeout) {
		t.Fatalf("expected ErrTimeout, got: %v", err)
	}
}
