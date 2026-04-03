package resilix

import (
	"sync"
)

// RegistryOption configures a typed registry at creation time.
type RegistryOption[T any] func(*registryConfig[T])

type registryConfig[T any] struct {
	defaultFactory func(name string) T
	onCreate       func(name string, policy T) // hook: fires after each creation
}

// WithDefaultFactory sets the function used to create a new policy
// when GetOrCreate is called for a name that doesn't exist yet.
func WithDefaultFactory[T any](f func(name string) T) RegistryOption[T] {
	return func(c *registryConfig[T]) { c.defaultFactory = f }
}

// WithOnCreate registers a hook called after each new policy is created.
// Useful for logging, registering with a metrics system, etc.
func WithOnCreate[T any](f func(name string, policy T)) RegistryOption[T] {
	return func(c *registryConfig[T]) { c.onCreate = f }
}

// Registry is a generic thread-safe named policy store.
// T is the policy type: *CircuitBreaker, *Retry, *Pipeline, etc.
type Registry[T any] struct {
	mu      sync.RWMutex
	entries map[string]T
	cfg     registryConfig[T]
}

// NewRegistry creates a new typed registry with the given options.
func NewRegistry[T any](opts ...RegistryOption[T]) *Registry[T] {
	r := &Registry[T]{entries: make(map[string]T)}
	for _, o := range opts {
		o(&r.cfg)
	}
	return r
}

// GetOrCreate returns the policy registered under name, creating it
// with the default factory if it doesn't exist yet.
// It is safe for concurrent use — only one instance is ever created per name.
func (r *Registry[T]) GetOrCreate(name string) T {
	// fast path: already exists
	r.mu.RLock()
	if p, ok := r.entries[name]; ok {
		r.mu.RUnlock()
		return p
	}
	r.mu.RUnlock()

	// slow path: create under write lock
	r.mu.Lock()
	defer r.mu.Unlock()

	// double-check after acquiring write lock
	if p, ok := r.entries[name]; ok {
		return p
	}

	if r.cfg.defaultFactory == nil {
		panic("resilix: Registry.GetOrCreate called with no default factory set for name: " + name)
	}

	p := r.cfg.defaultFactory(name)
	r.entries[name] = p

	if r.cfg.onCreate != nil {
		r.cfg.onCreate(name, p)
	}
	return p
}

// Register stores a pre-built policy under name.
// Returns false (and does not overwrite) if the name is already taken.
func (r *Registry[T]) Register(name string, policy T) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.entries[name]; exists {
		return false
	}
	r.entries[name] = policy
	return true
}

// Get returns the policy for name and whether it was found.
func (r *Registry[T]) Get(name string) (T, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.entries[name]
	return p, ok
}

// Remove deletes a policy from the registry.
func (r *Registry[T]) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, name)
}

// All returns a snapshot of all registered policies.
func (r *Registry[T]) All() map[string]T {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]T, len(r.entries))
	for k, v := range r.entries {
		out[k] = v
	}
	return out
}
