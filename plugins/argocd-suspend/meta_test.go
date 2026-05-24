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

	"github.com/Vedooo/reactive-policy/internal/action"
)

func TestMetadata(t *testing.T) {
	var p plugin
	if p.Name() != "argocd.suspend" {
		t.Errorf("Name() = %q", p.Name())
	}
	if p.Description() == "" {
		t.Error("Description() should not be empty")
	}
	if !p.IsReversible() {
		t.Error("argocd.suspend should be reversible")
	}
	rules := p.RequiredPermissions()
	if len(rules) != 1 || rules[0].APIGroups[0] != "argoproj.io" || rules[0].Resources[0] != "applications" {
		t.Errorf("RequiredPermissions() = %+v, want argoproj.io/applications", rules)
	}
}

func TestValidateAcceptsEmptyAndReason(t *testing.T) {
	var p plugin
	if err := p.Validate(nil); err != nil {
		t.Errorf("Validate(nil) = %v, want nil (reason is optional)", err)
	}
	if err := p.Validate(action.Params{"reason": {Raw: []byte(`"because"`)}}); err != nil {
		t.Errorf("Validate with reason = %v, want nil", err)
	}
}

func TestExecuteNoClientFails(t *testing.T) {
	var p plugin
	res, err := p.Execute(context.Background(), action.ExecuteInput{Target: appTarget(), TemplateData: map[string]any{}})
	if err == nil {
		t.Fatal("Execute without a client in context should fail")
	}
	if res.Status != action.StatusFailed {
		t.Errorf("status = %q, want Failed", res.Status)
	}
}

func TestExecuteTargetNotFound(t *testing.T) {
	c := newClient() // no application objects
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	_, err := p.Execute(ctx, action.ExecuteInput{Target: appTarget(), TemplateData: map[string]any{}})
	if err == nil {
		t.Fatal("Execute against a missing application should fail")
	}
}
