package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/ChiragRayani/resilix"
)

// ─────────────────────────────────────────────────────────────────────────────
// Global registries — create once at startup, use everywhere.
// ─────────────────────────────────────────────────────────────────────────────

// cbRegistry is a global circuit breaker registry with a default factory.
// Any goroutine or handler can call cbRegistry.GetOrCreate("service-name")
// and get back the same *CircuitBreaker instance, created on first access.
var cbRegistry = resilix.NewRegistry(
	resilix.WithDefaultFactory(func(name string) *resilix.CircuitBreaker {
		return resilix.NewCircuitBreaker(resilix.CBConfig{
			Name:             name,
			FailureThreshold: 0.5,
			MinRequests:      5,
			OpenTimeout:      30 * time.Second,
			Window:           resilix.NewSlidingWindow(60*time.Second, 6),
		})
	}),
	resilix.WithOnCreate(func(name string, _ *resilix.CircuitBreaker) {
		log.Printf("[registry] created circuit breaker: %s", name)
	}),
)

// retryRegistry is a global retry policy registry.
var retryRegistry = resilix.NewRegistry(
	resilix.WithDefaultFactory(func(name string) *resilix.Retry {
		return resilix.NewRetry(resilix.RetryConfig{
			Name:        name,
			MaxAttempts: 3,
			Backoff:     resilix.ExponentialBackoffWithJitter(100*time.Millisecond, 2.0, 10*time.Second, 0.2),
		})
	}),
	resilix.WithOnCreate(func(name string, _ *resilix.Retry) {
		log.Printf("[registry] created retry policy: %s", name)
	}),
)

// pipelineRegistry creates full default pipelines on demand.
var pipelineRegistry = resilix.NewRegistry(
	resilix.WithDefaultFactory(func(name string) *resilix.Pipeline {
		return resilix.DefaultPipeline(name)
	}),
	resilix.WithOnCreate(func(name string, _ *resilix.Pipeline) {
		log.Printf("[registry] created pipeline: %s", name)
	}),
)

// ─────────────────────────────────────────────────────────────────────────────
// Simulated services that use registries
// ─────────────────────────────────────────────────────────────────────────────

// PaymentService demonstrates using a circuit breaker from the global registry.
type PaymentService struct{}

func (s *PaymentService) Charge(ctx context.Context, amount int) error {
	// GetOrCreate returns the same CB instance every time for "payments-api".
	cb := cbRegistry.GetOrCreate("payments-api")

	return cb.ExecuteCB(ctx, func(ctx context.Context) error {
		log.Printf("[payment] charging $%d...", amount)
		// Simulate a successful call
		return nil
	})
}

// UserService demonstrates using a retry policy from the global registry.
type UserService struct{}

func (s *UserService) GetUser(ctx context.Context, userID string) (string, error) {
	r := retryRegistry.GetOrCreate("user-api")

	var result string
	err := r.ExecuteRetry(ctx, func(ctx context.Context) error {
		log.Printf("[user] fetching user %s...", userID)
		// Simulate success
		result = fmt.Sprintf("User-%s", userID)
		return nil
	})
	return result, err
}

// OrderService demonstrates using a full pipeline from the registry.
type OrderService struct{}

func (s *OrderService) PlaceOrder(ctx context.Context, orderID string) (string, error) {
	pipeline := pipelineRegistry.GetOrCreate("order-api")

	return resilix.Execute(ctx, pipeline, func(ctx context.Context) (string, error) {
		log.Printf("[order] placing order %s...", orderID)
		return fmt.Sprintf("order-%s-confirmed", orderID), nil
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Manual registration example
// ─────────────────────────────────────────────────────────────────────────────

func registerCustomPolicies() {
	// Register a hand-built circuit breaker with custom settings
	// for a critical downstream that needs tighter thresholds.
	cbRegistry.Register("auth-service", resilix.NewCircuitBreaker(resilix.CBConfig{
		Name:             "auth-service",
		FailureThreshold: 0.3,  // trip at 30% instead of the default 50%
		MinRequests:      3,    // fewer minimum requests before evaluation
		OpenTimeout:      60 * time.Second, // stay open longer
		Window:           resilix.NewCountWindow(20),
	}))
	log.Println("[registry] manually registered auth-service circuit breaker")
}

// ─────────────────────────────────────────────────────────────────────────────
// WithFallback + registry example
// ─────────────────────────────────────────────────────────────────────────────

func fetchWithFallback(ctx context.Context) (string, error) {
	pipeline := pipelineRegistry.GetOrCreate("catalog-api")

	return resilix.WithFallback(ctx, pipeline,
		func(ctx context.Context) (string, error) {
			// Primary path: call the catalog API
			return "", errors.New("catalog-api is down")
		},
		func(ctx context.Context, err error) (string, error) {
			// Fallback: return cached data when the pipeline fails
			log.Printf("[fallback] catalog-api failed (%v), serving cached data", err)
			return "cached-product-list", nil
		},
	)
}

// ─────────────────────────────────────────────────────────────────────────────
// Inspect the registry
// ─────────────────────────────────────────────────────────────────────────────

func printRegistryStatus() {
	fmt.Println("\n── Circuit Breaker Registry ──")
	for name, cb := range cbRegistry.All() {
		fmt.Printf("  %-20s state=%s\n", name, cb.State())
	}

	fmt.Println("\n── Retry Registry ──")
	for name, r := range retryRegistry.All() {
		fmt.Printf("  %-20s name=%s\n", name, r.Name())
	}

	fmt.Println("\n── Pipeline Registry ──")
	for name, p := range pipelineRegistry.All() {
		fmt.Printf("  %-20s name=%s\n", name, p.Name())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// main
// ─────────────────────────────────────────────────────────────────────────────

func main() {
	ctx := context.Background()

	// Step 1: Manually register a custom circuit breaker for auth-service
	registerCustomPolicies()

	// Step 2: Services use registries — no wiring needed
	payments := &PaymentService{}
	users := &UserService{}
	orders := &OrderService{}

	fmt.Println("\n═══ Using Services ═══")

	// Payment service uses the circuit breaker registry
	if err := payments.Charge(ctx, 99); err != nil {
		log.Printf("payment failed: %v", err)
	}

	// User service uses the retry registry
	user, err := users.GetUser(ctx, "42")
	if err != nil {
		log.Printf("user fetch failed: %v", err)
	} else {
		fmt.Printf("  got: %s\n", user)
	}

	// Order service uses the pipeline registry
	order, err := orders.PlaceOrder(ctx, "ORD-001")
	if err != nil {
		log.Printf("order failed: %v", err)
	} else {
		fmt.Printf("  got: %s\n", order)
	}

	// Step 3: Calling GetOrCreate again returns the SAME instance
	fmt.Println("\n═══ Instance Identity ═══")
	cb1 := cbRegistry.GetOrCreate("payments-api")
	cb2 := cbRegistry.GetOrCreate("payments-api")
	fmt.Printf("  same instance? %v\n", cb1 == cb2) // true

	// Step 4: Fallback with registry-managed pipeline
	fmt.Println("\n═══ Fallback Demo ═══")
	data, err := fetchWithFallback(ctx)
	if err != nil {
		log.Printf("fallback also failed: %v", err)
	} else {
		fmt.Printf("  got: %s\n", data)
	}

	// Step 5: Inspect all registered policies
	printRegistryStatus()

	// Step 6: Cleanup — remove a policy when a service is decommissioned
	fmt.Println("\n═══ Removing auth-service ═══")
	cbRegistry.Remove("auth-service")
	_, found := cbRegistry.Get("auth-service")
	fmt.Printf("  auth-service still in registry? %v\n", found) // false
}
