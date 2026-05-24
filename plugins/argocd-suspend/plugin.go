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

// Package argocdsuspend implements the argocd.suspend action plugin, which
// suspends auto-sync on a target ArgoCD Application by removing
// spec.syncPolicy.automated, and restores it on reverse.
package argocdsuspend

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

const (
	pluginName              = "argocd.suspend"
	applicationAPIVersion   = "argoproj.io/v1alpha1"
	applicationKind         = "Application"
	suspendReasonAnnotation = "reactive-policy.io/suspend-reason"
	defaultReason           = "auto-suspended by reactive-policy"
)

type plugin struct{}

func init() { action.Register(&plugin{}) }

func (*plugin) Name() string { return pluginName }

func (*plugin) Description() string {
	return "Suspends auto-sync on a target ArgoCD Application."
}

func (*plugin) Validate(raw action.Params) error {
	var p params
	// reason is optional; this just rejects malformed params.
	return action.Unmarshal(raw, &p)
}

func (*plugin) Execute(ctx context.Context, in action.ExecuteInput) (action.Result, error) {
	res := action.Result{PluginName: pluginName, Target: in.Target, Timestamp: time.Now()}

	if in.Target.APIVersion != applicationAPIVersion || in.Target.Kind != applicationKind {
		res.Status = action.StatusFailed
		return res, fmt.Errorf("%w: target must be %s/%s, got %s/%s",
			action.ErrInvalidParams, applicationAPIVersion, applicationKind, in.Target.APIVersion, in.Target.Kind)
	}

	var p params
	if err := action.Unmarshal(in.Params, &p); err != nil {
		res.Status = action.StatusFailed
		return res, err
	}
	reason := p.Reason
	if reason == "" {
		reason = defaultReason
	}

	c := action.ClientFrom(ctx)
	if c == nil {
		res.Status = action.StatusFailed
		return res, fmt.Errorf("%w: no Kubernetes client available", action.ErrPermanent)
	}

	app, err := getApplication(ctx, c, in.Target)
	if err != nil {
		res.Status = action.StatusFailed
		return res, err
	}

	if _, hasAutomated, err := unstructured.NestedMap(app.Object, "spec", "syncPolicy", "automated"); err != nil {
		res.Status = action.StatusFailed
		return res, fmt.Errorf("reading spec.syncPolicy.automated: %w", err)
	} else if !hasAutomated {
		res.Status = action.StatusSkipped
		res.Message = "auto-sync is already suspended"
		return res, nil
	}

	previousSyncPolicy, _, err := unstructured.NestedFieldCopy(app.Object, "spec", "syncPolicy")
	if err != nil {
		res.Status = action.StatusFailed
		return res, fmt.Errorf("reading spec.syncPolicy: %w", err)
	}

	unstructured.RemoveNestedField(app.Object, "spec", "syncPolicy", "automated")
	setAnnotation(app, suspendReasonAnnotation, reason)

	if err := c.Update(ctx, app); err != nil {
		res.Status = action.StatusFailed
		return res, fmt.Errorf("suspending application %s/%s: %w", in.Target.Namespace, in.Target.Name, err)
	}

	res.Status = action.StatusSucceeded
	res.Message = fmt.Sprintf("suspended auto-sync on Application %s", in.Target.Name)
	res.Details = map[string]any{"previousSyncPolicy": previousSyncPolicy, "reason": reason}
	return res, nil
}

// Reverse restores the stored spec.syncPolicy and removes the suspend-reason
// annotation (see docs/PLUGIN_INTERFACE.md §5.3).
func (*plugin) Reverse(ctx context.Context, prev action.Result) error {
	c := action.ClientFrom(ctx)
	if c == nil {
		return fmt.Errorf("%w: no Kubernetes client available", action.ErrPermanent)
	}

	app, err := getApplication(ctx, c, prev.Target)
	if err != nil {
		if errors.Is(err, action.ErrTargetNotFound) {
			return nil // nothing to undo
		}
		return err
	}

	if sp, ok := prev.Details["previousSyncPolicy"]; ok && sp != nil {
		if err := unstructured.SetNestedField(app.Object, sp, "spec", "syncPolicy"); err != nil {
			return fmt.Errorf("restoring spec.syncPolicy: %w", err)
		}
	}
	removeAnnotation(app, suspendReasonAnnotation)

	if err := c.Update(ctx, app); err != nil {
		return fmt.Errorf("restoring application %s/%s: %w", prev.Target.Namespace, prev.Target.Name, err)
	}
	return nil
}

func (*plugin) IsReversible() bool { return true }

func (*plugin) RequiredPermissions() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{APIGroups: []string{"argoproj.io"}, Resources: []string{"applications"}, Verbs: []string{"get", "patch", "update"}},
	}
}

func getApplication(ctx context.Context, c client.Client, target action.Target) (*unstructured.Unstructured, error) {
	app := &unstructured.Unstructured{}
	app.SetAPIVersion(target.APIVersion)
	app.SetKind(target.Kind)
	key := client.ObjectKey{Namespace: target.Namespace, Name: target.Name}
	if err := c.Get(ctx, key, app); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("%w: %s/%s", action.ErrTargetNotFound, target.Namespace, target.Name)
		}
		return nil, fmt.Errorf("getting application %s/%s: %w", target.Namespace, target.Name, err)
	}
	return app, nil
}

func setAnnotation(obj *unstructured.Unstructured, key, value string) {
	ann := obj.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	ann[key] = value
	obj.SetAnnotations(ann)
}

func removeAnnotation(obj *unstructured.Unstructured, key string) {
	ann := obj.GetAnnotations()
	if ann == nil {
		return
	}
	delete(ann, key)
	obj.SetAnnotations(ann)
}
