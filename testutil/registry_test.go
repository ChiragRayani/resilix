package testutil

import (
	"sync"
	"testing"
	"time"

	"github.com/ChiragRayani/resilix"
)

// ---------- CircuitBreaker registry tests ----------

func TestRegistry_CircuitBreaker_GetOrCreate(t *testing.T) {
	reg := resilix.NewRegistry(
		resilix.WithDefaultFactory(func(name string) *resilix.CircuitBreaker {
			return resilix.NewCircuitBreaker(resilix.CBConfig{
				Name:             name,
				FailureThreshold: 0.5,
				MinRequests:      5,
				OpenTimeout:      30 * time.Second,
			})
		}),
	)

	cb1 := reg.GetOrCreate("payments")
	cb2 := reg.GetOrCreate("payments")

	if cb1 != cb2 {
		t.Fatal("expected same instance for the same name")
	}
	if cb1.Name() != "payments" {
		t.Fatalf("expected name %q, got %q", "payments", cb1.Name())
	}
}

func TestRegistry_CircuitBreaker_Register(t *testing.T) {
	reg := resilix.NewRegistry[*resilix.CircuitBreaker]()

	cb := resilix.NewCircuitBreaker(resilix.CBConfig{Name: "auth-service"})
	ok := reg.Register("auth", cb)
	if !ok {
		t.Fatal("expected Register to succeed on first call")
	}
	ok = reg.Register("auth", resilix.NewCircuitBreaker(resilix.CBConfig{Name: "other"}))
	if ok {
		t.Fatal("expected Register to return false when name already taken")
	}
	got, found := reg.Get("auth")
	if !found || got != cb {
		t.Fatal("expected to retrieve the originally registered instance")
	}
}

func TestRegistry_CircuitBreaker_Get_NotFound(t *testing.T) {
	reg := resilix.NewRegistry[*resilix.CircuitBreaker]()

	_, found := reg.Get("nonexistent")
	if found {
		t.Fatal("expected Get to return false for missing key")
	}
}

func TestRegistry_CircuitBreaker_Remove(t *testing.T) {
	reg := resilix.NewRegistry[*resilix.CircuitBreaker]()

	cb := resilix.NewCircuitBreaker(resilix.CBConfig{Name: "temp"})
	reg.Register("temp", cb)
	reg.Remove("temp")

	_, found := reg.Get("temp")
	if found {
		t.Fatal("expected entry to be removed")
	}
}

func TestRegistry_CircuitBreaker_All(t *testing.T) {
	reg := resilix.NewRegistry[*resilix.CircuitBreaker]()

	cb1 := resilix.NewCircuitBreaker(resilix.CBConfig{Name: "svc-a"})
	cb2 := resilix.NewCircuitBreaker(resilix.CBConfig{Name: "svc-b"})
	reg.Register("a", cb1)
	reg.Register("b", cb2)

	all := reg.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(all))
	}

	// Mutating the returned map should not affect the registry
	delete(all, "a")
	if _, found := reg.Get("a"); !found {
		t.Fatal("deleting from All() snapshot should not affect registry")
	}
}

// ---------- Retry registry tests ----------

func TestRegistry_Retry_GetOrCreate(t *testing.T) {
	reg := resilix.NewRegistry(
		resilix.WithDefaultFactory(func(name string) *resilix.Retry {
			return resilix.NewRetry(resilix.RetryConfig{
				Name:        name,
				MaxAttempts: 3,
			})
		}),
	)

	r1 := reg.GetOrCreate("db-query")
	r2 := reg.GetOrCreate("db-query")

	if r1 != r2 {
		t.Fatal("expected same instance for the same name")
	}
	if r1.Name() != "db-query" {
		t.Fatalf("expected name %q, got %q", "db-query", r1.Name())
	}

	r3 := reg.GetOrCreate("api-call")
	if r1 == r3 {
		t.Fatal("expected different instances for different names")
	}
}

// ---------- Bulkhead registry tests ----------

func TestRegistry_Bulkhead_GetOrCreate(t *testing.T) {
	reg := resilix.NewRegistry(
		resilix.WithDefaultFactory(func(name string) *resilix.Bulkhead {
			return resilix.NewBulkhead(resilix.BulkheadConfig{
				Name:          name,
				MaxConcurrent: 10,
			})
		}),
	)

	bh := reg.GetOrCreate("downstream")
	if bh.Name() != "downstream" {
		t.Fatalf("expected name %q, got %q", "downstream", bh.Name())
	}
}

// ---------- Timeout registry tests ----------

func TestRegistry_Timeout_GetOrCreate(t *testing.T) {
	reg := resilix.NewRegistry(
		resilix.WithDefaultFactory(func(name string) *resilix.Timeout {
			return resilix.NewTimeout(resilix.TimeoutConfig{
				Name:     name,
				Duration: 5 * time.Second,
			})
		}),
	)

	to := reg.GetOrCreate("slow-api")
	if to.Name() != "slow-api" {
		t.Fatalf("expected name %q, got %q", "slow-api", to.Name())
	}
}

// ---------- Pipeline registry tests ----------

func TestRegistry_Pipeline_GetOrCreate(t *testing.T) {
	reg := resilix.NewRegistry(
		resilix.WithDefaultFactory(func(name string) *resilix.Pipeline {
			return resilix.DefaultPipeline(name)
		}),
	)

	p1 := reg.GetOrCreate("user-service")
	p2 := reg.GetOrCreate("user-service")
	if p1 != p2 {
		t.Fatal("expected same pipeline instance")
	}
	if p1.Name() != "user-service" {
		t.Fatalf("expected name %q, got %q", "user-service", p1.Name())
	}
}

// ---------- OnCreate hook tests ----------

func TestRegistry_OnCreateHook(t *testing.T) {
	var created []string
	reg := resilix.NewRegistry(
		resilix.WithDefaultFactory(func(name string) *resilix.CircuitBreaker {
			return resilix.NewCircuitBreaker(resilix.CBConfig{Name: name})
		}),
		resilix.WithOnCreate(func(name string, _ *resilix.CircuitBreaker) {
			created = append(created, name)
		}),
	)

	reg.GetOrCreate("alpha")
	reg.GetOrCreate("beta")
	reg.GetOrCreate("alpha") // duplicate — should not trigger hook

	if len(created) != 2 {
		t.Fatalf("expected 2 hook calls, got %d", len(created))
	}
	if created[0] != "alpha" || created[1] != "beta" {
		t.Fatalf("unexpected hook order: %v", created)
	}
}

// ---------- Panic on missing factory ----------

func TestRegistry_GetOrCreate_PanicsWithoutFactory(t *testing.T) {
	reg := resilix.NewRegistry[*resilix.CircuitBreaker]()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic when calling GetOrCreate without a factory")
		}
	}()

	reg.GetOrCreate("will-panic")
}

// ---------- Concurrent safety ----------

func TestRegistry_ConcurrentGetOrCreate(t *testing.T) {
	reg := resilix.NewRegistry(
		resilix.WithDefaultFactory(func(name string) *resilix.CircuitBreaker {
			return resilix.NewCircuitBreaker(resilix.CBConfig{Name: name})
		}),
	)

	const goroutines = 100
	results := make([]*resilix.CircuitBreaker, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = reg.GetOrCreate("shared")
		}(i)
	}
	wg.Wait()

	// All goroutines must get the exact same instance
	for i := 1; i < goroutines; i++ {
		if results[i] != results[0] {
			t.Fatalf("goroutine %d got a different instance", i)
		}
	}
}

func TestRegistry_ConcurrentMixedOps(t *testing.T) {
	reg := resilix.NewRegistry(
		resilix.WithDefaultFactory(func(name string) *resilix.Retry {
			return resilix.NewRetry(resilix.RetryConfig{Name: name})
		}),
	)

	var wg sync.WaitGroup
	wg.Add(4)

	// Writer 1: Register
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			reg.Register("manual", resilix.NewRetry(resilix.RetryConfig{Name: "manual"}))
		}
	}()

	// Writer 2: GetOrCreate
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			reg.GetOrCreate("auto")
		}
	}()

	// Reader
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			reg.Get("auto")
			reg.All()
		}
	}()

	// Deleter
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			reg.Remove("auto")
		}
	}()

	wg.Wait()
}
