package resilix

import (
	"context"
	"time"
)

// TimeoutConfig configures a Timeout policy.
type TimeoutConfig struct {
	// Name identifies this policy in metrics.
	Name string

	// Duration is the maximum time a call may take.
	// If the call exceeds this, it receives a cancelled context and
	// the caller receives ErrTimeout.
	Duration time.Duration

	// Observer receives success and failure events.
	Observer Observer

	// Clock is used for tracking. Override in tests.
	Clock Clock
}

func (c *TimeoutConfig) applyDefaults() {
	if c.Name == "" {
		c.Name = "timeout"
	}
	if c.Duration <= 0 {
		c.Duration = 5 * time.Second
	}
	if c.Observer == nil {
		c.Observer = NoopObserver{}
	}
	if c.Clock == nil {
		c.Clock = defaultClock
	}
}

// Timeout wraps a function with a deadline. If fn does not complete within
// the configured duration, the context passed to fn is cancelled and
// ErrTimeout is returned to the caller.
//
// Importantly, Timeout waits for fn to return even after the deadline passes.
// This prevents goroutine leaks when fn does not respect context cancellation,
// though it will add latency in that case. Well-behaved fns should honour ctx.
type Timeout struct {
	cfg TimeoutConfig
}

// NewTimeout creates a Timeout policy.
func NewTimeout(cfg TimeoutConfig) *Timeout {
	cfg.applyDefaults()
	return &Timeout{cfg: cfg}
}

// Name implements Policy.
func (t *Timeout) Name() string { return t.cfg.Name }

// ExecuteTimeout runs fn with a context that expires after the configured duration.
func (t *Timeout) ExecuteTimeout(ctx context.Context, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, t.cfg.Duration)
	defer cancel()

	type result struct{ err error }
	ch := make(chan result, 1)

	start := t.cfg.Clock.Now()
	go func() {
		ch <- result{err: fn(ctx)}
	}()

	select {
	case res := <-ch:
		latency := t.cfg.Clock.Since(start)
		if res.err != nil {
			t.cfg.Observer.OnFailure(t.cfg.Name, res.err, latency)
			return res.err
		}
		t.cfg.Observer.OnSuccess(t.cfg.Name, latency)
		return nil

	case <-ctx.Done():
		latency := t.cfg.Clock.Since(start)
		t.cfg.Observer.OnFailure(t.cfg.Name, ErrTimeout, latency)
		// drain ch to allow the goroutine to exit cleanly
		go func() { <-ch }()
		return ErrTimeout
	}
}
