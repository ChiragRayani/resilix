package resilix

import (
	"context"
	"time"
)

// policyRunner is the internal interface that lets Pipeline chain heterogeneous
// policies together.  Each policy adapter wraps one concrete policy type.
type policyRunner interface {
	Policy
	run(ctx context.Context, fn func(context.Context) error) error
}

// --- Adapters -----------------------------------------------------------
// Each adapter wraps one policy and implements policyRunner.

type cbRunner struct{ cb *CircuitBreaker }

func (a cbRunner) Name() string { return a.cb.Name() }
func (a cbRunner) run(ctx context.Context, fn func(context.Context) error) error {
	return a.cb.ExecuteCB(ctx, fn)
}

type retryRunner struct{ r *Retry }

func (a retryRunner) Name() string { return a.r.Name() }
func (a retryRunner) run(ctx context.Context, fn func(context.Context) error) error {
	return a.r.ExecuteRetry(ctx, fn)
}

type bulkheadRunner struct{ b *Bulkhead }

func (a bulkheadRunner) Name() string { return a.b.Name() }
func (a bulkheadRunner) run(ctx context.Context, fn func(context.Context) error) error {
	return a.b.ExecuteBulkhead(ctx, fn)
}

type timeoutRunner struct{ t *Timeout }

func (a timeoutRunner) Name() string { return a.t.Name() }
func (a timeoutRunner) run(ctx context.Context, fn func(context.Context) error) error {
	return a.t.ExecuteTimeout(ctx, fn)
}

// --- Pipeline -----------------------------------------------------------

// Pipeline chains multiple policies. Policies are applied in order: the first
// policy in the slice is the outermost wrapper.
//
// Recommended ordering (outermost → innermost):
//   Timeout → Retry → CircuitBreaker → Bulkhead
//
// This means: the timeout governs the entire operation including retries;
// the circuit breaker is checked before each attempt; the bulkhead gates
// each individual call.
type Pipeline struct {
	name    string
	runners []policyRunner
}

// NewPipeline creates a pipeline from a mix of policy types.
// Accepted types: *CircuitBreaker, *Retry, *Bulkhead, *Timeout.
// Panics on unknown types.
func NewPipeline(name string, policies ...Policy) *Pipeline {
	runners := make([]policyRunner, len(policies))
	for i, p := range policies {
		switch v := p.(type) {
		case *CircuitBreaker:
			runners[i] = cbRunner{v}
		case *Retry:
			runners[i] = retryRunner{v}
		case *Bulkhead:
			runners[i] = bulkheadRunner{v}
		case *Timeout:
			runners[i] = timeoutRunner{v}
		default:
			panic("resilix: unsupported policy type in NewPipeline")
		}
	}
	return &Pipeline{name: name, runners: runners}
}

// Name implements Policy.
func (p *Pipeline) Name() string { return p.name }

// Execute runs fn through all policies in order.
func (p *Pipeline) Execute(ctx context.Context, fn func(context.Context) error) error {
	return p.chain(ctx, fn, 0)
}

func (p *Pipeline) chain(ctx context.Context, fn func(context.Context) error, index int) error {
	if index >= len(p.runners) {
		return fn(ctx)
	}
	current := p.runners[index]
	return current.run(ctx, func(ctx context.Context) error {
		return p.chain(ctx, fn, index+1)
	})
}

// --- Generic top-level Execute ------------------------------------------

// Execute runs fn through policy, returning a typed result.
// policy can be a *CircuitBreaker, *Retry, *Bulkhead, *Timeout, or *Pipeline.
//
// Example:
//
//	result, err := resilix.Execute(ctx, pipeline, func(ctx context.Context) (MyResult, error) {
//	    return client.Call(ctx)
//	})
func Execute[T any](ctx context.Context, policy Policy, fn func(context.Context) (T, error)) (T, error) {
	var result T
	var innerErr error

	run := func(ctx context.Context) error {
		result, innerErr = fn(ctx)
		return innerErr
	}

	var policyErr error
	switch p := policy.(type) {
	case *CircuitBreaker:
		policyErr = p.ExecuteCB(ctx, run)
	case *Retry:
		policyErr = p.ExecuteRetry(ctx, run)
	case *Bulkhead:
		policyErr = p.ExecuteBulkhead(ctx, run)
	case *Timeout:
		policyErr = p.ExecuteTimeout(ctx, run)
	case *Pipeline:
		policyErr = p.Execute(ctx, run)
	default:
		panic("resilix: unsupported policy type in Execute")
	}

	if policyErr != nil {
		var zero T
		return zero, policyErr
	}
	return result, innerErr
}

// --- Fallback wrapper ----------------------------------------------------

// WithFallback runs fn through policy. If the policy returns an error,
// fallback is called with that error to produce a default value.
// This is useful for graceful degradation.
func WithFallback[T any](
	ctx context.Context,
	policy Policy,
	fn func(context.Context) (T, error),
	fallback func(context.Context, error) (T, error),
) (T, error) {
	result, err := Execute(ctx, policy, fn)
	if err != nil {
		return fallback(ctx, err)
	}
	return result, nil
}

// --- Convenience constructors -------------------------------------------

// DefaultPipeline builds a sensible pipeline for a single downstream with
// sane defaults. Good for getting started quickly.
//
//	Timeout(5s) → Retry(3 attempts, exp backoff) → CircuitBreaker(50% rate, 30s)
func DefaultPipeline(name string) *Pipeline {
	return NewPipeline(name,
		NewTimeout(TimeoutConfig{
			Name:     name + ".timeout",
			Duration: 5 * time.Second,
		}),
		NewRetry(RetryConfig{
			Name:        name + ".retry",
			MaxAttempts: 3,
			Backoff:     ExponentialBackoffWithJitter(100*time.Millisecond, 2.0, 10*time.Second, 0.2),
		}),
		NewCircuitBreaker(CBConfig{
			Name:             name + ".cb",
			FailureThreshold: 0.5,
			MinRequests:      5,
			OpenTimeout:      30 * time.Second,
			Window:           NewCountWindow(10),
		}),
	)
}
