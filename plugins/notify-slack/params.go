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

package notifyslack

// params is the typed view of a notify.slack action's parameters
// (see docs/PLUGIN_INTERFACE.md §5.1).
type params struct {
	WebhookSecretRef secretRef `json:"webhookSecretRef"`
	Channel          string    `json:"channel"`
	Severity         string    `json:"severity"`
	Template         string    `json:"template"`
}

// secretRef points at a key in a Secret in the policy's namespace.
type secretRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}
