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

	"github.com/Vedooo/reactive-policy/internal/action"
)

func TestMetadata(t *testing.T) {
	var p plugin
	if p.Name() != "mesh.shift" {
		t.Errorf("Name() = %q", p.Name())
	}
	if p.Description() == "" {
		t.Error("Description() should not be empty")
	}
	if !p.IsReversible() {
		t.Error("mesh.shift should be reversible")
	}
	if len(p.RequiredPermissions()) == 0 {
		t.Error("RequiredPermissions() should not be empty")
	}
}

func TestValidate(t *testing.T) {
	cases := map[string]struct {
		params  action.Params
		wantErr bool
	}{
		"valid":           {params: action.Params{"routeRef": {Raw: raw(`{"name":"r"}`)}, "backend": {Raw: raw(`"api"`)}}},
		"valid weight":    {params: action.Params{"routeRef": {Raw: raw(`{"name":"r"}`)}, "backend": {Raw: raw(`"api"`)}, "weight": {Raw: raw(`50`)}}},
		"missing route":   {params: action.Params{"backend": {Raw: raw(`"api"`)}}, wantErr: true},
		"missing backend": {params: action.Params{"routeRef": {Raw: raw(`{"name":"r"}`)}}, wantErr: true},
		"negative weight": {params: action.Params{"routeRef": {Raw: raw(`{"name":"r"}`)}, "backend": {Raw: raw(`"api"`)}, "weight": {Raw: raw(`-1`)}}, wantErr: true},
		"weight too high": {params: action.Params{"routeRef": {Raw: raw(`{"name":"r"}`)}, "backend": {Raw: raw(`"api"`)}, "weight": {Raw: raw(`2000000`)}}, wantErr: true},
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

func TestExecuteNoClientFails(t *testing.T) {
	var p plugin
	res, err := p.Execute(context.Background(), action.ExecuteInput{
		Target:       target(),
		Params:       action.Params{"routeRef": {Raw: raw(`{"name":"r"}`)}, "backend": {Raw: raw(`"api"`)}},
		TemplateData: map[string]any{},
	})
	if err == nil {
		t.Fatal("Execute without a client in context should fail")
	}
	if res.Status != action.StatusFailed {
		t.Errorf("status = %q, want Failed", res.Status)
	}
}

func TestReverseWithoutPreviousFails(t *testing.T) {
	var p plugin
	if err := p.Reverse(context.Background(), action.Result{Details: map[string]any{"routeName": "r"}}); err == nil {
		t.Fatal("Reverse with no recorded weights should fail")
	}
}
