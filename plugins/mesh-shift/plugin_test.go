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

package meshshift

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/Vedooo/reactive-policy/internal/action"
)

func raw(s string) []byte { return []byte(s) }

func routeGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: routeGroup, Version: routeVersion, Kind: routeKind}
}

// scheme and restMapper register the Gateway API HTTPRoute as unstructured so
// the fake client can serve it without vendoring the gateway-api types.
func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.AddKnownTypeWithName(routeGVK(), &unstructured.Unstructured{})
	listGVK := routeGVK()
	listGVK.Kind += "List"
	s.AddKnownTypeWithName(listGVK, &unstructured.UnstructuredList{})
	return s
}

func newRESTMapper() meta.RESTMapper {
	rm := meta.NewDefaultRESTMapper(nil)
	rm.Add(routeGVK(), meta.RESTScopeNamespace)
	return rm
}

func newClient(objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithRESTMapper(newRESTMapper()).
		WithObjects(objs...).
		Build()
}

func target() action.Target {
	return action.Target{APIVersion: "apps/v1", Kind: "Deployment", Name: "api", Namespace: "prod"}
}

func backendRef(name string, weight int64) map[string]any {
	return map[string]any{"name": name, "port": int64(80), "weight": weight}
}

func rule(refs ...map[string]any) map[string]any {
	br := make([]any, len(refs))
	for i := range refs {
		br[i] = refs[i]
	}
	return map[string]any{"backendRefs": br}
}

func newRoute(name, ns string, rules ...map[string]any) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(routeGVK())
	u.SetName(name)
	u.SetNamespace(ns)
	rs := make([]any, len(rules))
	for i := range rules {
		rs[i] = rules[i]
	}
	if err := unstructured.SetNestedSlice(u.Object, rs, "spec", "rules"); err != nil {
		panic(err)
	}
	return u
}

func getRouteObj(t *testing.T, c client.Client, name, ns string) *unstructured.Unstructured {
	t.Helper()
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(routeGVK())
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, u); err != nil {
		t.Fatalf("get route %s/%s: %v", ns, name, err)
	}
	return u
}

func weightAt(t *testing.T, u *unstructured.Unstructured, ri, bi int) (int64, bool) {
	t.Helper()
	rules, _, err := unstructured.NestedSlice(u.Object, "spec", "rules")
	if err != nil {
		t.Fatalf("reading rules: %v", err)
	}
	r := rules[ri].(map[string]any)
	refs := r["backendRefs"].([]any)
	ref := refs[bi].(map[string]any)
	return toInt64(ref["weight"])
}

func TestExecuteShiftsBackendWeight(t *testing.T) {
	c := newClient(newRoute("api-route", "prod",
		rule(backendRef("api", 100), backendRef("api-canary", 0))))
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	res, err := p.Execute(ctx, action.ExecuteInput{
		Target: target(),
		Params: action.Params{
			"routeRef": {Raw: raw(`{"name":"api-route"}`)}, // namespace defaults to target's
			"backend":  {Raw: raw(`"api"`)},
		},
		TemplateData: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if res.Status != action.StatusSucceeded {
		t.Fatalf("status = %q, want Succeeded", res.Status)
	}

	u := getRouteObj(t, c, "api-route", "prod")
	if w, _ := weightAt(t, u, 0, 0); w != 0 {
		t.Errorf("api weight = %d, want 0 (drained)", w)
	}
	if w, _ := weightAt(t, u, 0, 1); w != 0 {
		t.Errorf("api-canary weight = %d, want unchanged 0", w)
	}

	changes, err := decodeChanges(res.Details["previous"])
	if err != nil {
		t.Fatalf("decoding previous: %v", err)
	}
	if len(changes) != 1 || changes[0].Weight != 100 || !changes[0].Had {
		t.Errorf("previous = %+v, want one change recording old weight 100", changes)
	}
}

func TestExecuteWeightParam(t *testing.T) {
	c := newClient(newRoute("api-route", "prod", rule(backendRef("api", 100))))
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	_, err := p.Execute(ctx, action.ExecuteInput{
		Target: target(),
		Params: action.Params{
			"routeRef": {Raw: raw(`{"name":"api-route","namespace":"prod"}`)},
			"backend":  {Raw: raw(`"api"`)},
			"weight":   {Raw: raw(`20`)},
		},
		TemplateData: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	u := getRouteObj(t, c, "api-route", "prod")
	if w, _ := weightAt(t, u, 0, 0); w != 20 {
		t.Errorf("api weight = %d, want 20", w)
	}
}

func TestExecuteBackendAcrossRules(t *testing.T) {
	c := newClient(newRoute("api-route", "prod",
		rule(backendRef("api", 100)),
		rule(backendRef("api", 50), backendRef("other", 50))))
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	res, err := p.Execute(ctx, action.ExecuteInput{
		Target:       target(),
		Params:       action.Params{"routeRef": {Raw: raw(`{"name":"api-route"}`)}, "backend": {Raw: raw(`"api"`)}},
		TemplateData: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	changes, _ := decodeChanges(res.Details["previous"])
	if len(changes) != 2 {
		t.Errorf("expected 2 changed backendRefs across rules, got %d", len(changes))
	}
	u := getRouteObj(t, c, "api-route", "prod")
	if w, _ := weightAt(t, u, 1, 0); w != 0 {
		t.Errorf("rule 1 api weight = %d, want 0", w)
	}
}

func TestExecuteBackendNotFound(t *testing.T) {
	c := newClient(newRoute("api-route", "prod", rule(backendRef("api", 100))))
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	res, err := p.Execute(ctx, action.ExecuteInput{
		Target:       target(),
		Params:       action.Params{"routeRef": {Raw: raw(`{"name":"api-route"}`)}, "backend": {Raw: raw(`"ghost"`)}},
		TemplateData: map[string]any{},
	})
	if err == nil {
		t.Fatal("Execute should fail when the backend is not in the route")
	}
	if res.Status != action.StatusFailed {
		t.Errorf("status = %q, want Failed", res.Status)
	}
}

func TestExecuteAlreadyAtWeightSkips(t *testing.T) {
	c := newClient(newRoute("api-route", "prod", rule(backendRef("api", 0))))
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	res, err := p.Execute(ctx, action.ExecuteInput{
		Target:       target(),
		Params:       action.Params{"routeRef": {Raw: raw(`{"name":"api-route"}`)}, "backend": {Raw: raw(`"api"`)}},
		TemplateData: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if res.Status != action.StatusSkipped {
		t.Fatalf("status = %q, want Skipped (already at weight 0)", res.Status)
	}
}

func TestExecuteRouteNotFound(t *testing.T) {
	c := newClient()
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	_, err := p.Execute(ctx, action.ExecuteInput{
		Target:       target(),
		Params:       action.Params{"routeRef": {Raw: raw(`{"name":"missing"}`)}, "backend": {Raw: raw(`"api"`)}},
		TemplateData: map[string]any{},
	})
	if err == nil {
		t.Fatal("Execute should fail when the HTTPRoute does not exist")
	}
}

func TestReverseRestoresWeight(t *testing.T) {
	c := newClient(newRoute("api-route", "prod", rule(backendRef("api", 0))))
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	prev := action.Result{
		PluginName: pluginName,
		Target:     target(),
		Details: map[string]any{
			"routeName":      "api-route",
			"routeNamespace": "prod",
			"previous":       []backendChange{{Rule: 0, Ref: 0, Weight: 100, Had: true}},
		},
	}
	if err := p.Reverse(ctx, prev); err != nil {
		t.Fatalf("Reverse returned error: %v", err)
	}
	u := getRouteObj(t, c, "api-route", "prod")
	if w, had := weightAt(t, u, 0, 0); !had || w != 100 {
		t.Errorf("api weight = (%d, had=%v), want 100", w, had)
	}
}

func TestReverseAbsentWeightRemoved(t *testing.T) {
	c := newClient(newRoute("api-route", "prod", rule(backendRef("api", 0))))
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	prev := action.Result{
		Target: target(),
		Details: map[string]any{
			"routeName":      "api-route",
			"routeNamespace": "prod",
			"previous":       []backendChange{{Rule: 0, Ref: 0, Weight: 0, Had: false}},
		},
	}
	if err := p.Reverse(ctx, prev); err != nil {
		t.Fatalf("Reverse returned error: %v", err)
	}
	u := getRouteObj(t, c, "api-route", "prod")
	if _, had := weightAt(t, u, 0, 0); had {
		t.Error("Reverse should have removed the weight field that was originally absent")
	}
}

func TestReverseRouteGoneIsNoOp(t *testing.T) {
	c := newClient()
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	prev := action.Result{
		Target: target(),
		Details: map[string]any{
			"routeName":      "api-route",
			"routeNamespace": "prod",
			"previous":       []backendChange{{Rule: 0, Ref: 0, Weight: 100, Had: true}},
		},
	}
	if err := p.Reverse(ctx, prev); err != nil {
		t.Fatalf("reversing a missing route should be a no-op, got: %v", err)
	}
}
