/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package action

import (
	"fmt"
	"sort"
	"sync"
)

// Registry is a concurrency-safe set of action plugins keyed by name.
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]Action
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{plugins: make(map[string]Action)}
}

// Register adds a plugin. It panics if a plugin with the same name is already
// registered, since duplicate registration is always a programming error.
func (r *Registry) Register(a Action) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := a.Name()
	if _, exists := r.plugins[name]; exists {
		panic(fmt.Sprintf("action plugin %q already registered", name))
	}
	r.plugins[name] = a
}

// Lookup returns the plugin registered under name, or nil if there is none.
func (r *Registry) Lookup(name string) Action {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.plugins[name]
}

// All returns the registered plugins sorted by name.
func (r *Registry) All() []Action {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Action, 0, len(r.plugins))
	for _, a := range r.plugins {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// defaultRegistry is the process-global registry that built-in plugins register
// into via their package init functions (see docs/ARCHITECTURE.md §3.1).
var defaultRegistry = NewRegistry()

// Default returns the process-global registry.
func Default() *Registry { return defaultRegistry }

// Register adds a plugin to the default registry.
func Register(a Action) { defaultRegistry.Register(a) }

// Lookup looks a plugin up in the default registry.
func Lookup(name string) Action { return defaultRegistry.Lookup(name) }

// All returns every plugin in the default registry.
func All() []Action { return defaultRegistry.All() }
