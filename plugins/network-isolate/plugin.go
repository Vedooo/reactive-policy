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

// Package networkisolate implements the network.isolate action plugin, which
// quarantines the target workload's pods behind a restrictive NetworkPolicy.
package networkisolate

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Vedooo/reactive-policy/internal/action"
)

const (
	pluginName = "network.isolate"
	namePrefix = "rp-isolate-"
	dnsPort    = 53
)

type plugin struct{}

func init() { action.Register(&plugin{}) }

func (*plugin) Name() string { return pluginName }

func (*plugin) Description() string {
	return "Quarantines the target workload's pods with a restrictive NetworkPolicy."
}

func (*plugin) Validate(raw action.Params) error {
	var p params
	if err := action.Unmarshal(raw, &p); err != nil {
		return err
	}
	switch p.direction() {
	case directionIngress, directionEgress, directionBoth:
	default:
		return fmt.Errorf("%w: direction must be one of ingress, egress, both", action.ErrInvalidParams)
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

	selector := p.PodSelector
	if len(selector) == 0 {
		obj, err := getTarget(ctx, c, in.Target)
		if err != nil {
			res.Status = action.StatusFailed
			return res, err
		}
		selector = deriveSelector(obj)
	}
	if len(selector) == 0 {
		res.Status = action.StatusFailed
		return res, fmt.Errorf("%w: cannot derive a pod selector for %s %q; set params.podSelector",
			action.ErrPermanent, in.Target.Kind, in.Target.Name)
	}

	np := desiredPolicy(in, p, selector)
	if err := applyPolicy(ctx, c, np); err != nil {
		res.Status = action.StatusFailed
		return res, fmt.Errorf("isolating %s/%s: %w", in.Target.Namespace, in.Target.Name, err)
	}

	res.Status = action.StatusSucceeded
	res.Message = fmt.Sprintf("isolated %s %s with NetworkPolicy %s (%s)",
		in.Target.Kind, in.Target.Name, np.Name, p.direction())
	res.Details = map[string]any{
		"networkPolicyName": np.Name,
		"namespace":         np.Namespace,
		"direction":         p.direction(),
	}
	return res, nil
}

// Reverse deletes the NetworkPolicy the plugin created.
func (*plugin) Reverse(ctx context.Context, prev action.Result) error {
	name, _ := prev.Details["networkPolicyName"].(string)
	if name == "" {
		return fmt.Errorf("%w: result has no NetworkPolicy name to reverse", action.ErrInvalidParams)
	}
	namespace, _ := prev.Details["namespace"].(string)
	if namespace == "" {
		namespace = prev.Target.Namespace
	}
	c := action.ClientFrom(ctx)
	if c == nil {
		return fmt.Errorf("%w: no Kubernetes client available", action.ErrPermanent)
	}

	np := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	if err := c.Delete(ctx, np); err != nil {
		if apierrors.IsNotFound(err) {
			return nil // already gone
		}
		return fmt.Errorf("removing NetworkPolicy %s/%s: %w", namespace, name, err)
	}
	return nil
}

func (*plugin) IsReversible() bool { return true }

func (*plugin) RequiredPermissions() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{"networking.k8s.io"},
			Resources: []string{"networkpolicies"},
			Verbs:     []string{"get", "list", "create", "update", "patch", "delete"},
		},
		// Reading the target to derive its pod selector; the kind is policy-defined.
		{APIGroups: []string{"*"}, Resources: []string{"*"}, Verbs: []string{"get"}},
	}
}

// desiredPolicy builds the NetworkPolicy that isolates the selected pods.
func desiredPolicy(in action.ExecuteInput, p params, selector map[string]string) *networkingv1.NetworkPolicy {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namePrefix + in.Target.Name,
			Namespace: in.Target.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "reactive-policy",
			},
			Annotations: map[string]string{
				"reactive-policy.io/policy":          in.PolicyName,
				"reactive-policy.io/isolated-target": in.Target.Name,
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: selector},
		},
	}
	if p.isolatesIngress() {
		// A nil Ingress slice denies all ingress.
		np.Spec.PolicyTypes = append(np.Spec.PolicyTypes, networkingv1.PolicyTypeIngress)
	}
	if p.isolatesEgress() {
		np.Spec.PolicyTypes = append(np.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)
		if p.allowDNS() {
			np.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{{Ports: dnsPorts()}}
		}
		// Otherwise a nil Egress slice denies all egress.
	}
	return np
}

func dnsPorts() []networkingv1.NetworkPolicyPort {
	udp := corev1.ProtocolUDP
	tcp := corev1.ProtocolTCP
	port := intstr.FromInt32(dnsPort)
	return []networkingv1.NetworkPolicyPort{
		{Protocol: &udp, Port: &port},
		{Protocol: &tcp, Port: &port},
	}
}

// applyPolicy creates the NetworkPolicy, or updates it in place when a previous
// trigger already created one, so re-triggering is idempotent.
func applyPolicy(ctx context.Context, c client.Client, np *networkingv1.NetworkPolicy) error {
	existing := &networkingv1.NetworkPolicy{}
	err := c.Get(ctx, client.ObjectKeyFromObject(np), existing)
	if apierrors.IsNotFound(err) {
		return c.Create(ctx, np)
	}
	if err != nil {
		return err
	}
	existing.Labels = np.Labels
	existing.Annotations = np.Annotations
	existing.Spec = np.Spec
	return c.Update(ctx, existing)
}

// deriveSelector finds the pod selector for the target: a workload's
// spec.selector.matchLabels, a Service's spec.selector, or the object's own
// labels, in that order.
func deriveSelector(obj *unstructured.Unstructured) map[string]string {
	if ml, found, _ := unstructured.NestedStringMap(obj.Object, "spec", "selector", "matchLabels"); found && len(ml) > 0 {
		return ml
	}
	if sel, found, _ := unstructured.NestedStringMap(obj.Object, "spec", "selector"); found && len(sel) > 0 {
		return sel
	}
	if lbls := obj.GetLabels(); len(lbls) > 0 {
		return lbls
	}
	return nil
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
