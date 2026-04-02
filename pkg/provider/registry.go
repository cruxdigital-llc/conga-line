package provider

import (
	"fmt"
	"sort"
)

// Factory creates a provider instance from the given config.
type Factory func(cfg *Config) (Provider, error)

var registry = map[ProviderName]Factory{}

// Register adds a provider factory to the registry. Panics on duplicate.
func Register(name ProviderName, factory Factory) {
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("provider: duplicate registration %q", name))
	}
	registry[name] = factory
}

// Get returns a provider instance by name.
func Get(name ProviderName, cfg *Config) (Provider, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q (available: %v)", name, Names())
	}
	return factory(cfg)
}

// Names returns all registered provider names, sorted.
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, string(name))
	}
	sort.Strings(names)
	return names
}
