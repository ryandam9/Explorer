package services

import "sync"

// Registry holds the registered collectors.
type Registry struct {
	mu         sync.RWMutex
	collectors map[string]Collector
}

// NewRegistry creates a new registry.
func NewRegistry() *Registry {
	return &Registry{
		collectors: make(map[string]Collector),
	}
}

// Register adds a collector to the registry.
func (r *Registry) Register(c Collector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.collectors[c.Name()] = c
}

// Get returns a collector by name.
func (r *Registry) Get(name string) (Collector, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.collectors[name]
	return c, ok
}

// GetAll returns all registered collectors.
func (r *Registry) GetAll() []Collector {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []Collector
	for _, c := range r.collectors {
		all = append(all, c)
	}
	return all
}
