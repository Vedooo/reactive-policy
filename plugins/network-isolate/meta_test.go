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

	"github.com/Vedooo/reactive-policy/internal/action"
)

func TestMetadata(t *testing.T) {
	var p plugin
	if p.Name() != "network.isolate" {
		t.Errorf("Name() = %q", p.Name())
	}
	if p.Description() == "" {
		t.Error("Description() should not be empty")
	}
	if !p.IsReversible() {
		t.Error("network.isolate should be reversible")
	}
	if len(p.RequiredPermissions()) == 0 {
		t.Error("RequiredPermissions() should not be empty")
	}
}

func TestExecuteNoClientFails(t *testing.T) {
	var p plugin
	res, err := p.Execute(context.Background(), action.ExecuteInput{
		Target:       deploymentTarget(),
		Params:       action.Params{},
		TemplateData: map[string]any{},
	})
	if err == nil {
		t.Fatal("Execute without a client in context should fail")
	}
	if res.Status != action.StatusFailed {
		t.Errorf("status = %q, want Failed", res.Status)
	}
}

func TestReverseWithoutNameFails(t *testing.T) {
	var p plugin
	if err := p.Reverse(context.Background(), action.Result{}); err == nil {
		t.Fatal("Reverse with no stored NetworkPolicy name should fail")
	}
}
