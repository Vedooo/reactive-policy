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

// Command rp is the reactive-policy command-line client. It links the same
// action plugins as the operator so `plugin list` and `policy dry-run` can
// report and validate against the real registry (docs/ARCHITECTURE.md §1.2).
package main

import (
	"fmt"
	"os"

	"github.com/Vedooo/reactive-policy/pkg/cli"

	// Built-in action plugins register themselves via their init() functions.
	_ "github.com/Vedooo/reactive-policy/plugins/argocd-suspend"
	_ "github.com/Vedooo/reactive-policy/plugins/k8s-annotate"
	_ "github.com/Vedooo/reactive-policy/plugins/notify-slack"
)

func main() {
	root, _ := cli.NewRootCommand()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
