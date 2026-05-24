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
	"fmt"
	"strings"
	"text/template"
)

// ValidateTemplate parses tmpl and returns an error if it is not a valid Go
// text/template. Plugins call it from Validate so bad templates fail at
// admission rather than at execution time (see docs/PLUGIN_INTERFACE.md §6).
func ValidateTemplate(tmpl string) error {
	if _, err := template.New("action").Option("missingkey=error").Parse(tmpl); err != nil {
		return fmt.Errorf("parsing template: %w", err)
	}
	return nil
}

// RenderTemplate executes tmpl against data and returns the rendered string. A
// reference to a key absent from data is an error, surfacing typos in policies.
func RenderTemplate(tmpl string, data map[string]any) (string, error) {
	t, err := template.New("action").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}
	var sb strings.Builder
	if err := t.Execute(&sb, data); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}
	return sb.String(), nil
}
