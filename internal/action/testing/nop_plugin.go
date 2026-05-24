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

// Package testing provides fakes for exercising the action framework in tests.
package testing

import (
	"context"
	"sync/atomic"
	"time"

	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/Vedooo/reactive-policy/internal/action"
)

// Nop is a configurable, recording action plugin for tests. Construct it with
// NewNop; the zero value is not usable.
type Nop struct {
	name        string
	description string
	reversible  bool

	// ValidateErr, ExecuteErr and ReverseErr, when set, are returned by the
	// corresponding methods so tests can drive failure paths.
	ValidateErr error
	ExecuteErr  error
	ReverseErr  error

	// Permissions is returned verbatim by RequiredPermissions.
	Permissions []rbacv1.PolicyRule

	executeCount int64
	reverseCount int64
}

// NewNop returns a Nop plugin registered under name. It reports itself
// reversible and all methods succeed unless the matching *Err field is set.
func NewNop(name string) *Nop {
	return &Nop{name: name, description: "no-op test plugin", reversible: true}
}

// SetReversible overrides whether the plugin reports itself reversible and
// returns the plugin for chaining.
func (n *Nop) SetReversible(v bool) *Nop {
	n.reversible = v
	return n
}

// Name returns the plugin name.
func (n *Nop) Name() string { return n.name }

// Description returns the plugin description.
func (n *Nop) Description() string { return n.description }

// Validate returns the configured ValidateErr.
func (n *Nop) Validate(action.Params) error { return n.ValidateErr }

// Execute records the call and returns a Result reflecting ExecuteErr.
func (n *Nop) Execute(_ context.Context, in action.ExecuteInput) (action.Result, error) {
	atomic.AddInt64(&n.executeCount, 1)
	if n.ExecuteErr != nil {
		return action.Result{PluginName: n.name, Status: action.StatusFailed, Message: n.ExecuteErr.Error()}, n.ExecuteErr
	}
	return action.Result{
		PluginName: n.name,
		Target:     in.Target,
		Timestamp:  time.Now(),
		Status:     action.StatusSucceeded,
		Message:    "nop executed",
	}, nil
}

// Reverse records the call and returns the configured ReverseErr.
func (n *Nop) Reverse(context.Context, action.Result) error {
	atomic.AddInt64(&n.reverseCount, 1)
	return n.ReverseErr
}

// IsReversible reports the configured reversibility.
func (n *Nop) IsReversible() bool { return n.reversible }

// RequiredPermissions returns the configured Permissions.
func (n *Nop) RequiredPermissions() []rbacv1.PolicyRule { return n.Permissions }

// ExecuteCount returns how many times Execute has been called.
func (n *Nop) ExecuteCount() int { return int(atomic.LoadInt64(&n.executeCount)) }

// ReverseCount returns how many times Reverse has been called.
func (n *Nop) ReverseCount() int { return int(atomic.LoadInt64(&n.reverseCount)) }

// NewFakeRegistry returns a registry pre-populated with the given plugins.
func NewFakeRegistry(plugins ...action.Action) *action.Registry {
	r := action.NewRegistry()
	for _, p := range plugins {
		r.Register(p)
	}
	return r
}

// MockTarget returns a Target with the given kind and name in the default
// namespace.
func MockTarget(kind, name string) action.Target {
	return action.Target{
		APIVersion: "apps/v1",
		Kind:       kind,
		Name:       name,
		Namespace:  "default",
	}
}
