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

// params is the typed view of a network.isolate action's parameters
// (see docs/PLUGIN_INTERFACE.md §5.2).
type params struct {
	// PodSelector overrides the auto-detected pod selector. When set, these
	// matchLabels choose the pods the NetworkPolicy isolates.
	PodSelector map[string]string `json:"podSelector,omitempty"`
	// Direction is "ingress", "egress", or "both" (default "both").
	Direction string `json:"direction,omitempty"`
	// AllowDNS keeps DNS egress (port 53 UDP/TCP) open so isolated pods can
	// still resolve names; defaults to true and only applies when egress is
	// isolated.
	AllowDNS *bool `json:"allowDNS,omitempty"`
}

const (
	directionIngress = "ingress"
	directionEgress  = "egress"
	directionBoth    = "both"
)

func (p params) direction() string {
	if p.Direction == "" {
		return directionBoth
	}
	return p.Direction
}

func (p params) allowDNS() bool {
	return p.AllowDNS == nil || *p.AllowDNS
}

func (p params) isolatesIngress() bool {
	return p.direction() == directionIngress || p.direction() == directionBoth
}

func (p params) isolatesEgress() bool {
	return p.direction() == directionEgress || p.direction() == directionBoth
}
