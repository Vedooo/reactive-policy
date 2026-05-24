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

// Package cli implements the `rp` command-line client. It is a thin, stateless
// reader of the Kubernetes API: every view is built from ReactivePolicy status,
// ActionAudit records, and the compiled-in plugin registry. No business logic
// lives here (see docs/ARCHITECTURE.md §1.2).
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Vedooo/reactive-policy/api/v1alpha1"
	"github.com/Vedooo/reactive-policy/internal/action"
	"github.com/Vedooo/reactive-policy/internal/prometheus"
)

// Factory carries the persistent flags and the dependencies every subcommand
// needs. The function fields are injectable so tests can supply a fake client
// and metric source.
type Factory struct {
	Namespace     string
	AllNamespaces bool
	Output        string
	Kubeconfig    string

	// NewClient builds the Kubernetes client. Defaults to a kubeconfig client.
	NewClient func() (client.Client, error)
	// NewProm builds a metric client for dry-run. Defaults to the HTTP client.
	NewProm prometheus.Factory
	// Registry is the action plugin registry. Defaults to the global registry.
	Registry *action.Registry
}

// Namespace selection for list/get operations. An empty string means all
// namespaces; otherwise the configured namespace (defaulting to "default").
func (f *Factory) listNamespace() string {
	if f.AllNamespaces {
		return ""
	}
	return f.namespace()
}

func (f *Factory) namespace() string {
	if f.Namespace != "" {
		return f.Namespace
	}
	return "default"
}

func (f *Factory) output() string {
	if f.Output == "" {
		return outputTable
	}
	return f.Output
}

func (f *Factory) registry() *action.Registry {
	if f.Registry != nil {
		return f.Registry
	}
	return action.Default()
}

func (f *Factory) client() (client.Client, error) {
	if f.NewClient != nil {
		return f.NewClient()
	}
	return f.defaultClient()
}

func (f *Factory) prom() prometheus.Factory {
	if f.NewProm != nil {
		return f.NewProm
	}
	return prometheus.NewClient
}

// defaultClient builds a controller-runtime client from the kubeconfig,
// honoring an explicit --kubeconfig path and the standard loading rules.
func (f *Factory) defaultClient() (client.Client, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if f.Kubeconfig != "" {
		loadingRules.ExplicitPath = f.Kubeconfig
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}
	return client.New(cfg, client.Options{Scheme: scheme()})
}

// scheme returns a scheme registering the core Kubernetes types and the
// reactive-policy CRDs.
func scheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(v1alpha1.AddToScheme(s))
	return s
}

// NewRootCommand builds the `rp` root command and wires its subcommands. The
// returned Factory is bound to the persistent flags and is shared by every
// subcommand; tests mutate its function fields before executing.
func NewRootCommand() (*cobra.Command, *Factory) {
	f := &Factory{}
	root := &cobra.Command{
		Use:   "rp",
		Short: "Inspect and operate reactive-policy from the command line",
		Long: "rp is the command-line client for reactive-policy. It reads policies,\n" +
			"action audit history, and installed plugins directly from the Kubernetes API.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	pf := root.PersistentFlags()
	pf.StringVarP(&f.Namespace, "namespace", "n", "", "Namespace to operate in (defaults to \"default\")")
	pf.BoolVarP(&f.AllNamespaces, "all-namespaces", "A", false, "List across all namespaces")
	pf.StringVarP(&f.Output, "output", "o", "table", "Output format: table, json, or yaml")
	pf.StringVar(&f.Kubeconfig, "kubeconfig", "", "Path to the kubeconfig file (defaults to standard loading rules)")

	root.AddCommand(newPolicyCommand(f))
	root.AddCommand(newActionCommand(f))
	root.AddCommand(newPluginCommand(f))
	return root, f
}
