package provider

import (
	"fmt"
	"sort"
)

// Factory creates a provider instance from the given config.
type Factory func(cfg *Config) (Provider, error)

var registry = map[string]Factory{}

// Register adds a provider factory to the registry.
func Register(name string, factory Factory) {
	registry[name] = factory
}

// Get returns a provider instance by name.
func Get(name string, cfg *Config) (Provider, error) {
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
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
