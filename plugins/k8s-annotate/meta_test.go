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

	"github.com/Vedooo/reactive-policy/internal/action"
)

func TestMetadata(t *testing.T) {
	var p plugin
	if p.Name() != "k8s.annotate" {
		t.Errorf("Name() = %q", p.Name())
	}
	if p.Description() == "" {
		t.Error("Description() should not be empty")
	}
	if !p.IsReversible() {
		t.Error("k8s.annotate should be reversible")
	}
	if len(p.RequiredPermissions()) == 0 {
		t.Error("RequiredPermissions() should not be empty")
	}
}

func TestExecuteNoClientFails(t *testing.T) {
	var p plugin
	res, err := p.Execute(context.Background(), action.ExecuteInput{
		Target:       deploymentTarget(),
		Params:       action.Params{"key": {Raw: raw(`"k"`)}, "value": {Raw: raw(`"v"`)}},
		TemplateData: map[string]any{},
	})
	if err == nil {
		t.Fatal("Execute without a client in context should fail")
	}
	if res.Status != action.StatusFailed {
		t.Errorf("status = %q, want Failed", res.Status)
	}
}

func TestReverseWithoutKeyFails(t *testing.T) {
	var p plugin
	if err := p.Reverse(context.Background(), action.Result{}); err == nil {
		t.Fatal("Reverse with no stored key should fail")
	}
}

func TestReverseTargetNotFoundIsNoOp(t *testing.T) {
	c := newClientWithDeployment(nil) // deployment "api" exists, but target a missing one
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	prev := action.Result{
		Target:  action.Target{APIVersion: "apps/v1", Kind: "Deployment", Name: "ghost", Namespace: "default"},
		Details: map[string]any{"key": annotationKey},
	}
	if err := p.Reverse(ctx, prev); err != nil {
		t.Fatalf("reversing a missing target should be a no-op, got: %v", err)
	}
}
