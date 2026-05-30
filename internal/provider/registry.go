package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/secrets"
	"github.com/phrony-platform/runtime/internal/store"
)

// Registry holds providers keyed by provider id (for example anthropic, openai).
type Registry struct {
	byID map[string]Provider
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byID: make(map[string]Provider)}
}

// Register adds a provider. It replaces any existing provider with the same ID.
func (r *Registry) Register(p Provider) {
	if r == nil || p == nil {
		return
	}
	r.byID[p.ID()] = p
}

// Get returns the provider for id, or false when not registered.
func (r *Registry) Get(id string) (Provider, bool) {
	if r == nil {
		return nil, false
	}
	p, ok := r.byID[id]
	return p, ok
}

// ModelProvider returns the provider configured on the agent manifest's spec.model.
func (r *Registry) ModelProvider(agent *manifest.Agent) (Provider, error) {
	if agent == nil {
		return nil, fmt.Errorf("agent manifest is required")
	}
	providerID := strings.TrimSpace(agent.Spec.Model.Provider)
	if providerID == "" {
		return nil, fmt.Errorf("spec.model.provider is required")
	}
	p, ok := r.Get(providerID)
	if !ok {
		return nil, fmt.Errorf("provider %q is not registered", providerID)
	}
	return p, nil
}

// NewForAgentVersion builds a registry with the model provider configured from secrets
// decrypted for the given agent version.
func NewForAgentVersion(
	ctx context.Context,
	enc *secrets.Encryptor,
	q *store.Queries,
	agentVersionID string,
	agent *manifest.Agent,
) (*Registry, error) {
	if agent == nil {
		return nil, fmt.Errorf("agent manifest is required")
	}
	providerID := strings.TrimSpace(agent.Spec.Model.Provider)
	if providerID == "" {
		return nil, fmt.Errorf("spec.model.provider is required")
	}

	apiKey, err := APIKeyForModel(ctx, enc, q, agentVersionID, agent)
	if err != nil {
		return nil, err
	}
	defer zeroString(apiKey)

	p, err := New(providerID, apiKey)
	if err != nil {
		return nil, err
	}
	reg := NewRegistry()
	reg.Register(p)
	return reg, nil
}

// New constructs a provider implementation for the given id and API key.
func New(id, apiKey string) (Provider, error) {
	switch strings.TrimSpace(id) {
	case IDAnthropic:
		return newAnthropicProvider(apiKey), nil
	case IDOpenAI:
		return newOpenAIProvider(apiKey), nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", id)
	}
}
