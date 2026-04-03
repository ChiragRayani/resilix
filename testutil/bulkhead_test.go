package testutil

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ChiragRayani/resilix"
)

func TestBulkhead_RejectsBeyondLimit(t *testing.T) {
	bh := resilix.NewBulkhead(resilix.BulkheadConfig{
		MaxConcurrent: 2,
		WaitTimeout:   0,
	})

	ready := make(chan struct{})
	done := make(chan struct{})
	blocker := func(_ context.Context) error {
		ready <- struct{}{}
		<-done
		return nil
	}

	go func() { _ = bh.ExecuteBulkhead(context.Background(), blocker) }()
	go func() { _ = bh.ExecuteBulkhead(context.Background(), blocker) }()

	<-ready
	<-ready

	err := bh.ExecuteBulkhead(context.Background(), succeed)
	if !errors.Is(err, resilix.ErrBulkheadFull) {
		t.Fatalf("expected ErrBulkheadFull, got: %v", err)
	}

	close(done)
}

func TestBulkhead_SequentialAcquireRelease(t *testing.T) {
	bh := resilix.NewBulkhead(resilix.BulkheadConfig{
		MaxConcurrent: 1,
		WaitTimeout:   0,
	})

	if err := bh.ExecuteBulkhead(context.Background(), succeed); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := bh.ExecuteBulkhead(context.Background(), succeed); err != nil {
		t.Fatalf("second call after release: %v", err)
	}
	if avail := bh.Available(); avail != 1 {
		t.Fatalf("expected 1 available slot when idle, got %d", avail)
	}
}

func TestBulkhead_WaitTimeoutAcquiresWhenSlotFrees(t *testing.T) {
	bh := resilix.NewBulkhead(resilix.BulkheadConfig{
		MaxConcurrent: 1,
		WaitTimeout:   300 * time.Millisecond,
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = bh.ExecuteBulkhead(context.Background(), func(_ context.Context) error {
			time.Sleep(30 * time.Millisecond)
			return nil
		})
	}()

	time.Sleep(5 * time.Millisecond)

	err := bh.ExecuteBulkhead(context.Background(), succeed)
	if err != nil {
		t.Fatalf("expected waiter to acquire after holder finished: %v", err)
	}
	wg.Wait()
}

func TestBulkhead_ContextCanceledWhileWaiting(t *testing.T) {
	bh := resilix.NewBulkhead(resilix.BulkheadConfig{
		MaxConcurrent: 1,
		WaitTimeout:   time.Second,
	})

	hold := make(chan struct{})
	release := make(chan struct{})

	go func() {
		_ = bh.ExecuteBulkhead(context.Background(), func(_ context.Context) error {
			close(hold)
			<-release
			return nil
		})
	}()

	<-hold

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- bh.ExecuteBulkhead(ctx, succeed)
	}()

	cancel()

	err := <-errCh
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}

	close(release)
}
