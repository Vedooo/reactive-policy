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
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Vedooo/reactive-policy/api/v1alpha1"
)

func newActionCommand(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "action",
		Aliases: []string{"actions", "audit"},
		Short:   "Inspect and revert recorded action executions",
	}
	cmd.AddCommand(newActionAuditCommand(f))
	cmd.AddCommand(newActionHistoryCommand(f))
	cmd.AddCommand(newActionRevertCommand(f))
	return cmd
}

func newActionAuditCommand(f *Factory) *cobra.Command {
	var since string
	var policyName string
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Show recent action executions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			window, err := time.ParseDuration(since)
			if err != nil {
				return fmt.Errorf("invalid --since %q: %w", since, err)
			}
			audits, err := f.listAudits(cmd, policyName)
			if err != nil {
				return err
			}
			cutoff := time.Now().Add(-window)
			recent := audits[:0]
			for i := range audits {
				if audits[i].Spec.TriggeredAt.Time.After(cutoff) {
					recent = append(recent, audits[i])
				}
			}
			sortAudits(recent)
			if handled, err := printObject(cmd.OutOrStdout(), f.output(), &v1alpha1.ActionAuditList{Items: recent}); handled {
				return err
			}
			return printAuditTable(cmd.OutOrStdout(), recent, f.AllNamespaces)
		},
	}
	cmd.Flags().StringVar(&since, "since", "1h", "Only show executions newer than this duration")
	cmd.Flags().StringVar(&policyName, "policy", "", "Only show executions from this policy")
	return cmd
}

func newActionHistoryCommand(f *Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "history <policy>",
		Short: "Show the per-action history for a policy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			audits, err := f.listAudits(cmd, args[0])
			if err != nil {
				return err
			}
			sortAudits(audits)
			if handled, err := printObject(cmd.OutOrStdout(), f.output(), &v1alpha1.ActionAuditList{Items: audits}); handled {
				return err
			}
			return printHistoryTable(cmd.OutOrStdout(), audits)
		},
	}
}

func newActionRevertCommand(f *Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "revert <audit-name>",
		Short: "Request the operator to reverse a recorded execution",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			var audit v1alpha1.ActionAudit
			nn := types.NamespacedName{Namespace: f.namespace(), Name: args[0]}
			if err := c.Get(cmd.Context(), nn, &audit); err != nil {
				return fmt.Errorf("getting audit record %q: %w", args[0], err)
			}
			if audit.Status.Reverted {
				fmt.Fprintf(cmd.OutOrStdout(), "audit %q was already reverted at %s\n", audit.Name, audit.Status.RevertedAt)
				return nil
			}
			audit.Spec.RevertRequested = true
			if err := c.Update(cmd.Context(), &audit); err != nil {
				return fmt.Errorf("requesting revert: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "requested revert of audit %q; the operator will reverse its reversible actions\n", audit.Name)
			return nil
		},
	}
}

// listAudits fetches audit records in the selected namespace, optionally
// filtered to a single policy via the policy label.
func (f *Factory) listAudits(cmd *cobra.Command, policyName string) ([]v1alpha1.ActionAudit, error) {
	c, err := f.client()
	if err != nil {
		return nil, err
	}
	opts := []client.ListOption{client.InNamespace(f.listNamespace())}
	if policyName != "" {
		opts = append(opts, client.MatchingLabels{v1alpha1.LabelPolicy: policyName})
	}
	var list v1alpha1.ActionAuditList
	if err := c.List(cmd.Context(), &list, opts...); err != nil {
		return nil, fmt.Errorf("listing audit records: %w", err)
	}
	return list.Items, nil
}

// sortAudits orders records newest trigger first.
func sortAudits(items []v1alpha1.ActionAudit) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Spec.TriggeredAt.Time.After(items[j].Spec.TriggeredAt.Time)
	})
}

func printAuditTable(w io.Writer, items []v1alpha1.ActionAudit, allNamespaces bool) error {
	tw := newTab(w)
	if allNamespaces {
		fmt.Fprintln(tw, "NAMESPACE\tNAME\tPOLICY\tTRIGGERED\tACTIONS\tOUTCOME\tREVERTED")
	} else {
		fmt.Fprintln(tw, "NAME\tPOLICY\tTRIGGERED\tACTIONS\tOUTCOME\tREVERTED")
	}
	for i := range items {
		a := &items[i]
		outcome := colorize(auditOutcome(a))
		when := age(a.Spec.TriggeredAt) + " ago"
		if allNamespaces {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\t%t\n",
				a.Namespace, a.Name, a.Spec.PolicyRef, when, len(a.Spec.Actions), outcome, a.Status.Reverted)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%t\n",
				a.Name, a.Spec.PolicyRef, when, len(a.Spec.Actions), outcome, a.Status.Reverted)
		}
	}
	return tw.Flush()
}

func printHistoryTable(w io.Writer, items []v1alpha1.ActionAudit) error {
	tw := newTab(w)
	fmt.Fprintln(tw, "TRIGGERED\tAUDIT\tACTION\tSTATUS\tREVERSIBLE\tMESSAGE")
	for i := range items {
		a := &items[i]
		when := age(a.Spec.TriggeredAt) + " ago"
		for _, rec := range a.Spec.Actions {
			fmt.Fprintf(tw, "%s\t%s\t%d.%s\t%s\t%t\t%s\n",
				when, a.Name, rec.Index, rec.Plugin, colorize(rec.Status), rec.Reversible, truncate(rec.Message, 48))
		}
	}
	return tw.Flush()
}

// auditOutcome summarizes a record's per-action statuses into one word.
func auditOutcome(a *v1alpha1.ActionAudit) string {
	failed := false
	succeeded := 0
	for _, rec := range a.Spec.Actions {
		switch rec.Status {
		case "Failed":
			failed = true
		case "Succeeded":
			succeeded++
		}
	}
	switch {
	case failed:
		return "Failed"
	case len(a.Spec.Actions) > 0 && succeeded == len(a.Spec.Actions):
		return "Succeeded"
	default:
		return "Skipped"
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
