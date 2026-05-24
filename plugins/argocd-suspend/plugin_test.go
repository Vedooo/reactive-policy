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

package argocdsuspend

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/Vedooo/reactive-policy/internal/action"
)

var appGVK = schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "Application"}

func appTarget() action.Target {
	return action.Target{APIVersion: applicationAPIVersion, Kind: applicationKind, Name: "api", Namespace: "default"}
}

// newApplication builds an ArgoCD Application; automated controls whether
// spec.syncPolicy.automated is present.
func newApplication(automated bool) *unstructured.Unstructured {
	app := &unstructured.Unstructured{}
	app.SetGroupVersionKind(appGVK)
	app.SetName("api")
	app.SetNamespace("default")
	syncPolicy := map[string]any{"syncOptions": []any{"CreateNamespace=true"}}
	if automated {
		syncPolicy["automated"] = map[string]any{"prune": true, "selfHeal": true}
	}
	_ = unstructured.SetNestedField(app.Object, syncPolicy, "spec", "syncPolicy")
	return app
}

func newClient(objs ...client.Object) client.Client {
	s := runtime.NewScheme()
	s.AddKnownTypeWithName(appGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(appGVK.GroupVersion().WithKind("ApplicationList"), &unstructured.UnstructuredList{})
	metav1.AddToGroupVersion(s, appGVK.GroupVersion())
	return fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
}

func getApp(t *testing.T, c client.Client) *unstructured.Unstructured {
	t.Helper()
	app := &unstructured.Unstructured{}
	app.SetGroupVersionKind(appGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "api"}, app); err != nil {
		t.Fatalf("getting application: %v", err)
	}
	return app
}

func hasAutomated(t *testing.T, app *unstructured.Unstructured) bool {
	t.Helper()
	_, found, err := unstructured.NestedMap(app.Object, "spec", "syncPolicy", "automated")
	if err != nil {
		t.Fatalf("reading automated: %v", err)
	}
	return found
}

func TestExecuteSuspendsAutoSync(t *testing.T) {
	c := newClient(newApplication(true))
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	res, err := p.Execute(ctx, action.ExecuteInput{
		Target:       appTarget(),
		Params:       action.Params{"reason": {Raw: []byte(`"high error rate"`)}},
		TemplateData: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if res.Status != action.StatusSucceeded {
		t.Fatalf("status = %q, want Succeeded", res.Status)
	}

	app := getApp(t, c)
	if hasAutomated(t, app) {
		t.Error("spec.syncPolicy.automated should have been removed")
	}
	if app.GetAnnotations()[suspendReasonAnnotation] != "high error rate" {
		t.Errorf("suspend-reason annotation = %q", app.GetAnnotations()[suspendReasonAnnotation])
	}
	if res.Details["previousSyncPolicy"] == nil {
		t.Error("Details should retain the previous syncPolicy for reverse")
	}
}

func TestExecuteAlreadySuspendedIsSkipped(t *testing.T) {
	c := newClient(newApplication(false))
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	res, err := p.Execute(ctx, action.ExecuteInput{Target: appTarget(), TemplateData: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if res.Status != action.StatusSkipped {
		t.Fatalf("status = %q, want Skipped", res.Status)
	}
}

func TestExecuteWrongTargetKindFails(t *testing.T) {
	c := newClient(newApplication(true))
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	_, err := p.Execute(ctx, action.ExecuteInput{
		Target:       action.Target{APIVersion: "apps/v1", Kind: "Deployment", Name: "api", Namespace: "default"},
		TemplateData: map[string]any{},
	})
	if err == nil {
		t.Fatal("a non-Application target should be rejected")
	}
}

func TestReverseRestoresAutoSync(t *testing.T) {
	c := newClient(newApplication(true))
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	res, err := p.Execute(ctx, action.ExecuteInput{Target: appTarget(), TemplateData: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if hasAutomated(t, getApp(t, c)) {
		t.Fatal("precondition: automated should be removed after Execute")
	}

	if err := p.Reverse(ctx, res); err != nil {
		t.Fatalf("Reverse returned error: %v", err)
	}
	app := getApp(t, c)
	if !hasAutomated(t, app) {
		t.Error("Reverse should restore spec.syncPolicy.automated")
	}
	if _, ok := app.GetAnnotations()[suspendReasonAnnotation]; ok {
		t.Error("Reverse should remove the suspend-reason annotation")
	}
}
