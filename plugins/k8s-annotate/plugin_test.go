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

package k8sannotate

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/Vedooo/reactive-policy/internal/action"
)

func raw(s string) []byte { return []byte(s) }

const annotationKey = "reactive-policy.io/last-trigger"

func deploymentTarget() action.Target {
	return action.Target{APIVersion: "apps/v1", Kind: "Deployment", Name: "api", Namespace: "default"}
}

func newClientWithDeployment(annotations map[string]string) client.Client {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default", Annotations: annotations},
	}
	return fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(dep).Build()
}

func deploymentAnnotations(t *testing.T, c client.Client) map[string]string {
	t.Helper()
	var dep appsv1.Deployment
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "api"}, &dep); err != nil {
		t.Fatalf("getting deployment: %v", err)
	}
	return dep.GetAnnotations()
}

func TestValidate(t *testing.T) {
	cases := map[string]struct {
		params  action.Params
		wantErr bool
	}{
		"valid":          {params: action.Params{"key": {Raw: raw(`"k"`)}, "value": {Raw: raw(`"v"`)}}},
		"missing key":    {params: action.Params{"value": {Raw: raw(`"v"`)}}, wantErr: true},
		"missing value":  {params: action.Params{"key": {Raw: raw(`"k"`)}}, wantErr: true},
		"bad template":   {params: action.Params{"key": {Raw: raw(`"k"`)}, "value": {Raw: raw(`"{{ .X "`)}}, wantErr: true},
		"valid template": {params: action.Params{"key": {Raw: raw(`"k"`)}, "value": {Raw: raw(`"{{ .MetricValue }}"`)}}},
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

func TestExecuteAnnotatesTarget(t *testing.T) {
	c := newClientWithDeployment(nil)
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	res, err := p.Execute(ctx, action.ExecuteInput{
		Target:       deploymentTarget(),
		Params:       action.Params{"key": {Raw: raw(`"` + annotationKey + `"`)}, "value": {Raw: raw(`"value={{ .MetricValue }}"`)}},
		MetricValue:  "0.9",
		TemplateData: map[string]any{"MetricValue": "0.9"},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if res.Status != action.StatusSucceeded {
		t.Fatalf("status = %q, want Succeeded", res.Status)
	}
	if got := deploymentAnnotations(t, c)[annotationKey]; got != "value=0.9" {
		t.Errorf("annotation = %q, want value=0.9", got)
	}
}

func TestExecuteRespectsOverwriteFalse(t *testing.T) {
	c := newClientWithDeployment(map[string]string{annotationKey: "original"})
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	res, err := p.Execute(ctx, action.ExecuteInput{
		Target: deploymentTarget(),
		Params: action.Params{
			"key":       {Raw: raw(`"` + annotationKey + `"`)},
			"value":     {Raw: raw(`"new"`)},
			"overwrite": {Raw: raw(`false`)},
		},
		TemplateData: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if res.Status != action.StatusSkipped {
		t.Fatalf("status = %q, want Skipped", res.Status)
	}
	if got := deploymentAnnotations(t, c)[annotationKey]; got != "original" {
		t.Errorf("annotation = %q, want unchanged original", got)
	}
}

func TestExecuteTargetNotFound(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	_, err := p.Execute(ctx, action.ExecuteInput{
		Target:       deploymentTarget(),
		Params:       action.Params{"key": {Raw: raw(`"k"`)}, "value": {Raw: raw(`"v"`)}},
		TemplateData: map[string]any{},
	})
	if err == nil {
		t.Fatal("Execute against a missing target should fail")
	}
}

func TestReverseRemovesAnnotation(t *testing.T) {
	c := newClientWithDeployment(map[string]string{annotationKey: "value=0.9", "keep": "me"})
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	prev := action.Result{
		PluginName: pluginName,
		Target:     deploymentTarget(),
		Details:    map[string]any{"key": annotationKey},
	}
	if err := p.Reverse(ctx, prev); err != nil {
		t.Fatalf("Reverse returned error: %v", err)
	}

	ann := deploymentAnnotations(t, c)
	if _, ok := ann[annotationKey]; ok {
		t.Error("Reverse should have removed the annotation key")
	}
	if ann["keep"] != "me" {
		t.Error("Reverse should not touch other annotations")
	}
}
