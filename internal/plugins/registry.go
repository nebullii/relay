package plugins

import (
	"encoding/json"
	"fmt"
)

// Capability is a named, typed tool that agents can invoke.
type Capability struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	ArgsSchema  json.RawMessage `json:"args_schema"`
	Cacheable   bool            `json:"cacheable"`
	CacheTTLSec int             `json:"cache_ttl_sec,omitempty"`
}

// InvokeRequest is the input to a capability invocation.
type InvokeRequest struct {
	Capability     string          `json:"capability"`
	ThreadID       string          `json:"thread_id"`
	Args           json.RawMessage `json:"args"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Tenant         string          `json:"tenant,omitempty"`
}

// InvokeResult is the output of a capability invocation.
type InvokeResult struct {
	Capability  string          `json:"capability"`
	Preview     json.RawMessage `json:"preview"`
	ArtifactRef string          `json:"artifact_ref,omitempty"`
	CacheHit    bool            `json:"cache_hit"`
	CacheKey    string          `json:"cache_key,omitempty"`
	DurationMs  int64           `json:"duration_ms"`
}

// Handler is the function signature for capability handlers.
type Handler func(req *InvokeRequest) (*InvokeResult, error)

// Registry holds all registered capabilities.
type Registry struct {
	capabilities map[string]*Capability
	handlers     map[string]Handler
}

func NewRegistry() *Registry {
	return &Registry{
		capabilities: make(map[string]*Capability),
		handlers:     make(map[string]Handler),
	}
}

// Register adds a capability to the registry.
func (r *Registry) Register(cap *Capability, handler Handler) error {
	if cap.Name == "" {
		return fmt.Errorf("capability name is required")
	}
	if _, exists := r.capabilities[cap.Name]; exists {
		return fmt.Errorf("capability %q already registered", cap.Name)
	}
	r.capabilities[cap.Name] = cap
	r.handlers[cap.Name] = handler
	return nil
}

// Get retrieves a capability by name.
func (r *Registry) Get(name string) (*Capability, Handler, error) {
	cap, ok := r.capabilities[name]
	if !ok {
		return nil, nil, fmt.Errorf("capability %q not found", name)
	}
	return cap, r.handlers[name], nil
}

// List returns all registered capabilities.
func (r *Registry) List() []*Capability {
	caps := make([]*Capability, 0, len(r.capabilities))
	for _, c := range r.capabilities {
		caps = append(caps, c)
	}
	return caps
}
