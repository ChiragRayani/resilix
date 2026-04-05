# resilix

[![Go Reference](https://pkg.go.dev/badge/github.com/ChiragRayani/resilix.svg)](https://pkg.go.dev/github.com/ChiragRayani/resilix)
[![Go Report Card](https://goreportcard.com/badge/github.com/ChiragRayani/resilix)](https://goreportcard.com/report/github.com/ChiragRayani/resilix)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**Composable, type-safe resilience primitives for Go services.**

Zero external dependencies in the core library. Optional OpenTelemetry integration ships as a separate subpackage.

---

## What is resilix?

**resilix** is a lightweight Go library that provides battle-tested resilience patterns to protect your services from cascading failures. When an upstream API goes down, a database slows to a crawl, or a third-party service starts timing out — resilix keeps your application responsive and degrading gracefully instead of collapsing.

It ships four core primitives — **Circuit Breaker**, **Retry**, **Bulkhead**, and **Timeout** — that can be used standalone or composed into a **Pipeline** for layered protection. Everything is built with Go generics, giving you full type safety without sacrificing ergonomics.

### Why resilix?

- **No framework lock-in** — resilix is a library, not a framework. Drop it into any Go project.
- **Generics-first API** — `Execute[T]` and `WithFallback[T]` return typed results directly. No `interface{}` boxing, no type assertions.
- **Zero core dependencies** — the core library imports only the Go standard library. The OpenTelemetry integration is an optional subpackage (`resilix/otel`).
- **Production-ready defaults** — `DefaultPipeline("my-service")` gives you a sensible Timeout → Retry → Circuit Breaker chain in a single line.
- **Fully testable** — inject `FakeClock` to make time-dependent tests instant and deterministic.

---

## Features

| Primitive | Description |
|---|---|
| **Circuit Breaker** | Trips when failure rate exceeds threshold; recovers via probe calls in half-open state |
| **Retry** | Re-executes on failure with pluggable backoff strategies and predicates |
| **Bulkhead** | Limits concurrent calls to protect goroutine pools from slow downstreams |
| **Timeout** | Cancels calls exceeding a deadline with no goroutine leaks |
| **Pipeline** | Chains primitives in order; generic `Execute[T]` for fully type-safe results |
| **Registry** | Thread-safe named store for any policy type — get-or-create with factories & hooks |

### Highlights

- **Generics-first** — `Execute[T]` and `WithFallback[T]` return typed results, no boxing or type assertions
- **Composable** — mix and match primitives via `Pipeline`, or use them standalone
- **Observable** — plug in OpenTelemetry, Prometheus, or any custom `Observer` implementation
- **Testable** — `FakeClock` makes time-dependent tests fast and deterministic
- **Zero dependencies** — core library has no external deps; OTel integration is opt-in

---

## How resilix compares

| Feature | **resilix** | sony/gobreaker | failsafe-go | hystrix-go |
|---|---|---|---|---|
| **Generics-first API** | ✅ `Execute[T]` — typed results, no boxing | ❌ `interface{}` | ✅ | ❌ `interface{}` |
| **Zero core dependencies** | ✅ stdlib only | ✅ stdlib only | ❌ | ❌ |
| **Circuit Breaker** | ✅ | ✅ | ✅ | ✅ |
| **Retry** | ✅ pluggable backoff & predicates | ❌ | ✅ | ❌ |
| **Bulkhead** | ✅ | ❌ | ✅ | ✅ (semaphore) |
| **Timeout** | ✅ | ❌ | ✅ | ✅ |
| **Composable Pipeline** | ✅ `DefaultPipeline` one-liner | ❌ | ❌ | ❌ |
| **Named Registry** | ✅ generic, thread-safe | ❌ | ❌ | ✅ |
| **OpenTelemetry** | ✅ opt-in subpackage | ❌ | ✅ | ❌ |
| **Custom Observer** | ✅ plug in any backend | ❌ | ✅ | ✅ (statsd) |
| **Sliding time window** | ✅ | ❌ count-only | ✅ | ❌ |
| **FakeClock for testing** | ✅ built-in | ❌ | ❌ | ❌ |
| **Active maintenance** | ✅ | ⚠️ minimal | ✅ | ❌ archived |
| **Chaos injection** | 🔜 `WithChaos()` — delays, errors & timeouts via probability config | ❌ | ❌ | ❌ |
| **Deadline propagation** | 🔜 `BudgetedTimeout` — caps child timeouts to parent context remainder | ❌ | ❌ | ❌ |

---

## Installation

```bash
go get github.com/ChiragRayani/resilix
```

Requires **Go 1.25+** (generics).

---

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/ChiragRayani/resilix"
)

func main() {
    // One-liner with sane defaults:
    // Timeout(5s) → Retry(3 attempts, exp backoff) → CircuitBreaker(50%, 30s)
    pipeline := resilix.DefaultPipeline("my-api")

    // Generic execute — full type safety, no boxing
    result, err := resilix.Execute(context.Background(), pipeline,
        func(ctx context.Context) (string, error) {
            return "hello from upstream!", nil
        },
    )
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(result) // "hello from upstream!"
}
```

---

## Custom Pipeline

Build a pipeline tailored to your downstream:

```go
pipeline := resilix.NewPipeline("payment-service",
    resilix.NewTimeout(resilix.TimeoutConfig{
        Duration: 3 * time.Second,
    }),
    resilix.NewRetry(resilix.RetryConfig{
        MaxAttempts: 3,
        Backoff:     resilix.ExponentialBackoffWithJitter(100*time.Millisecond, 2.0, 5*time.Second, 0.2),
        RetryIf:     myRetryPredicate,
    }),
    resilix.NewCircuitBreaker(resilix.CBConfig{
        FailureThreshold: 0.5,
        MinRequests:      5,
        OpenTimeout:      30 * time.Second,
        Window:           resilix.NewSlidingWindow(60*time.Second, 6),
    }),
    resilix.NewBulkhead(resilix.BulkheadConfig{
        MaxConcurrent: 20,
        WaitTimeout:   500 * time.Millisecond,
    }),
)
```

---

## Policy Ordering

Recommended ordering (outermost → innermost):

```
Timeout → Retry → CircuitBreaker → Bulkhead
```

| Position | Policy | Rationale |
|---|---|---|
| **Outermost** | Timeout | Governs the entire operation including all retries |
| | Retry | Re-attempts before hitting the circuit breaker each time |
| | Circuit Breaker | Short-circuits early when the downstream is known-bad |
| **Innermost** | Bulkhead | Limits simultaneous in-flight calls to the downstream |

---

## Registry — Named Policy Store

> See [examples/registry/main.go](examples/registry/main.go) for a full runnable demo.

`Registry[T]` is a generic, thread-safe named store that lets you create and reuse policies by name. No need to pass `*CircuitBreaker` instances around — just ask the registry.

### Circuit Breaker Registry

```go
cbRegistry := resilix.NewRegistry(
    resilix.WithDefaultFactory(func(name string) *resilix.CircuitBreaker {
        return resilix.NewCircuitBreaker(resilix.CBConfig{
            Name:             name,
            FailureThreshold: 0.5,
            MinRequests:      5,
            OpenTimeout:      30 * time.Second,
        })
    }),
)

// First call creates; subsequent calls return the same instance.
cb := cbRegistry.GetOrCreate("payments-api")
```

### Retry Registry

```go
retryRegistry := resilix.NewRegistry(
    resilix.WithDefaultFactory(func(name string) *resilix.Retry {
        return resilix.NewRetry(resilix.RetryConfig{
            Name:        name,
            MaxAttempts: 3,
            Backoff:     resilix.ExponentialBackoffWithJitter(100*time.Millisecond, 2.0, 10*time.Second, 0.2),
        })
    }),
)

r := retryRegistry.GetOrCreate("db-query")
```

### Bulkhead & Timeout Registries

```go
bhRegistry := resilix.NewRegistry(
    resilix.WithDefaultFactory(func(name string) *resilix.Bulkhead {
        return resilix.NewBulkhead(resilix.BulkheadConfig{
            Name:          name,
            MaxConcurrent: 20,
        })
    }),
)

toRegistry := resilix.NewRegistry(
    resilix.WithDefaultFactory(func(name string) *resilix.Timeout {
        return resilix.NewTimeout(resilix.TimeoutConfig{
            Name:     name,
            Duration: 5 * time.Second,
        })
    }),
)
```

### Pipeline Registry

```go
pipelineRegistry := resilix.NewRegistry(
    resilix.WithDefaultFactory(func(name string) *resilix.Pipeline {
        return resilix.DefaultPipeline(name)
    }),
)

pipeline := pipelineRegistry.GetOrCreate("user-service")
```

### Manual Registration & Hooks

```go
// Pre-build and register a custom policy
reg := resilix.NewRegistry(
    resilix.WithOnCreate(func(name string, cb *resilix.CircuitBreaker) {
        log.Printf("created circuit breaker: %s", name)
    }),
)

// Register a hand-built instance (returns false if name is taken)
reg.Register("auth-service", resilix.NewCircuitBreaker(resilix.CBConfig{
    Name:             "auth-service",
    FailureThreshold: 0.3,
    OpenTimeout:      60 * time.Second,
}))

// Lookup
cb, ok := reg.Get("auth-service")

// Snapshot of all entries
all := reg.All()

// Remove
reg.Remove("auth-service")
```

---

## Standalone Usage

Each primitive can also be used independently:

### Circuit Breaker

```go
cb := resilix.NewCircuitBreaker(resilix.CBConfig{
    FailureThreshold: 0.5,
    MinRequests:      5,
    OpenTimeout:      30 * time.Second,
    Window:           resilix.NewCountWindow(10),
})

err := cb.ExecuteCB(ctx, func(ctx context.Context) error {
    return callDownstream(ctx)
})
```

### Retry

```go
r := resilix.NewRetry(resilix.RetryConfig{
    MaxAttempts: 3,
    Backoff:     resilix.ExponentialBackoffWithJitter(100*time.Millisecond, 2.0, 10*time.Second, 0.2),
    RetryIf:     resilix.RetryAll,
})

err := r.ExecuteRetry(ctx, func(ctx context.Context) error {
    return callDownstream(ctx)
})
```

### Bulkhead

```go
bh := resilix.NewBulkhead(resilix.BulkheadConfig{
    MaxConcurrent: 20,
    WaitTimeout:   500 * time.Millisecond,
})

err := bh.ExecuteBulkhead(ctx, func(ctx context.Context) error {
    return callDownstream(ctx)
})
```

### Timeout

```go
to := resilix.NewTimeout(resilix.TimeoutConfig{
    Duration: 3 * time.Second,
})

err := to.ExecuteTimeout(ctx, func(ctx context.Context) error {
    return callDownstream(ctx)
})
```

---

## Failure Windows

Two implementations ship out of the box for the circuit breaker's failure tracking:

```go
// Count-based: tracks last N calls. Lightest option.
resilix.NewCountWindow(10)

// Sliding time-based: 60s window split into 6 × 10s buckets. More accurate under variable load.
resilix.NewSlidingWindow(60*time.Second, 6)

// Atomic counter: tracks consecutive failures. Use for "N consecutive failures" semantics.
// (implements FailureWindow)
&resilix.AtomicCounter{}
```

Implement the `resilix.FailureWindow` interface to plug in your own strategy (e.g., a Google SRE-style adaptive throttle).

---

## Backoff Strategies

```go
// Constant: same wait before every retry
resilix.ConstantBackoff(500 * time.Millisecond)

// Exponential: base * multiplier^(attempt-1), capped at max
resilix.ExponentialBackoff(100*time.Millisecond, 2.0, 10*time.Second)

// Exponential with jitter: ±20% random jitter to prevent thundering herd
resilix.ExponentialBackoffWithJitter(100*time.Millisecond, 2.0, 10*time.Second, 0.2)

// Linear: increases wait by step on each attempt
resilix.LinearBackoff(100*time.Millisecond, 200*time.Millisecond, 5*time.Second)
```

---

## Retry Predicates

Control which errors trigger a retry:

```go
// Retry any non-nil error (default)
resilix.RetryAll

// Never retry
resilix.RetryNone

// Retry only specific error types
resilix.RetryOn(mySpecificError)

// Compose predicates with OR semantics
resilix.RetryIf(
    resilix.RetryOn(errTimeout),
    resilix.RetryOn(errServiceUnavailable),
)
```

---

## Fallback / Graceful Degradation

```go
product, err := resilix.WithFallback(ctx, pipeline,
    func(ctx context.Context) (*Product, error) {
        return api.Get(ctx, id)
    },
    func(ctx context.Context, err error) (*Product, error) {
        return cache.Get(ctx, id) // serve stale data
    },
)
```

---

## Observability

### OpenTelemetry Integration

```go
import "github.com/ChiragRayani/resilix/otel"

obs, err := otel.NewObserver(meter, tracer)
if err != nil {
    log.Fatal(err)
}

cb := resilix.NewCircuitBreaker(resilix.CBConfig{
    Observer: obs,
    // ...other config
})
```

**Recorded metrics:**

| Metric | Type | Description |
|---|---|---|
| `resilix.executions` | Counter | Total calls, labelled by policy and result |
| `resilix.rejections` | Counter | Calls rejected without execution |
| `resilix.latency` | Histogram | Execution latency per policy (seconds) |
| `resilix.retries` | Counter | Retry attempts per policy |
| `resilix.state_changes` | Counter | Circuit breaker state transitions |

### Custom Observer

Implement the `resilix.Observer` interface to wire in Prometheus, structured logs, or any other system:

```go
type Observer interface {
    OnStateChange(policy string, from, to State)
    OnSuccess(policy string, latency time.Duration)
    OnFailure(policy string, err error, latency time.Duration)
    OnRejected(policy string)
    OnRetry(policy string, attempt int, err error)
}
```

---

## Testing

Use `testutil.FakeClock` to control time in circuit breaker and backoff tests — no `time.Sleep` required:

```go
import "github.com/ChiragRayani/resilix/testutil"

clock := testutil.NewFakeClock(time.Now())
cb := resilix.NewCircuitBreaker(resilix.CBConfig{
    Clock:       clock,
    OpenTimeout: 30 * time.Second,
})

// ... trip the breaker ...

// Jump past the timeout instantly — no waiting
clock.Advance(31 * time.Second)
// cb.State() == StateHalfOpen ✓
```

Run the test suite:

```bash
go test ./... -v
```

---

## Sentinel Errors

| Error | Returned when |
|---|---|
| `ErrCircuitOpen` | Call rejected because the circuit breaker is open |
| `ErrBulkheadFull` | No execution slot available in the bulkhead |
| `ErrMaxRetriesExceeded` | All retry attempts exhausted (wraps the last error) |
| `ErrTimeout` | Execution exceeded its deadline |

All errors support `errors.Is` and `errors.Unwrap` for idiomatic Go error handling.

---

## Project Structure

```
resilix/
├── types.go              # Core interfaces (Policy, Observer, Clock, FailureWindow) & errors
├── circuit_breaker.go    # Circuit Breaker implementation
├── retry.go              # Retry with pluggable backoff & predicates
├── bulkhead.go           # Semaphore-based concurrency limiter
├── timeout.go            # Context-based timeout with goroutine leak prevention
├── pipeline.go           # Pipeline, generic Execute[T], WithFallback[T], DefaultPipeline
├── registry.go           # Generic thread-safe Registry[T] for named policy management
├── window.go             # CountWindow, SlidingWindow, AtomicCounter
├── otel/
│   └── observer.go       # OpenTelemetry Observer implementation
├── testutil/
│   ├── fake_clock.go     # FakeClock for deterministic time control
│   ├── circuit_breaker_test.go  # Comprehensive test suite
│   └── registry_test.go  # Registry tests for all policy types
├── examples/
│   ├── basic/main.go     # Real-world HTTP client example
│   └── registry/main.go  # Registry usage patterns & global stores
├── go.mod
└── README.md
```

---

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

---

## License

This project is licensed under the MIT License — see the [LICENSE](LICENSE) file for details.
