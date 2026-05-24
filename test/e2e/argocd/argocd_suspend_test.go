//go:build e2e

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

package argocd

import (
	"context"
	"testing"
	"time"

	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Vedooo/reactive-policy/internal/action"

	// Register the argocd.suspend plugin into the default registry.
	_ "github.com/Vedooo/reactive-policy/plugins/argocd-suspend"
)

var appGVK = schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "Application"}

func ptrBool(b bool) *bool { return &b }

// TestArgoCDSuspendE2E exercises the argocd.suspend plugin against a live
// cluster (kind): it installs a minimal ArgoCD Application CRD, creates an
// Application with auto-sync enabled, suspends it, and reverses the suspension.
func TestArgoCDSuspendE2E(t *testing.T) {
	cfg := ctrl.GetConfigOrDie()
	ctx := context.Background()

	installApplicationCRD(t, cfg)

	c := newAppClient(t, cfg)
	app := newApplication()
	if err := c.Create(ctx, app); err != nil {
		t.Fatalf("creating Application: %v", err)
	}
	t.Cleanup(func() { _ = c.Delete(ctx, newApplication()) })

	plugin := action.Lookup("argocd.suspend")
	if plugin == nil {
		t.Fatal("argocd.suspend plugin is not registered")
	}
	target := action.Target{APIVersion: "argoproj.io/v1alpha1", Kind: "Application", Name: "e2e-app", Namespace: "default"}
	pluginCtx := action.WithClient(ctx, c)

	res, err := plugin.Execute(pluginCtx, action.ExecuteInput{
		Target:       target,
		Params:       action.Params{"reason": {Raw: []byte(`"e2e suspend"`)}},
		TemplateData: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Status != action.StatusSucceeded {
		t.Fatalf("Execute status = %q, want Succeeded", res.Status)
	}

	got := getApplication(t, c)
	if _, found, _ := unstructured.NestedMap(got.Object, "spec", "syncPolicy", "automated"); found {
		t.Error("auto-sync should be suspended (automated removed)")
	}
	if got.GetAnnotations()["reactive-policy.io/suspend-reason"] != "e2e suspend" {
		t.Errorf("suspend-reason annotation = %q", got.GetAnnotations()["reactive-policy.io/suspend-reason"])
	}

	if err := plugin.Reverse(pluginCtx, res); err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	got = getApplication(t, c)
	if _, found, _ := unstructured.NestedMap(got.Object, "spec", "syncPolicy", "automated"); !found {
		t.Error("Reverse should restore spec.syncPolicy.automated")
	}
	if _, ok := got.GetAnnotations()["reactive-policy.io/suspend-reason"]; ok {
		t.Error("Reverse should remove the suspend-reason annotation")
	}
}

func installApplicationCRD(t *testing.T, cfg *rest.Config) {
	t.Helper()
	s := runtime.NewScheme()
	if err := apiextv1.AddToScheme(s); err != nil {
		t.Fatalf("adding apiextensions to scheme: %v", err)
	}
	c, err := client.New(cfg, client.Options{Scheme: s})
	if err != nil {
		t.Fatalf("building CRD client: %v", err)
	}

	crd := &apiextv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "applications.argoproj.io"},
		Spec: apiextv1.CustomResourceDefinitionSpec{
			Group: "argoproj.io",
			Names: apiextv1.CustomResourceDefinitionNames{
				Plural:   "applications",
				Singular: "application",
				Kind:     "Application",
				ListKind: "ApplicationList",
			},
			Scope: apiextv1.NamespaceScoped,
			Versions: []apiextv1.CustomResourceDefinitionVersion{{
				Name:    "v1alpha1",
				Served:  true,
				Storage: true,
				Schema: &apiextv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextv1.JSONSchemaProps{
						Type:                   "object",
						XPreserveUnknownFields: ptrBool(true),
					},
				},
			}},
		},
	}
	if err := c.Create(context.Background(), crd); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("creating Application CRD: %v", err)
	}

	// Wait for the CRD to be Established so the API is served.
	deadline := time.Now().Add(60 * time.Second)
	for {
		var got apiextv1.CustomResourceDefinition
		if err := c.Get(context.Background(), types.NamespacedName{Name: crd.Name}, &got); err == nil {
			for _, cond := range got.Status.Conditions {
				if cond.Type == apiextv1.Established && cond.Status == apiextv1.ConditionTrue {
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for Application CRD to become Established")
		}
		time.Sleep(time.Second)
	}
}

func newAppClient(t *testing.T, cfg *rest.Config) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	s.AddKnownTypeWithName(appGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(appGVK.GroupVersion().WithKind("ApplicationList"), &unstructured.UnstructuredList{})
	metav1.AddToGroupVersion(s, appGVK.GroupVersion())

	// Retry: the RESTMapper needs to discover the freshly installed CRD.
	deadline := time.Now().Add(30 * time.Second)
	for {
		c, err := client.New(cfg, client.Options{Scheme: s})
		if err == nil {
			probe := &unstructured.Unstructured{}
			probe.SetGroupVersionKind(appGVK)
			if listErr := c.List(context.Background(), &unstructured.UnstructuredList{Object: map[string]any{
				"apiVersion": "argoproj.io/v1alpha1", "kind": "ApplicationList",
			}}); listErr == nil {
				return c
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out building a client that can see Applications: %v", err)
		}
		time.Sleep(time.Second)
	}
}

func newApplication() *unstructured.Unstructured {
	app := &unstructured.Unstructured{}
	app.SetGroupVersionKind(appGVK)
	app.SetName("e2e-app")
	app.SetNamespace("default")
	_ = unstructured.SetNestedField(app.Object, map[string]any{
		"automated":   map[string]any{"prune": true, "selfHeal": true},
		"syncOptions": []any{"CreateNamespace=true"},
	}, "spec", "syncPolicy")
	return app
}

func getApplication(t *testing.T, c client.Client) *unstructured.Unstructured {
	t.Helper()
	app := &unstructured.Unstructured{}
	app.SetGroupVersionKind(appGVK)
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "e2e-app"}, app); err != nil {
		t.Fatalf("getting Application: %v", err)
	}
	return app
}
