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

// Package action defines the contract every action plugin implements and the
// machinery that runs a policy's action pipeline when it triggers. See
// docs/PLUGIN_INTERFACE.md for the full plugin specification.
package action

import (
	"context"
	"errors"
	"time"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Action is the contract every action plugin implements. Plugins are stateless
// and must be safe for concurrent use (see docs/ARCHITECTURE.md §7).
type Action interface {
	// Name is the registered plugin name in <category>.<verb> form.
	Name() string
	// Description is a one-line human-readable summary.
	Description() string
	// Validate checks the params at admission time; it must not have side effects.
	Validate(params Params) error
	// Execute performs the action and returns a Result describing what happened.
	Execute(ctx context.Context, in ExecuteInput) (Result, error)
	// Reverse undoes a previous successful execution described by prev.
	Reverse(ctx context.Context, prev Result) error
	// IsReversible reports whether Reverse is supported.
	IsReversible() bool
	// RequiredPermissions are the RBAC rules the plugin needs at runtime.
	RequiredPermissions() []rbacv1.PolicyRule
}

// Params holds plugin-specific parameters as raw JSON keyed by field name.
type Params map[string]runtime.RawExtension

// ExecuteInput carries everything a plugin needs for a single execution.
type ExecuteInput struct {
	// Target is the resource the action operates on.
	Target Target
	// Params are the plugin-specific parameters from the policy.
	Params Params
	// PolicyName and Namespace identify the triggering policy.
	PolicyName string
	Namespace  string
	// MetricValue is the observed value that crossed the threshold.
	MetricValue string
	// Timestamp is the trigger time, shared across the whole pipeline.
	Timestamp time.Time
	// TemplateData is the variable map for Go-template expansion (§6).
	TemplateData map[string]any
}

// Target identifies the Kubernetes resource an action operates on.
type Target struct {
	APIVersion string
	Kind       string
	Name       string
	Namespace  string
}

// Result records the outcome of a single action execution. Plugins populate
// Details with enough information for Reverse to undo the action later.
type Result struct {
	ActionID   string
	PluginName string
	Target     Target
	Timestamp  time.Time
	Status     ResultStatus
	Message    string
	Details    map[string]any
}

// ResultStatus enumerates the terminal states of an action execution.
type ResultStatus string

const (
	// StatusSucceeded means the action completed successfully.
	StatusSucceeded ResultStatus = "Succeeded"
	// StatusFailed means the action returned an error.
	StatusFailed ResultStatus = "Failed"
	// StatusSkipped means the action was not run or was reversed.
	StatusSkipped ResultStatus = "Skipped"
)

// Sentinel errors plugins and the executor use to classify failures.
var (
	// ErrNotReversible is returned by Reverse when a plugin cannot undo itself.
	ErrNotReversible = errors.New("action is not reversible")
	// ErrTargetNotFound is returned when the action's target no longer exists.
	ErrTargetNotFound = errors.New("target not found")
	// ErrInvalidParams is returned when params fail to unmarshal or validate.
	ErrInvalidParams = errors.New("invalid params")
	// ErrPermanent wraps errors that must not be retried.
	ErrPermanent = errors.New("permanent error; do not retry")
)
