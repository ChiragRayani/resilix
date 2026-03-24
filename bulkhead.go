package resilix

import (
	"context"
	"time"
)

// BulkheadConfig configures a Bulkhead.
type BulkheadConfig struct {
	// Name identifies this policy in metrics.
	Name string

	// MaxConcurrent is the maximum number of calls that may execute simultaneously.
	// Default: 10.
	MaxConcurrent int

	// WaitTimeout is how long a caller will wait for a slot before receiving
	// ErrBulkheadFull. Use 0 for fail-fast (never queue).
	// Default: 0 (fail fast).
	WaitTimeout time.Duration

	// Observer receives rejected and success events.
	Observer Observer

	// Clock is used for timeout tracking. Override in tests.
	Clock Clock
}

func (c *BulkheadConfig) applyDefaults() {
	if c.Name == "" {
		c.Name = "bulkhead"
	}
	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = 10
	}
	if c.Observer == nil {
		c.Observer = NoopObserver{}
	}
	if c.Clock == nil {
		c.Clock = defaultClock
	}
}

// Bulkhead limits the number of concurrent executions to prevent a slow
// downstream from monopolising the caller's goroutine pool.
//
// It is backed by a buffered channel used as a semaphore — zero allocations
// per call once the channel is created.
type Bulkhead struct {
	cfg  BulkheadConfig
	sem  chan struct{}
}

// NewBulkhead creates a Bulkhead with the given config.
func NewBulkhead(cfg BulkheadConfig) *Bulkhead {
	cfg.applyDefaults()
	return &Bulkhead{
		cfg: cfg,
		sem: make(chan struct{}, cfg.MaxConcurrent),
	}
}

// Name implements Policy.
func (b *Bulkhead) Name() string { return b.cfg.Name }

// Available returns the number of free execution slots.
func (b *Bulkhead) Available() int {
	return b.cfg.MaxConcurrent - len(b.sem)
}

// ExecuteBulkhead runs fn if a slot is available; otherwise it either waits
// (if WaitTimeout > 0) or returns ErrBulkheadFull immediately.
func (b *Bulkhead) ExecuteBulkhead(ctx context.Context, fn func(context.Context) error) error {
	acquired, err := b.acquire(ctx)
	if err != nil {
		b.cfg.Observer.OnRejected(b.cfg.Name)
		return err
	}
	if !acquired {
		b.cfg.Observer.OnRejected(b.cfg.Name)
		return ErrBulkheadFull
	}
	defer b.release()

	start := b.cfg.Clock.Now()
	err = fn(ctx)
	latency := b.cfg.Clock.Since(start)

	if err != nil {
		b.cfg.Observer.OnFailure(b.cfg.Name, err, latency)
	} else {
		b.cfg.Observer.OnSuccess(b.cfg.Name, latency)
	}
	return err
}

func (b *Bulkhead) acquire(ctx context.Context) (bool, error) {
	if b.cfg.WaitTimeout == 0 {
		// non-blocking
		select {
		case b.sem <- struct{}{}:
			return true, nil
		default:
			return false, nil
		}
	}

	// blocking with timeout
	timeout := time.NewTimer(b.cfg.WaitTimeout)
	defer timeout.Stop()

	select {
	case b.sem <- struct{}{}:
		return true, nil
	case <-timeout.C:
		return false, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func (b *Bulkhead) release() { <-b.sem }
