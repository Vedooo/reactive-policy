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

// Package k8sannotate implements the k8s.annotate action plugin, which adds or
// updates an annotation on the target resource.
package k8sannotate

import (
	"context"
	"errors"
	"fmt"
	"time"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Vedooo/reactive-policy/internal/action"
)

const pluginName = "k8s.annotate"

type plugin struct{}

func init() { action.Register(&plugin{}) }

func (*plugin) Name() string { return pluginName }

func (*plugin) Description() string {
	return "Adds or updates an annotation on the target resource."
}

func (*plugin) Validate(raw action.Params) error {
	var p params
	if err := action.Unmarshal(raw, &p); err != nil {
		return err
	}
	if p.Key == "" {
		return fmt.Errorf("%w: key is required", action.ErrInvalidParams)
	}
	if p.Value == "" {
		return fmt.Errorf("%w: value is required", action.ErrInvalidParams)
	}
	if err := action.ValidateTemplate(p.Value); err != nil {
		return fmt.Errorf("%w: %w", action.ErrInvalidParams, err)
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

	obj, err := getTarget(ctx, c, in.Target)
	if err != nil {
		res.Status = action.StatusFailed
		return res, err
	}

	annotations := obj.GetAnnotations()
	if _, existed := annotations[p.Key]; existed && !p.overwrite() {
		res.Status = action.StatusSkipped
		res.Message = fmt.Sprintf("annotation %q already present and overwrite is false", p.Key)
		return res, nil
	}

	value, err := action.RenderTemplate(p.Value, in.TemplateData)
	if err != nil {
		res.Status = action.StatusFailed
		return res, err
	}
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[p.Key] = value
	obj.SetAnnotations(annotations)
	if err := c.Update(ctx, obj); err != nil {
		res.Status = action.StatusFailed
		return res, fmt.Errorf("annotating %s/%s: %w", in.Target.Namespace, in.Target.Name, err)
	}

	res.Status = action.StatusSucceeded
	res.Message = fmt.Sprintf("annotated %s %s with %q", in.Target.Kind, in.Target.Name, p.Key)
	res.Details = map[string]any{"key": p.Key, "value": value}
	return res, nil
}

// Reverse removes the annotation key the plugin added (see docs/PLUGIN_INTERFACE.md §5.2).
func (*plugin) Reverse(ctx context.Context, prev action.Result) error {
	key, _ := prev.Details["key"].(string)
	if key == "" {
		return fmt.Errorf("%w: result has no annotation key to reverse", action.ErrInvalidParams)
	}
	c := action.ClientFrom(ctx)
	if c == nil {
		return fmt.Errorf("%w: no Kubernetes client available", action.ErrPermanent)
	}

	obj, err := getTarget(ctx, c, prev.Target)
	if err != nil {
		if errors.Is(err, action.ErrTargetNotFound) {
			return nil // nothing to undo
		}
		return err
	}
	annotations := obj.GetAnnotations()
	if _, ok := annotations[key]; !ok {
		return nil
	}
	delete(annotations, key)
	obj.SetAnnotations(annotations)
	if err := c.Update(ctx, obj); err != nil {
		return fmt.Errorf("removing annotation %q: %w", key, err)
	}
	return nil
}

func (*plugin) IsReversible() bool { return true }

func (*plugin) RequiredPermissions() []rbacv1.PolicyRule {
	// The target kind is policy-defined, so the plugin needs broad read/patch
	// access; Week 5's RBAC aggregation narrows this to the configured kinds.
	return []rbacv1.PolicyRule{
		{APIGroups: []string{"*"}, Resources: []string{"*"}, Verbs: []string{"get", "patch", "update"}},
	}
}

func getTarget(ctx context.Context, c client.Client, target action.Target) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(target.APIVersion)
	obj.SetKind(target.Kind)
	key := client.ObjectKey{Namespace: target.Namespace, Name: target.Name}
	if err := c.Get(ctx, key, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %s/%s", action.ErrTargetNotFound, target.Namespace, target.Name)
		}
		return nil, fmt.Errorf("getting target %s/%s: %w", target.Namespace, target.Name, err)
	}
	return obj, nil
}
