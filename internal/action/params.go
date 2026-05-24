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

package action

import (
	"encoding/json"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// ParamsFromCRD converts the CRD's parameter map into the Params type plugins
// consume. It returns nil for nil input.
func ParamsFromCRD(in map[string]apiextensionsv1.JSON) Params {
	if in == nil {
		return nil
	}
	out := make(Params, len(in))
	for k, v := range in {
		out[k] = runtime.RawExtension{Raw: v.Raw}
	}
	return out
}

// Unmarshal decodes params into out, which must be a pointer to a struct whose
// JSON tags match the parameter names. Plugins use it in Validate and Execute to
// obtain a typed view of their parameters.
func Unmarshal(p Params, out any) error {
	raw := make(map[string]json.RawMessage, len(p))
	for k, v := range p {
		if len(v.Raw) == 0 {
			continue
		}
		raw[k] = json.RawMessage(v.Raw)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("encoding params: %w", err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidParams, err)
	}
	return nil
}
