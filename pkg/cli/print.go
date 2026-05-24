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

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/duration"
	"sigs.k8s.io/yaml"
)

const (
	outputTable = "table"
	outputJSON  = "json"
	outputYAML  = "yaml"
)

// colorEnabled is true when stdout is an interactive terminal, so status text is
// only colorized for humans and never in pipes, files, or test buffers.
var colorEnabled = isTerminal(os.Stdout)

// newTab returns a tab writer tuned for the aligned tables the CLI prints.
func newTab(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 2, 3, ' ', 0)
}

// formatFloat renders a metric value compactly, matching the operator's status
// formatting so dry-run and live values look identical.
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// age renders a Kubernetes timestamp the way kubectl does, e.g. "5m" or "3d".
func age(t metav1.Time) string {
	if t.IsZero() {
		return "<unknown>"
	}
	return duration.HumanDuration(time.Since(t.Time))
}

// colorize wraps s in an ANSI color when stdout is a terminal, otherwise returns
// it unchanged. status drives the color: green for success, red for failure,
// yellow for skipped.
func colorize(status string) string {
	if !colorEnabled {
		return status
	}
	switch status {
	case "Succeeded", "Watching":
		return "\033[32m" + status + "\033[0m" // green
	case "Failed", "Invalid":
		return "\033[31m" + status + "\033[0m" // red
	case "Skipped", "Cooldown", "RateLimited":
		return "\033[33m" + status + "\033[0m" // yellow
	default:
		return status
	}
}

// printObject renders a single typed object as json or yaml. It returns false
// when the requested format is the table format, leaving table rendering to the
// caller.
func printObject(w io.Writer, output string, obj any) (bool, error) {
	if output == outputJSON || output == outputYAML {
		ensureGVK(obj)
	}
	switch output {
	case outputJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return true, enc.Encode(obj)
	case outputYAML:
		raw, err := yaml.Marshal(obj)
		if err != nil {
			return true, fmt.Errorf("marshaling yaml: %w", err)
		}
		_, err = w.Write(raw)
		return true, err
	default:
		return false, nil
	}
}

// ensureGVK stamps a typed object's apiVersion and kind so json and yaml output
// is self-describing and round-trippable, the way kubectl prints. Objects the
// scheme does not recognize (plain report structs) are left untouched.
func ensureGVK(obj any) {
	ro, ok := obj.(runtime.Object)
	if !ok {
		return
	}
	if gvks, _, err := scheme().ObjectKinds(ro); err == nil && len(gvks) > 0 {
		ro.GetObjectKind().SetGroupVersionKind(gvks[0])
	}
}
