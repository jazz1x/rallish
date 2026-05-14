// Package adapter defines the adapter interface and registry for agent runtimes.
package adapter

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/jazz1x/hocketty/pkg/contract"
)

// Adapter is the common interface for agent runtimes.
type Adapter interface {
	Name() string
	Run(ctx context.Context, req contract.TurnRequest) (contract.TurnResponse, error)
}

// Registry stores and retrieves named adapters.
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		adapters: make(map[string]Adapter),
	}
}

// Register adds an adapter to the registry. It returns an error if the name is already taken.
func (r *Registry) Register(name string, a Adapter) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.adapters[name]; exists {
		return fmt.Errorf("adapter %q already registered", name)
	}
	r.adapters[name] = a
	return nil
}

// Get retrieves an adapter by name.
func (r *Registry) Get(name string) (Adapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[name]
	return a, ok
}

// Names returns a sorted list of registered adapter names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.adapters))
	for n := range r.adapters {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

type checker interface {
	Check() error
}

// CheckAll calls Check() on every adapter that implements it.
// For adapters without a Check method it records a nil error.
func (r *Registry) CheckAll(ctx context.Context) map[string]error {
	_ = ctx
	r.mu.RLock()
	defer r.mu.RUnlock()
	results := make(map[string]error, len(r.adapters))
	for name, a := range r.adapters {
		if c, ok := a.(checker); ok {
			results[name] = c.Check()
		} else {
			results[name] = nil
		}
	}
	return results
}
