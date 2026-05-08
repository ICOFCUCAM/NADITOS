package connectors

import (
	"errors"
	"sync"
)

// CountryRouter picks the right provider implementation for a (tenant,
// module) pair. Adapters register themselves at boot; a service asks for
// the active one before each call.
//
// Default fallback wins if no per-tenant binding exists.
type CountryRouter[T any] struct {
	mu      sync.RWMutex
	default_ T
	hasDefault bool
	byTenant map[string]T
}

func NewRouter[T any]() *CountryRouter[T] {
	return &CountryRouter[T]{byTenant: map[string]T{}}
}

func (r *CountryRouter[T]) SetDefault(p T) {
	r.mu.Lock()
	r.default_ = p
	r.hasDefault = true
	r.mu.Unlock()
}

func (r *CountryRouter[T]) Bind(tenantID string, p T) {
	r.mu.Lock()
	r.byTenant[tenantID] = p
	r.mu.Unlock()
}

func (r *CountryRouter[T]) For(tenantID string) (T, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if p, ok := r.byTenant[tenantID]; ok {
		return p, nil
	}
	if r.hasDefault {
		return r.default_, nil
	}
	var zero T
	return zero, errors.New("connectors: no provider bound for tenant")
}
