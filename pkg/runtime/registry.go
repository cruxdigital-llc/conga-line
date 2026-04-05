package runtime

import (
	"fmt"
	"sort"
)

// Factory creates a runtime instance.
type Factory func() Runtime

var registry = map[RuntimeName]Factory{}

// Register adds a runtime factory to the registry. Panics on duplicate.
func Register(name RuntimeName, factory Factory) {
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("runtime: duplicate registration %q", name))
	}
	registry[name] = factory
}

// Get returns a runtime instance by name.
func Get(name RuntimeName) (Runtime, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown runtime %q (available: %v)", name, Names())
	}
	return factory(), nil
}

// Names returns all registered runtime names, sorted.
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, string(name))
	}
	sort.Strings(names)
	return names
}
