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

// maxWeight is the upper bound the Gateway API allows for a backendRef weight.
const maxWeight = 1_000_000

// routeRef identifies the Gateway API HTTPRoute whose backend weights the
// action adjusts.
type routeRef struct {
	Name string `json:"name"`
	// Namespace defaults to the triggering policy's namespace when empty.
	Namespace string `json:"namespace,omitempty"`
}

// params is the typed view of a mesh.shift action's parameters
// (see docs/PLUGIN_INTERFACE.md §5.2).
type params struct {
	// RouteRef is the HTTPRoute to adjust.
	RouteRef routeRef `json:"routeRef"`
	// Backend is the name of the backendRef whose weight is overridden.
	Backend string `json:"backend"`
	// Weight is the weight to set on the matched backendRef; defaults to 0,
	// which drains all traffic from that backend onto the others.
	Weight *int32 `json:"weight,omitempty"`
}

func (p params) weight() int32 {
	if p.Weight == nil {
		return 0
	}
	return *p.Weight
}
