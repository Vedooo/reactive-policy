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
	"context"
	"fmt"
	"time"

	"github.com/Vedooo/reactive-policy/api/v1alpha1"
	"github.com/Vedooo/reactive-policy/internal/metrics"
)

// Executor runs a policy's ordered action pipeline against a single target,
// applying each action's onFailure policy (see ADR-006).
type Executor struct {
	registry *Registry
}

// NewExecutor returns an Executor backed by the given registry.
func NewExecutor(r *Registry) *Executor {
	return &Executor{registry: r}
}

// Lookup returns the registered plugin by name, or nil if none is registered.
// Callers that record or reverse executions use it to reach plugin metadata and
// the Reverse method without taking a separate dependency on the registry.
func (e *Executor) Lookup(name string) Action {
	return e.registry.Lookup(name)
}

// Run executes the policy's actions in order against target. It returns the
// per-action results plus an error if the pipeline was aborted by a failing
// action whose onFailure is "stop" or "rollback". Results are returned even on
// abort so the caller can write a complete audit trail.
func (e *Executor) Run(ctx context.Context, policy *v1alpha1.ReactivePolicy, target Target, metricValue string) ([]Result, error) {
	ts := time.Now()
	tmpl := templateData(policy, target, metricValue, ts)

	results := make([]Result, 0, len(policy.Spec.Actions))
	for i := range policy.Spec.Actions {
		act := policy.Spec.Actions[i]
		res, execErr := e.runOne(ctx, act, target, policy, metricValue, ts, tmpl)
		results = append(results, res)
		if execErr == nil {
			continue
		}

		onFailure := act.OnFailure
		if onFailure == "" {
			onFailure = v1alpha1.FailureStop
		}
		switch onFailure {
		case v1alpha1.FailureContinue:
			// Record the failure and move on to the next action.
		case v1alpha1.FailureRollback:
			e.rollback(ctx, results)
			return results, fmt.Errorf("action %d (%s) failed, rolled back preceding actions: %w", i, act.Plugin, execErr)
		default: // FailureStop
			return results, fmt.Errorf("action %d (%s) failed, pipeline stopped: %w", i, act.Plugin, execErr)
		}
	}
	return results, nil
}

// runOne executes a single action, records its metrics, and normalizes its
// Result.
func (e *Executor) runOne(ctx context.Context, act v1alpha1.Action, target Target, policy *v1alpha1.ReactivePolicy, metricValue string, ts time.Time, tmpl map[string]any) (Result, error) {
	plugin := e.registry.Lookup(act.Plugin)
	if plugin == nil {
		metrics.RecordAction(act.Plugin, target.Kind, string(StatusFailed))
		return Result{
			PluginName: act.Plugin,
			Target:     target,
			Timestamp:  time.Now(),
			Status:     StatusFailed,
			Message:    "plugin not registered",
		}, fmt.Errorf("plugin %q: %w", act.Plugin, ErrInvalidParams)
	}

	in := ExecuteInput{
		Target:       target,
		Params:       ParamsFromCRD(act.Params),
		PolicyName:   policy.Name,
		Namespace:    policy.Namespace,
		MetricValue:  metricValue,
		Timestamp:    ts,
		TemplateData: tmpl,
	}

	start := time.Now()
	res, err := plugin.Execute(ctx, in)
	metrics.ObserveActionDuration(plugin.Name(), time.Since(start).Seconds())

	if res.PluginName == "" {
		res.PluginName = plugin.Name()
	}
	if res.Timestamp.IsZero() {
		res.Timestamp = time.Now()
	}
	res.Target = target
	if err != nil {
		if res.Status == "" {
			res.Status = StatusFailed
		}
		if res.Message == "" {
			res.Message = err.Error()
		}
	} else if res.Status == "" {
		res.Status = StatusSucceeded
	}
	metrics.RecordAction(plugin.Name(), target.Kind, string(res.Status))
	return res, err
}

// rollback reverses previously succeeded actions in reverse order. It is
// best-effort: a Reverse error is recorded in the result's Details but does not
// halt the rollback of the remaining actions.
func (e *Executor) rollback(ctx context.Context, results []Result) {
	for i := len(results) - 1; i >= 0; i-- {
		if results[i].Status != StatusSucceeded {
			continue
		}
		plugin := e.registry.Lookup(results[i].PluginName)
		if plugin == nil {
			continue
		}
		if err := plugin.Reverse(ctx, results[i]); err != nil {
			annotate(&results[i], "rollbackError", err.Error())
			continue
		}
		annotate(&results[i], "reversed", true)
	}
}

// annotate sets a key on a result's Details, allocating the map if needed.
func annotate(res *Result, key string, value any) {
	if res.Details == nil {
		res.Details = make(map[string]any)
	}
	res.Details[key] = value
}

// templateData builds the variable map plugins use for Go-template expansion
// (see docs/PLUGIN_INTERFACE.md §6).
func templateData(policy *v1alpha1.ReactivePolicy, target Target, metricValue string, ts time.Time) map[string]any {
	return map[string]any{
		"PolicyName":  policy.Name,
		"Namespace":   policy.Namespace,
		"MetricValue": metricValue,
		"Timestamp":   ts.Format(time.RFC3339),
		"Target":      target,
	}
}
