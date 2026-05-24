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
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/Vedooo/reactive-policy/api/v1alpha1"
	"github.com/Vedooo/reactive-policy/internal/action"
	"github.com/Vedooo/reactive-policy/internal/prometheus"
)

func newPolicyCommand(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "policy",
		Aliases: []string{"policies", "rp"},
		Short:   "Inspect ReactivePolicy resources",
	}
	cmd.AddCommand(newPolicyListCommand(f))
	cmd.AddCommand(newPolicyGetCommand(f))
	cmd.AddCommand(newPolicyDescribeCommand(f))
	cmd.AddCommand(newPolicyDryRunCommand(f))
	return cmd
}

func newPolicyListCommand(f *Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List policies and their status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			var list v1alpha1.ReactivePolicyList
			if err := c.List(cmd.Context(), &list, client.InNamespace(f.listNamespace())); err != nil {
				return fmt.Errorf("listing policies: %w", err)
			}
			if handled, err := printObject(cmd.OutOrStdout(), f.output(), &list); handled {
				return err
			}
			return printPolicyTable(cmd.OutOrStdout(), list.Items, f.AllNamespaces)
		},
	}
}

func newPolicyGetCommand(f *Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Show a single policy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			var policy v1alpha1.ReactivePolicy
			if err := c.Get(cmd.Context(), types.NamespacedName{Namespace: f.namespace(), Name: args[0]}, &policy); err != nil {
				return fmt.Errorf("getting policy %q: %w", args[0], err)
			}
			if handled, err := printObject(cmd.OutOrStdout(), f.output(), &policy); handled {
				return err
			}
			return printPolicyTable(cmd.OutOrStdout(), []v1alpha1.ReactivePolicy{policy}, f.AllNamespaces)
		},
	}
}

func newPolicyDescribeCommand(f *Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "describe <name>",
		Short: "Show a policy's full configuration and status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := f.client()
			if err != nil {
				return err
			}
			var policy v1alpha1.ReactivePolicy
			if err := c.Get(cmd.Context(), types.NamespacedName{Namespace: f.namespace(), Name: args[0]}, &policy); err != nil {
				return fmt.Errorf("getting policy %q: %w", args[0], err)
			}
			if handled, err := printObject(cmd.OutOrStdout(), f.output(), &policy); handled {
				return err
			}
			return describePolicy(cmd.OutOrStdout(), &policy)
		},
	}
}

func newPolicyDryRunCommand(f *Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "dry-run <file>",
		Short: "Simulate a policy against current metric values without applying it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("reading policy file: %w", err)
			}
			var policy v1alpha1.ReactivePolicy
			if err := yaml.Unmarshal(raw, &policy); err != nil {
				return fmt.Errorf("parsing policy file: %w", err)
			}
			report := dryRun(cmd.Context(), f, &policy)
			if handled, err := printObject(cmd.OutOrStdout(), f.output(), report); handled {
				return err
			}
			return printDryRun(cmd.OutOrStdout(), report)
		},
	}
}

func printPolicyTable(w io.Writer, items []v1alpha1.ReactivePolicy, allNamespaces bool) error {
	tw := newTab(w)
	if allNamespaces {
		fmt.Fprintln(tw, "NAMESPACE\tNAME\tSTATE\tLAST TRIGGERED\tCOUNT\tVALUE\tAGE")
	} else {
		fmt.Fprintln(tw, "NAME\tSTATE\tLAST TRIGGERED\tCOUNT\tVALUE\tAGE")
	}
	for i := range items {
		p := &items[i]
		state := colorize(string(p.Status.State))
		last := "<never>"
		if p.Status.LastTriggeredAt != nil {
			last = age(*p.Status.LastTriggeredAt)
		}
		value := p.Status.CurrentMetricValue
		if value == "" {
			value = "<none>"
		}
		if allNamespaces {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
				p.Namespace, p.Name, state, last, p.Status.TriggerCount, value, age(p.CreationTimestamp))
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n",
				p.Name, state, last, p.Status.TriggerCount, value, age(p.CreationTimestamp))
		}
	}
	return tw.Flush()
}

func describePolicy(w io.Writer, p *v1alpha1.ReactivePolicy) error {
	fmt.Fprintf(w, "Name:        %s\n", p.Name)
	fmt.Fprintf(w, "Namespace:   %s\n", p.Namespace)
	fmt.Fprintf(w, "State:       %s\n", colorize(string(p.Status.State)))
	fmt.Fprintf(w, "Value:       %s\n", orNone(p.Status.CurrentMetricValue))
	fmt.Fprintf(w, "Triggers:    %d\n", p.Status.TriggerCount)
	if p.Status.LastTriggeredAt != nil {
		fmt.Fprintf(w, "Last fired:  %s ago\n", age(*p.Status.LastTriggeredAt))
	}

	o := p.Spec.Observe
	fmt.Fprintln(w, "Observe:")
	fmt.Fprintf(w, "  Source:    %s\n", o.Source)
	fmt.Fprintf(w, "  Endpoint:  %s\n", o.Endpoint)
	fmt.Fprintf(w, "  Query:     %s\n", o.Query)
	fmt.Fprintf(w, "  Condition: value %s %s for %s\n", o.Operator, o.Threshold, o.Duration.Duration)
	if o.PollInterval.Duration > 0 {
		fmt.Fprintf(w, "  Poll:      %s\n", o.PollInterval.Duration)
	}

	fmt.Fprintf(w, "Cooldown:    %s\n", p.Spec.Cooldown.Duration)
	if p.Spec.MaxTriggersPerHour != nil {
		fmt.Fprintf(w, "Max/hour:    %d\n", *p.Spec.MaxTriggersPerHour)
	}

	fmt.Fprintln(w, "Actions:")
	for i := range p.Spec.Actions {
		a := p.Spec.Actions[i]
		onFailure := a.OnFailure
		if onFailure == "" {
			onFailure = v1alpha1.FailureStop
		}
		fmt.Fprintf(w, "  %d. %s (onFailure=%s)\n", i, a.Plugin, onFailure)
	}

	if len(p.Status.Conditions) > 0 {
		fmt.Fprintln(w, "Conditions:")
		tw := newTab(w)
		fmt.Fprintln(tw, "  TYPE\tSTATUS\tREASON\tMESSAGE")
		for _, c := range p.Status.Conditions {
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", c.Type, c.Status, c.Reason, c.Message)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	return nil
}

// dryRunReport is the machine-readable result of a dry-run.
type dryRunReport struct {
	Policy           string         `json:"policy"`
	Namespace        string         `json:"namespace"`
	MetricValue      string         `json:"metricValue,omitempty"`
	Threshold        string         `json:"threshold"`
	Operator         string         `json:"operator"`
	Crossed          bool           `json:"crossed"`
	DurationRequired string         `json:"durationRequired"`
	QueryError       string         `json:"queryError,omitempty"`
	Actions          []dryRunAction `json:"actions"`
	WouldTrigger     bool           `json:"wouldTrigger"`
}

type dryRunAction struct {
	Index      int    `json:"index"`
	Plugin     string `json:"plugin"`
	Reversible bool   `json:"reversible"`
	OnFailure  string `json:"onFailure"`
	Valid      bool   `json:"valid"`
	Error      string `json:"error,omitempty"`
}

// dryRun evaluates a policy against the live metric source and validates its
// actions, without mutating any cluster state.
func dryRun(ctx context.Context, f *Factory, policy *v1alpha1.ReactivePolicy) *dryRunReport {
	report := &dryRunReport{
		Policy:           policy.Name,
		Namespace:        policy.Namespace,
		Threshold:        policy.Spec.Observe.Threshold,
		Operator:         string(policy.Spec.Observe.Operator),
		DurationRequired: policy.Spec.Observe.Duration.Duration.String(),
	}

	reg := f.registry()
	actionsValid := true
	for i := range policy.Spec.Actions {
		a := policy.Spec.Actions[i]
		entry := dryRunAction{Index: i, Plugin: a.Plugin, OnFailure: string(a.OnFailure), Valid: true}
		if entry.OnFailure == "" {
			entry.OnFailure = string(v1alpha1.FailureStop)
		}
		plugin := reg.Lookup(a.Plugin)
		switch {
		case plugin == nil:
			entry.Valid = false
			entry.Error = "plugin not registered"
		default:
			entry.Reversible = plugin.IsReversible()
			if err := plugin.Validate(action.ParamsFromCRD(a.Params)); err != nil {
				entry.Valid = false
				entry.Error = err.Error()
			}
		}
		if !entry.Valid {
			actionsValid = false
		}
		report.Actions = append(report.Actions, entry)
	}

	promClient, err := f.prom()(policy.Spec.Observe.Endpoint)
	if err != nil {
		report.QueryError = err.Error()
		return report
	}
	value, err := promClient.Query(ctx, policy.Spec.Observe.Query)
	if err != nil {
		report.QueryError = err.Error()
		return report
	}
	report.MetricValue = formatFloat(value)

	threshold, err := prometheus.ParseThreshold(policy.Spec.Observe.Threshold)
	if err != nil {
		report.QueryError = fmt.Sprintf("invalid threshold: %v", err)
		return report
	}
	report.Crossed = prometheus.Compare(value, threshold, policy.Spec.Observe.Operator)
	report.WouldTrigger = report.Crossed && actionsValid
	return report
}

func printDryRun(w io.Writer, r *dryRunReport) error {
	fmt.Fprintf(w, "Policy:    %s/%s\n", r.Namespace, r.Policy)
	if r.QueryError != "" {
		fmt.Fprintf(w, "Metric:    error: %s\n", r.QueryError)
	} else {
		fmt.Fprintf(w, "Metric:    %s (threshold: %s %s)\n", r.MetricValue, r.Operator, r.Threshold)
		fmt.Fprintf(w, "Crossed:   %t (must hold for %s before a real trigger)\n", r.Crossed, r.DurationRequired)
	}

	fmt.Fprintln(w, "Actions that would run:")
	for _, a := range r.Actions {
		status := "ok"
		if !a.Valid {
			status = "INVALID: " + a.Error
		}
		fmt.Fprintf(w, "  %d. %s (reversible=%t, onFailure=%s) [%s]\n",
			a.Index, a.Plugin, a.Reversible, a.OnFailure, status)
	}

	fmt.Fprintf(w, "Would trigger now: %t\n", r.WouldTrigger)
	return nil
}

func orNone(s string) string {
	if s == "" {
		return "<none>"
	}
	return s
}
