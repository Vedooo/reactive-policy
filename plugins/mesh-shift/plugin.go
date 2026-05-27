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

// Package meshshift implements the mesh.shift action plugin, which drains
// traffic from a backend by adjusting its weight on a Gateway API HTTPRoute.
// Because it speaks the vendor-neutral Gateway API, it works with Istio,
// Linkerd, and any other Gateway API implementation.
package meshshift

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Vedooo/reactive-policy/internal/action"
)

const (
	pluginName   = "mesh.shift"
	routeGroup   = "gateway.networking.k8s.io"
	routeVersion = "v1"
	routeKind    = "HTTPRoute"

	// defaultWeight is the Gateway API weight an absent weight field implies.
	defaultWeight = 1
)

type plugin struct{}

func init() { action.Register(&plugin{}) }

func (*plugin) Name() string { return pluginName }

func (*plugin) Description() string {
	return "Drains traffic from a backend by setting its weight on a Gateway API HTTPRoute."
}

func (*plugin) Validate(raw action.Params) error {
	var p params
	if err := action.Unmarshal(raw, &p); err != nil {
		return err
	}
	if p.RouteRef.Name == "" {
		return fmt.Errorf("%w: routeRef.name is required", action.ErrInvalidParams)
	}
	if p.Backend == "" {
		return fmt.Errorf("%w: backend is required", action.ErrInvalidParams)
	}
	if w := p.weight(); w < 0 || w > maxWeight {
		return fmt.Errorf("%w: weight must be between 0 and %d", action.ErrInvalidParams, maxWeight)
	}
	return nil
}

func (*plugin) Execute(ctx context.Context, in action.ExecuteInput) (action.Result, error) {
	res := action.Result{PluginName: pluginName, Target: in.Target, Timestamp: time.Now()}

	var p params
	if err := action.Unmarshal(in.Params, &p); err != nil {
		res.Status = action.StatusFailed
		return res, err
	}

	c := action.ClientFrom(ctx)
	if c == nil {
		res.Status = action.StatusFailed
		return res, fmt.Errorf("%w: no Kubernetes client available", action.ErrPermanent)
	}

	namespace := p.RouteRef.Namespace
	if namespace == "" {
		namespace = in.Target.Namespace
	}

	route, err := getRoute(ctx, c, p.RouteRef.Name, namespace)
	if err != nil {
		res.Status = action.StatusFailed
		return res, err
	}

	matched, changes, err := applyWeight(route, p.Backend, int64(p.weight()))
	if err != nil {
		res.Status = action.StatusFailed
		return res, fmt.Errorf("adjusting HTTPRoute %s/%s: %w", namespace, p.RouteRef.Name, err)
	}
	if !matched {
		res.Status = action.StatusFailed
		return res, fmt.Errorf("%w: backend %q not found in HTTPRoute %s/%s",
			action.ErrPermanent, p.Backend, namespace, p.RouteRef.Name)
	}
	if len(changes) == 0 {
		res.Status = action.StatusSkipped
		res.Message = fmt.Sprintf("backend %q already at weight %d", p.Backend, p.weight())
		return res, nil
	}

	if err := c.Update(ctx, route); err != nil {
		res.Status = action.StatusFailed
		return res, fmt.Errorf("updating HTTPRoute %s/%s: %w", namespace, p.RouteRef.Name, err)
	}

	res.Status = action.StatusSucceeded
	res.Message = fmt.Sprintf("shifted backend %q to weight %d on HTTPRoute %s", p.Backend, p.weight(), p.RouteRef.Name)
	res.Details = map[string]any{
		"routeName":      p.RouteRef.Name,
		"routeNamespace": namespace,
		"backend":        p.Backend,
		"weight":         int64(p.weight()),
		"previous":       changes,
	}
	return res, nil
}

// Reverse restores the backend weights the action overrode.
func (*plugin) Reverse(ctx context.Context, prev action.Result) error {
	changes, err := decodeChanges(prev.Details["previous"])
	if err != nil {
		return fmt.Errorf("%w: decoding previous weights: %w", action.ErrInvalidParams, err)
	}
	if len(changes) == 0 {
		return fmt.Errorf("%w: result has no previous weights to restore", action.ErrInvalidParams)
	}
	name, _ := prev.Details["routeName"].(string)
	if name == "" {
		return fmt.Errorf("%w: result has no HTTPRoute name to reverse", action.ErrInvalidParams)
	}
	namespace, _ := prev.Details["routeNamespace"].(string)
	if namespace == "" {
		namespace = prev.Target.Namespace
	}

	c := action.ClientFrom(ctx)
	if c == nil {
		return fmt.Errorf("%w: no Kubernetes client available", action.ErrPermanent)
	}

	route, err := getRoute(ctx, c, name, namespace)
	if err != nil {
		if errors.Is(err, action.ErrTargetNotFound) {
			return nil // route gone; nothing to restore
		}
		return err
	}
	if err := restoreWeights(route, changes); err != nil {
		return fmt.Errorf("restoring HTTPRoute %s/%s weights: %w", namespace, name, err)
	}
	return c.Update(ctx, route)
}

func (*plugin) IsReversible() bool { return true }

func (*plugin) RequiredPermissions() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{routeGroup},
			Resources: []string{"httproutes"},
			Verbs:     []string{"get", "list", "update", "patch"},
		},
	}
}

// backendChange records one backendRef's weight before it was overridden so
// Reverse can restore it. Had is false when the weight field was absent (the
// Gateway API treats an absent weight as 1).
type backendChange struct {
	Rule   int   `json:"rule"`
	Ref    int   `json:"ref"`
	Weight int64 `json:"weight"`
	Had    bool  `json:"had"`
}

// applyWeight sets weight on every backendRef named backend across all rules,
// recording the prior value of each one it actually changes. matched reports
// whether the backend appears at all (so the caller can tell "not found" from
// "already at the target weight").
func applyWeight(route *unstructured.Unstructured, backend string, weight int64) (matched bool, changes []backendChange, err error) {
	rules, found, err := unstructured.NestedSlice(route.Object, "spec", "rules")
	if err != nil {
		return false, nil, err
	}
	if !found {
		return false, nil, nil
	}
	for ri := range rules {
		rule, ok := rules[ri].(map[string]any)
		if !ok {
			continue
		}
		refs, ok := rule["backendRefs"].([]any)
		if !ok {
			continue
		}
		for bi := range refs {
			ref, ok := refs[bi].(map[string]any)
			if !ok {
				continue
			}
			if name, _ := ref["name"].(string); name != backend {
				continue
			}
			matched = true
			old, had := toInt64(ref["weight"])
			effective := old
			if !had {
				effective = defaultWeight
			}
			if effective == weight {
				continue // already at the target weight
			}
			changes = append(changes, backendChange{Rule: ri, Ref: bi, Weight: old, Had: had})
			ref["weight"] = weight
			refs[bi] = ref
		}
		rule["backendRefs"] = refs
		rules[ri] = rule
	}
	if len(changes) > 0 {
		if err := unstructured.SetNestedSlice(route.Object, rules, "spec", "rules"); err != nil {
			return matched, nil, err
		}
	}
	return matched, changes, nil
}

// restoreWeights re-applies the weights recorded by applyWeight.
func restoreWeights(route *unstructured.Unstructured, changes []backendChange) error {
	rules, found, err := unstructured.NestedSlice(route.Object, "spec", "rules")
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	for _, ch := range changes {
		if ch.Rule < 0 || ch.Rule >= len(rules) {
			continue
		}
		rule, ok := rules[ch.Rule].(map[string]any)
		if !ok {
			continue
		}
		refs, ok := rule["backendRefs"].([]any)
		if !ok || ch.Ref < 0 || ch.Ref >= len(refs) {
			continue
		}
		ref, ok := refs[ch.Ref].(map[string]any)
		if !ok {
			continue
		}
		if ch.Had {
			ref["weight"] = ch.Weight
		} else {
			delete(ref, "weight")
		}
		refs[ch.Ref] = ref
		rule["backendRefs"] = refs
		rules[ch.Rule] = rule
	}
	return unstructured.SetNestedSlice(route.Object, rules, "spec", "rules")
}

func getRoute(ctx context.Context, c client.Client, name, namespace string) (*unstructured.Unstructured, error) {
	route := &unstructured.Unstructured{}
	route.SetGroupVersionKind(schema.GroupVersionKind{Group: routeGroup, Version: routeVersion, Kind: routeKind})
	key := client.ObjectKey{Namespace: namespace, Name: name}
	if err := c.Get(ctx, key, route); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: HTTPRoute %s/%s", action.ErrTargetNotFound, namespace, name)
		}
		return nil, fmt.Errorf("getting HTTPRoute %s/%s: %w", namespace, name, err)
	}
	return route, nil
}

// decodeChanges reads the recorded weights back from a Result's Details, which
// round-trip through the ActionAudit as generic JSON.
func decodeChanges(v any) ([]backendChange, error) {
	if v == nil {
		return nil, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var changes []backendChange
	if err := json.Unmarshal(raw, &changes); err != nil {
		return nil, err
	}
	return changes, nil
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	default:
		return 0, false
	}
}
