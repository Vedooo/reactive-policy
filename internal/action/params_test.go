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

package action_test

import (
	"errors"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/Vedooo/reactive-policy/internal/action"
)

func TestParamsFromCRDRoundTrip(t *testing.T) {
	crd := map[string]apiextensionsv1.JSON{
		"channel":  {Raw: []byte(`"#sre-alerts"`)},
		"severity": {Raw: []byte(`"warning"`)},
	}

	var typed struct {
		Channel  string `json:"channel"`
		Severity string `json:"severity"`
	}
	if err := action.Unmarshal(action.ParamsFromCRD(crd), &typed); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if typed.Channel != "#sre-alerts" || typed.Severity != "warning" {
		t.Errorf("decoded %+v, want channel=#sre-alerts severity=warning", typed)
	}
}

func TestParamsFromCRDNilIsNil(t *testing.T) {
	if action.ParamsFromCRD(nil) != nil {
		t.Error("ParamsFromCRD(nil) should be nil")
	}
}

func TestUnmarshalInvalidParamsError(t *testing.T) {
	p := action.Params{"count": {Raw: []byte(`"not-an-int"`)}}

	var typed struct {
		Count int `json:"count"`
	}
	err := action.Unmarshal(p, &typed)
	if err == nil {
		t.Fatal("expected an error decoding a string into an int field")
	}
	if !errors.Is(err, action.ErrInvalidParams) {
		t.Errorf("error %v should wrap ErrInvalidParams", err)
	}
}
