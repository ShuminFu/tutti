package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// A refreshed extension may coexist with sessions launched from its previous
// installation. Keep both adapters and route every session by its launch ref.
func adapterBindingKey(input AdapterResolveInput) string {
	provider := strings.TrimSpace(input.Provider)
	if providerTargetRefString(input.ProviderTargetRef, "kind") != "agent_extension" {
		return provider
	}
	key, _ := json.Marshal([3]string{provider, strings.TrimSpace(input.AgentTargetID),
		providerTargetRefString(input.ProviderTargetRef, "extensionInstallationId")})
	return string(key)
}

func adapterMatchesInput(adapter Adapter, input AdapterResolveInput) bool {
	if adapter == nil {
		return false
	}
	bound, ok := adapter.(ResolveInputBoundAdapter)
	return !ok || bound.MatchesAdapterResolveInput(input)
}

func (c *Controller) adapterForSessionLocked(session Session) Adapter {
	input := AdapterResolveInput{Provider: session.Provider, AgentTargetID: session.AgentTargetID,
		CWD: session.CWD, ProviderTargetRef: session.ProviderTargetRef}
	adapter := c.adapters[adapterBindingKey(input)]
	if adapter == nil {
		adapter = c.adapters[session.Provider]
	}
	if !adapterMatchesInput(adapter, input) {
		return nil
	}
	return adapter
}

func (c *Controller) adapterForSession(session Session) Adapter {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.adapterForSessionLocked(session)
}

func (c *Controller) resolveAdapter(ctx context.Context, input AdapterResolveInput) (Adapter, error) {
	if c == nil {
		return nil, nil
	}
	provider := strings.TrimSpace(input.Provider)
	key := adapterBindingKey(input)
	c.mu.Lock()
	adapter := c.adapters[key]
	if adapter == nil {
		adapter = c.adapters[provider]
	}
	c.mu.Unlock()
	if adapterMatchesInput(adapter, input) {
		return adapter, nil
	}
	if c.adapterResolver == nil {
		if adapter != nil {
			return nil, fmt.Errorf("cached adapter binding mismatch for %q", provider)
		}
		return nil, nil
	}
	adapter, err := c.adapterResolver.ResolveAdapter(ctx, input)
	if err != nil {
		return nil, err
	}
	if adapter == nil || strings.TrimSpace(adapter.Provider()) != provider || !adapterMatchesInput(adapter, input) {
		return nil, fmt.Errorf("resolved adapter binding mismatch for %q", provider)
	}
	c.configureAdapter(adapter)
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing := c.adapters[key]; existing != nil {
		if !adapterMatchesInput(existing, input) {
			return nil, fmt.Errorf("cached adapter binding mismatch for %q", provider)
		}
		return existing, nil
	}
	c.adapters[key] = adapter
	return adapter, nil
}
