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
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"
)

func newPluginCommand(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "plugin",
		Aliases: []string{"plugins"},
		Short:   "Inspect installed action plugins",
	}
	cmd.AddCommand(newPluginListCommand(f))
	return cmd
}

// pluginInfo is the machine-readable description of one installed plugin.
type pluginInfo struct {
	Name        string `json:"name"`
	Reversible  bool   `json:"reversible"`
	Permissions int    `json:"permissions"`
	Description string `json:"description"`
}

func newPluginListCommand(f *Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the action plugins compiled into this build",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			plugins := f.registry().All()
			infos := make([]pluginInfo, 0, len(plugins))
			for _, p := range plugins {
				infos = append(infos, pluginInfo{
					Name:        p.Name(),
					Reversible:  p.IsReversible(),
					Permissions: len(p.RequiredPermissions()),
					Description: p.Description(),
				})
			}
			sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })

			if handled, err := printObject(cmd.OutOrStdout(), f.output(), infos); handled {
				return err
			}
			return printPluginTable(cmd.OutOrStdout(), infos)
		},
	}
}

func printPluginTable(w io.Writer, infos []pluginInfo) error {
	tw := newTab(w)
	fmt.Fprintln(tw, "NAME\tREVERSIBLE\tPERMISSIONS\tDESCRIPTION")
	for _, p := range infos {
		fmt.Fprintf(tw, "%s\t%t\t%d\t%s\n", p.Name, p.Reversible, p.Permissions, p.Description)
	}
	return tw.Flush()
}
