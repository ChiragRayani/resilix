package resilix

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"time"
)

// BackoffStrategy returns the wait duration before attempt number `attempt` (1-indexed).
type BackoffStrategy func(attempt int) time.Duration

// ConstantBackoff waits the same duration before every retry.
func ConstantBackoff(d time.Duration) BackoffStrategy {
	return func(_ int) time.Duration { return d }
}

// ExponentialBackoff implements base * multiplier^(attempt-1), capped at max.
// Pass max=0 to disable the cap.
func ExponentialBackoff(base time.Duration, multiplier float64, max time.Duration) BackoffStrategy {
	return func(attempt int) time.Duration {
		d := float64(base) * math.Pow(multiplier, float64(attempt-1))
		if max > 0 && time.Duration(d) > max {
			return max
		}
		return time.Duration(d)
	}
}

// ExponentialBackoffWithJitter adds ±jitterFraction random jitter to
// ExponentialBackoff to avoid thundering herd on retry storms.
// jitterFraction should be in (0, 1]; 0.2 means ±20%.
func ExponentialBackoffWithJitter(base time.Duration, multiplier float64, max time.Duration, jitterFraction float64) BackoffStrategy {
	exp := ExponentialBackoff(base, multiplier, max)
	return func(attempt int) time.Duration {
		d := exp(attempt)
		jitter := float64(d) * jitterFraction * (rand.Float64()*2 - 1) //nolint:gosec
		return time.Duration(float64(d) + jitter)
	}
}

// LinearBackoff increases wait by `step` on each attempt.
func LinearBackoff(initial, step time.Duration, max time.Duration) BackoffStrategy {
	return func(attempt int) time.Duration {
		d := initial + step*time.Duration(attempt-1)
		if max > 0 && d > max {
			return max
		}
		return d
	}
}

// --- Built-in retry predicates -------------------------------------------

// RetryAll retries any non-nil error.
func RetryAll(err error, _ int) bool { return err != nil }

// RetryNone never retries.
func RetryNone(_ error, _ int) bool { return false }

// RetryOn returns a predicate that retries only when the error matches target
// (using errors.Is semantics).
func RetryOn(target error) RetryPredicate {
	return func(err error, _ int) bool { return errors.Is(err, target) }
}

// RetryIf composes multiple predicates with OR semantics.
func RetryIf(preds ...RetryPredicate) RetryPredicate {
	return func(err error, attempt int) bool {
		for _, p := range preds {
			if p(err, attempt) {
				return true
			}
		}
		return false
	}
}

// --- Retry config & implementation ---------------------------------------

// RetryConfig configures a Retry policy.
type RetryConfig struct {
	// Name identifies this policy in metrics.
	Name string

	// MaxAttempts is the total number of attempts (including the first).
	// A value of 3 means 1 initial call + 2 retries.
	// Default: 3.
	MaxAttempts int

	// Backoff controls how long to wait between attempts.
	// Default: ExponentialBackoff(100ms, 2.0, 10s).
	Backoff BackoffStrategy

	// RetryIf is called after each failure to decide whether to retry.
	// Default: RetryAll (retry any error).
	RetryIf RetryPredicate

	// Observer receives retry and failure events.
	Observer Observer

	// Clock is used for sleeping. Override in tests.
	Clock Clock
}

func (c *RetryConfig) applyDefaults() {
	if c.Name == "" {
		c.Name = "retry"
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 3
	}
	if c.Backoff == nil {
		c.Backoff = ExponentialBackoff(100*time.Millisecond, 2.0, 10*time.Second)
	}
	if c.RetryIf == nil {
		c.RetryIf = RetryAll
	}
	if c.Observer == nil {
		c.Observer = NoopObserver{}
	}
	if c.Clock == nil {
		c.Clock = defaultClock
	}
}

// Retry is a policy that re-executes a call on failure according to a
// configurable backoff and predicate.
type Retry struct {
	cfg RetryConfig
}

// NewRetry creates a Retry policy with the given config.
func NewRetry(cfg RetryConfig) *Retry {
	cfg.applyDefaults()
	return &Retry{cfg: cfg}
}

// Name implements Policy.
func (r *Retry) Name() string { return r.cfg.Name }

// ExecuteRetry runs fn with retry semantics. Returns the last error if all
// attempts are exhausted, wrapped in ErrMaxRetriesExceeded.
func (r *Retry) ExecuteRetry(ctx context.Context, fn func(context.Context) error) error {
	var lastErr error
	for attempt := 1; attempt <= r.cfg.MaxAttempts; attempt++ {
		start := r.cfg.Clock.Now()
		err := fn(ctx)
		latency := r.cfg.Clock.Since(start)

		if err == nil {
			r.cfg.Observer.OnSuccess(r.cfg.Name, latency)
			return nil
		}

		lastErr = err
		r.cfg.Observer.OnFailure(r.cfg.Name, err, latency)

		if attempt == r.cfg.MaxAttempts || !r.cfg.RetryIf(err, attempt) {
			break
		}

		r.cfg.Observer.OnRetry(r.cfg.Name, attempt, err)
		wait := r.cfg.Backoff(attempt)
		if wait > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
	}

	return wrapMaxRetries(lastErr)
}
