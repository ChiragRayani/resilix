package resilix

import (
	"context"
	"sync"
	"time"
)

// CBConfig configures a CircuitBreaker.
type CBConfig struct {
	// Name identifies this breaker in metrics and logs.
	Name string

	// FailureThreshold is the failure rate [0,1] at which the circuit trips.
	// Default: 0.5 (50% failure rate trips the breaker).
	FailureThreshold float64

	// MinRequests is the minimum number of calls in the window before the
	// failure rate is evaluated. Prevents tripping on a single early failure.
	// Default: 5.
	MinRequests int64

	// OpenTimeout is how long the circuit stays open before moving to half-open.
	// Default: 30s.
	OpenTimeout time.Duration

	// HalfOpenMax is how many probe calls are allowed in the half-open state.
	// Default: 1.
	HalfOpenMax int64

	// Window is the failure tracking strategy. Defaults to CountWindow(10).
	Window FailureWindow

	// Observer receives state change and execution events.
	Observer Observer

	// Clock is used for time. Defaults to the real clock.
	// Override in tests with testutil.NewFakeClock().
	Clock Clock
}

func (c *CBConfig) applyDefaults() {
	if c.Name == "" {
		c.Name = "circuit-breaker"
	}
	if c.FailureThreshold <= 0 {
		c.FailureThreshold = 0.5
	}
	if c.MinRequests <= 0 {
		c.MinRequests = 5
	}
	if c.OpenTimeout <= 0 {
		c.OpenTimeout = 30 * time.Second
	}
	if c.HalfOpenMax <= 0 {
		c.HalfOpenMax = 1
	}
	if c.Window == nil {
		c.Window = NewCountWindow(10)
	}
	if c.Observer == nil {
		c.Observer = NoopObserver{}
	}
	if c.Clock == nil {
		c.Clock = defaultClock
	}
}

// CircuitBreaker implements the classic three-state circuit breaker pattern.
//
// Use Execute or ExecuteCB to run calls through it. CircuitBreaker is
// safe for concurrent use.
type CircuitBreaker struct {
	cfg          CBConfig
	mu           sync.Mutex
	state        State
	openedAt     time.Time     // when the circuit last opened
	halfOpenSent int64         // probe calls dispatched in half-open
}

// NewCircuitBreaker creates a new CircuitBreaker with the given config.
func NewCircuitBreaker(cfg CBConfig) *CircuitBreaker {
	cfg.applyDefaults()
	return &CircuitBreaker{cfg: cfg, state: StateClosed}
}

// Name implements Policy.
func (cb *CircuitBreaker) Name() string { return cb.cfg.Name }

// State returns the current circuit state. Safe to call concurrently.
func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.currentState()
}

// currentState evaluates whether an open circuit should transition to half-open.
// Must be called with cb.mu held.
func (cb *CircuitBreaker) currentState() State {
	if cb.state == StateOpen {
		if cb.cfg.Clock.Since(cb.openedAt) >= cb.cfg.OpenTimeout {
			cb.transitionTo(StateHalfOpen)
		}
	}
	return cb.state
}

// transitionTo moves to a new state and notifies the observer.
// Must be called with cb.mu held.
func (cb *CircuitBreaker) transitionTo(next State) {
	if cb.state == next {
		return
	}
	prev := cb.state
	cb.state = next
	if next == StateHalfOpen {
		cb.halfOpenSent = 0
	}
	if next == StateClosed {
		cb.cfg.Window.Reset()
	}
	cb.cfg.Observer.OnStateChange(cb.cfg.Name, prev, next)
}

// allow returns true if the call may proceed.
// Must be called with cb.mu held.
func (cb *CircuitBreaker) allow() bool {
	switch cb.currentState() {
	case StateClosed:
		return true
	case StateOpen:
		return false
	case StateHalfOpen:
		if cb.halfOpenSent < cb.cfg.HalfOpenMax {
			cb.halfOpenSent++
			return true
		}
		return false
	}
	return false
}

// record processes the result of a completed call.
// Must be called with cb.mu held.
func (cb *CircuitBreaker) record(success bool) {
	cb.cfg.Window.Record(success)

	switch cb.currentState() {
	case StateClosed:
		if cb.cfg.Window.Total() >= cb.cfg.MinRequests &&
			cb.cfg.Window.FailureRate() >= cb.cfg.FailureThreshold {
			cb.openedAt = cb.cfg.Clock.Now()
			cb.transitionTo(StateOpen)
		}
	case StateHalfOpen:
		if success {
			cb.transitionTo(StateClosed)
		} else {
			cb.openedAt = cb.cfg.Clock.Now()
			cb.transitionTo(StateOpen)
		}
	}
}

// ExecuteCB runs fn through the circuit breaker, returning ErrCircuitOpen if
// the circuit is open. Use the generic Execute[T] function for type-safe results.
func (cb *CircuitBreaker) ExecuteCB(ctx context.Context, fn func(context.Context) error) error {
	cb.mu.Lock()
	if !cb.allow() {
		cb.mu.Unlock()
		cb.cfg.Observer.OnRejected(cb.cfg.Name)
		return ErrCircuitOpen
	}
	cb.mu.Unlock()

	start := cb.cfg.Clock.Now()
	err := fn(ctx)
	latency := cb.cfg.Clock.Since(start)

	cb.mu.Lock()
	cb.record(err == nil)
	cb.mu.Unlock()

	if err != nil {
		cb.cfg.Observer.OnFailure(cb.cfg.Name, err, latency)
	} else {
		cb.cfg.Observer.OnSuccess(cb.cfg.Name, latency)
	}
	return err
}

// Reset forces the circuit breaker back to the closed state and clears the window.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	prev := cb.state
	cb.state = StateClosed
	cb.cfg.Window.Reset()
	cb.cfg.Observer.OnStateChange(cb.cfg.Name, prev, StateClosed)
}
