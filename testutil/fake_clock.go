package testutil

import (
	"sync"
	"time"
)

// FakeClock is a controllable clock for use in tests. It implements
// resilix.Clock and allows you to advance time manually, making tests
// for time-dependent logic (circuit breaker timeouts, backoff intervals)
// fast and deterministic.
//
// Usage:
//
//	clock := testutil.NewFakeClock(time.Now())
//	cb := resilix.NewCircuitBreaker(resilix.CBConfig{
//	    Clock: clock,
//	    OpenTimeout: 30 * time.Second,
//	    ...
//	})
//
//	// trip the breaker
//	// ...
//
//	clock.Advance(31 * time.Second)  // now the breaker should be half-open
type FakeClock struct {
	mu  sync.RWMutex
	now time.Time
}

// NewFakeClock returns a FakeClock starting at the given time.
// Pass time.Now() for a realistic starting point, or a fixed value for
// fully deterministic tests.
func NewFakeClock(initial time.Time) *FakeClock {
	return &FakeClock{now: initial}
}

// Now implements resilix.Clock.
func (c *FakeClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

// Since implements resilix.Clock.
func (c *FakeClock) Since(t time.Time) time.Duration {
	return c.Now().Sub(t)
}

// Advance moves the clock forward by d. Panics if d is negative.
func (c *FakeClock) Advance(d time.Duration) {
	if d < 0 {
		panic("testutil.FakeClock: Advance called with negative duration")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Set moves the clock to an absolute time.
func (c *FakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}
