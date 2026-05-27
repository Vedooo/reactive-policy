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

package networkisolate

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/Vedooo/reactive-policy/internal/action"
)

func raw(s string) []byte { return []byte(s) }

func deploymentTarget() action.Target {
	return action.Target{APIVersion: "apps/v1", Kind: "Deployment", Name: "api", Namespace: "default"}
}

func newClientWithDeployment(matchLabels map[string]string) client.Client {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: matchLabels}},
	}
	return fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(dep).Build()
}

func getPolicy(t *testing.T, c client.Client, name string) *networkingv1.NetworkPolicy {
	t.Helper()
	var np networkingv1.NetworkPolicy
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: name}, &np); err != nil {
		t.Fatalf("getting NetworkPolicy %s: %v", name, err)
	}
	return &np
}

func TestValidate(t *testing.T) {
	cases := map[string]struct {
		params  action.Params
		wantErr bool
	}{
		"empty is both":     {params: action.Params{}},
		"ingress":           {params: action.Params{"direction": {Raw: raw(`"ingress"`)}}},
		"egress":            {params: action.Params{"direction": {Raw: raw(`"egress"`)}}},
		"both":              {params: action.Params{"direction": {Raw: raw(`"both"`)}}},
		"bad direction":     {params: action.Params{"direction": {Raw: raw(`"sideways"`)}}, wantErr: true},
		"explicit selector": {params: action.Params{"podSelector": {Raw: raw(`{"app":"api"}`)}}},
	}
	var p plugin
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if (p.Validate(tc.params) != nil) != tc.wantErr {
				t.Fatalf("Validate wantErr = %v", tc.wantErr)
			}
		})
	}
}

func TestExecuteCreatesPolicyFromWorkloadSelector(t *testing.T) {
	c := newClientWithDeployment(map[string]string{"app": "api"})
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	res, err := p.Execute(ctx, action.ExecuteInput{
		Target:       deploymentTarget(),
		PolicyName:   "high-error-rate",
		Params:       action.Params{},
		TemplateData: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if res.Status != action.StatusSucceeded {
		t.Fatalf("status = %q, want Succeeded", res.Status)
	}
	np := getPolicy(t, c, "rp-isolate-api")
	if np.Spec.PodSelector.MatchLabels["app"] != "api" {
		t.Errorf("podSelector = %v, want app=api", np.Spec.PodSelector.MatchLabels)
	}
	if len(np.Spec.PolicyTypes) != 2 {
		t.Errorf("policyTypes = %v, want Ingress+Egress", np.Spec.PolicyTypes)
	}
	if len(np.Spec.Egress) != 1 || len(np.Spec.Egress[0].Ports) != 2 {
		t.Errorf("expected one DNS egress rule with two ports, got %+v", np.Spec.Egress)
	}
	if np.Spec.Ingress != nil {
		t.Errorf("ingress should be nil (deny all), got %+v", np.Spec.Ingress)
	}
}

func TestExecuteExplicitPodSelector(t *testing.T) {
	// No workload object exists; the selector comes purely from params.
	c := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	_, err := p.Execute(ctx, action.ExecuteInput{
		Target:       deploymentTarget(),
		Params:       action.Params{"podSelector": {Raw: raw(`{"role":"web"}`)}},
		TemplateData: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	np := getPolicy(t, c, "rp-isolate-api")
	if np.Spec.PodSelector.MatchLabels["role"] != "web" {
		t.Errorf("podSelector = %v, want role=web", np.Spec.PodSelector.MatchLabels)
	}
}

func TestExecuteIngressOnly(t *testing.T) {
	c := newClientWithDeployment(map[string]string{"app": "api"})
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	_, err := p.Execute(ctx, action.ExecuteInput{
		Target:       deploymentTarget(),
		Params:       action.Params{"direction": {Raw: raw(`"ingress"`)}},
		TemplateData: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	np := getPolicy(t, c, "rp-isolate-api")
	if len(np.Spec.PolicyTypes) != 1 || np.Spec.PolicyTypes[0] != networkingv1.PolicyTypeIngress {
		t.Errorf("policyTypes = %v, want [Ingress]", np.Spec.PolicyTypes)
	}
	if np.Spec.Egress != nil {
		t.Errorf("egress should be nil for ingress-only, got %+v", np.Spec.Egress)
	}
}

func TestExecuteEgressNoDNS(t *testing.T) {
	c := newClientWithDeployment(map[string]string{"app": "api"})
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	_, err := p.Execute(ctx, action.ExecuteInput{
		Target:       deploymentTarget(),
		Params:       action.Params{"direction": {Raw: raw(`"egress"`)}, "allowDNS": {Raw: raw(`false`)}},
		TemplateData: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	np := getPolicy(t, c, "rp-isolate-api")
	if len(np.Spec.PolicyTypes) != 1 || np.Spec.PolicyTypes[0] != networkingv1.PolicyTypeEgress {
		t.Errorf("policyTypes = %v, want [Egress]", np.Spec.PolicyTypes)
	}
	if np.Spec.Egress != nil {
		t.Errorf("egress should be nil (deny all) when allowDNS=false, got %+v", np.Spec.Egress)
	}
}

func TestExecuteIdempotent(t *testing.T) {
	c := newClientWithDeployment(map[string]string{"app": "api"})
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	in := action.ExecuteInput{Target: deploymentTarget(), Params: action.Params{}, TemplateData: map[string]any{}}
	if _, err := p.Execute(ctx, in); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if _, err := p.Execute(ctx, in); err != nil {
		t.Fatalf("second Execute should be idempotent: %v", err)
	}
	var list networkingv1.NetworkPolicyList
	if err := c.List(context.Background(), &list); err != nil {
		t.Fatalf("listing policies: %v", err)
	}
	if len(list.Items) != 1 {
		t.Errorf("expected exactly one NetworkPolicy, got %d", len(list.Items))
	}
}

func TestExecuteSelectorNotDerivable(t *testing.T) {
	// Deployment with an empty selector and no labels, and no podSelector param.
	c := newClientWithDeployment(nil)
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	res, err := p.Execute(ctx, action.ExecuteInput{
		Target:       deploymentTarget(),
		Params:       action.Params{},
		TemplateData: map[string]any{},
	})
	if err == nil {
		t.Fatal("Execute should fail when no selector can be derived")
	}
	if res.Status != action.StatusFailed {
		t.Errorf("status = %q, want Failed", res.Status)
	}
}

func TestExecuteTargetNotFound(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	_, err := p.Execute(ctx, action.ExecuteInput{
		Target:       deploymentTarget(),
		Params:       action.Params{},
		TemplateData: map[string]any{},
	})
	if err == nil {
		t.Fatal("Execute against a missing target with no podSelector should fail")
	}
}

func TestReverseDeletesPolicy(t *testing.T) {
	existing := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "rp-isolate-api", Namespace: "default"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(existing).Build()
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	prev := action.Result{
		PluginName: pluginName,
		Target:     deploymentTarget(),
		Details:    map[string]any{"networkPolicyName": "rp-isolate-api", "namespace": "default"},
	}
	if err := p.Reverse(ctx, prev); err != nil {
		t.Fatalf("Reverse returned error: %v", err)
	}
	var np networkingv1.NetworkPolicy
	err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "rp-isolate-api"}, &np)
	if err == nil {
		t.Error("Reverse should have deleted the NetworkPolicy")
	}
}

func TestReverseMissingPolicyIsNoOp(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	prev := action.Result{
		Target:  deploymentTarget(),
		Details: map[string]any{"networkPolicyName": "rp-isolate-ghost", "namespace": "default"},
	}
	if err := p.Reverse(ctx, prev); err != nil {
		t.Fatalf("reversing a missing policy should be a no-op, got: %v", err)
	}
}
