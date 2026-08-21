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
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/Vedooo/reactive-policy/api/v1alpha1"
	acttest "github.com/Vedooo/reactive-policy/internal/action/testing"
	"github.com/Vedooo/reactive-policy/internal/prometheus"
)

type fakeProm struct {
	value float64
	err   error
}

func (f fakeProm) Query(context.Context, string) (float64, error) { return f.value, f.err }

// newTestCLI builds the root command with injected dependencies and a buffer
// capturing its output.
func newTestCLI(t *testing.T, objs ...client.Object) (*cobra.Command, *Factory, *bytes.Buffer) {
	t.Helper()
	c := fake.NewClientBuilder().WithScheme(scheme()).WithObjects(objs...).Build()
	root, f := NewRootCommand()
	f.NewClient = func() (client.Client, error) { return c, nil }
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	return root, f, buf
}

func execCLI(root *cobra.Command, args ...string) error {
	root.SetArgs(args)
	return root.Execute()
}

func samplePolicy(name string) *v1alpha1.ReactivePolicy {
	p := &v1alpha1.ReactivePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: v1alpha1.ReactivePolicySpec{
			Observe: v1alpha1.Observe{
				Source:    v1alpha1.SourcePrometheus,
				Endpoint:  "http://prom:9090",
				Query:     "vector(1)",
				Threshold: "0.05",
				Operator:  v1alpha1.OpGreaterThan,
				Duration:  metav1.Duration{Duration: time.Minute},
			},
			Cooldown: metav1.Duration{Duration: 5 * time.Minute},
			Actions:  []v1alpha1.Action{{Plugin: "nop"}},
		},
	}
	p.Status.State = v1alpha1.StateWatching
	p.Status.CurrentMetricValue = "0.01"
	p.Status.TriggerCount = 3
	return p
}

func sampleAudit(name, policy string, triggered time.Time, status string) *v1alpha1.ActionAudit {
	return &v1alpha1.ActionAudit{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{v1alpha1.LabelPolicy: policy},
		},
		Spec: v1alpha1.ActionAuditSpec{
			PolicyRef:   policy,
			TriggeredAt: metav1.Time{Time: triggered},
			Actions: []v1alpha1.ActionRecord{
				{Index: 0, Plugin: "nop", Status: status, Reversible: true, Message: "did a thing"},
			},
		},
	}
}

func TestPolicyList(t *testing.T) {
	root, _, buf := newTestCLI(t, samplePolicy("web"), samplePolicy("api"))
	if err := execCLI(root, "policy", "list"); err != nil {
		t.Fatalf("policy list: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"NAME", "STATE", "web", "api", "Watching"} {
		if !strings.Contains(out, want) {
			t.Errorf("policy list output missing %q:\n%s", want, out)
		}
	}
}

func TestPolicyListJSON(t *testing.T) {
	root, _, buf := newTestCLI(t, samplePolicy("web"))
	if err := execCLI(root, "policy", "list", "-o", "json"); err != nil {
		t.Fatalf("policy list -o json: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "ReactivePolicyList") || !strings.Contains(out, "\"web\"") {
		t.Errorf("json output unexpected:\n%s", out)
	}
}

func TestPolicyGet(t *testing.T) {
	root, _, buf := newTestCLI(t, samplePolicy("web"))
	if err := execCLI(root, "policy", "get", "web"); err != nil {
		t.Fatalf("policy get: %v", err)
	}
	if !strings.Contains(buf.String(), "web") {
		t.Errorf("policy get missing name:\n%s", buf.String())
	}
}

func TestPolicyGetNotFound(t *testing.T) {
	root, _, _ := newTestCLI(t, samplePolicy("web"))
	if err := execCLI(root, "policy", "get", "missing"); err == nil {
		t.Fatal("expected an error getting a missing policy")
	}
}

func TestPolicyDescribe(t *testing.T) {
	root, _, buf := newTestCLI(t, samplePolicy("web"))
	if err := execCLI(root, "policy", "describe", "web"); err != nil {
		t.Fatalf("policy describe: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Observe:", "Endpoint:", "Actions:", "nop"} {
		if !strings.Contains(out, want) {
			t.Errorf("describe output missing %q:\n%s", want, out)
		}
	}
}

func TestPolicyDryRun(t *testing.T) {
	root, f, buf := newTestCLI(t)
	f.Registry = acttest.NewFakeRegistry(acttest.NewNop("nop"))
	f.NewProm = func(string, ...prometheus.Option) (prometheus.Client, error) {
		return fakeProm{value: 0.9}, nil
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	doc := strings.Join([]string{
		"apiVersion: reactive-policy.io/v1alpha1",
		"kind: ReactivePolicy",
		"metadata:",
		"  name: dry",
		"  namespace: default",
		"spec:",
		"  target:",
		"    selector: {}",
		"    kinds:",
		"    - apiVersion: apps/v1",
		"      kind: Deployment",
		"  observe:",
		"    source: prometheus",
		"    endpoint: http://prom:9090",
		"    query: vector(1)",
		"    threshold: \"0.05\"",
		"    operator: GreaterThan",
		"    duration: 1m",
		"  actions:",
		"  - plugin: nop",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("write policy file: %v", err)
	}

	if err := execCLI(root, "policy", "dry-run", path); err != nil {
		t.Fatalf("policy dry-run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Would trigger now: true") {
		t.Errorf("dry-run should report a trigger:\n%s", out)
	}
	if !strings.Contains(out, "nop") {
		t.Errorf("dry-run should list the nop action:\n%s", out)
	}
}

func TestPolicyDryRunUnknownPlugin(t *testing.T) {
	root, f, buf := newTestCLI(t)
	f.Registry = acttest.NewFakeRegistry() // empty: nop is unknown
	f.NewProm = func(string, ...prometheus.Option) (prometheus.Client, error) {
		return fakeProm{value: 0.9}, nil
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	doc := "apiVersion: reactive-policy.io/v1alpha1\nkind: ReactivePolicy\nmetadata:\n  name: dry\nspec:\n  observe:\n    source: prometheus\n    endpoint: http://prom:9090\n    query: vector(1)\n    threshold: \"0.05\"\n    operator: GreaterThan\n    duration: 1m\n  actions:\n  - plugin: nope\n"
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := execCLI(root, "policy", "dry-run", path); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "INVALID") || !strings.Contains(out, "Would trigger now: false") {
		t.Errorf("dry-run should flag the unknown plugin and refuse to trigger:\n%s", out)
	}
}

func TestActionAuditSince(t *testing.T) {
	now := time.Now()
	root, _, buf := newTestCLI(t,
		sampleAudit("recent", "web", now.Add(-10*time.Minute), "Succeeded"),
		sampleAudit("old", "web", now.Add(-2*time.Hour), "Succeeded"),
	)
	if err := execCLI(root, "action", "audit", "--since", "1h"); err != nil {
		t.Fatalf("action audit: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "recent") {
		t.Errorf("audit should include the recent record:\n%s", out)
	}
	if strings.Contains(out, "old") {
		t.Errorf("audit should exclude the record older than --since:\n%s", out)
	}
}

func TestActionHistory(t *testing.T) {
	now := time.Now()
	root, _, buf := newTestCLI(t,
		sampleAudit("h1", "web", now.Add(-10*time.Minute), "Succeeded"),
		sampleAudit("other", "api", now.Add(-5*time.Minute), "Succeeded"),
	)
	if err := execCLI(root, "action", "history", "web"); err != nil {
		t.Fatalf("action history: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "0.nop") {
		t.Errorf("history should show the per-action row:\n%s", out)
	}
	if strings.Contains(out, "other") {
		t.Errorf("history should be scoped to the named policy:\n%s", out)
	}
}

func TestActionRevert(t *testing.T) {
	audit := sampleAudit("rev", "web", time.Now(), "Succeeded")
	c := fake.NewClientBuilder().WithScheme(scheme()).WithObjects(audit).Build()
	root, f := NewRootCommand()
	f.NewClient = func() (client.Client, error) { return c, nil }
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)

	if err := execCLI(root, "action", "revert", "rev"); err != nil {
		t.Fatalf("action revert: %v", err)
	}
	if !strings.Contains(buf.String(), "requested revert") {
		t.Errorf("revert should confirm the request:\n%s", buf.String())
	}

	var got v1alpha1.ActionAudit
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "rev"}, &got); err != nil {
		t.Fatalf("get audit: %v", err)
	}
	if !got.Spec.RevertRequested {
		t.Error("revert should set spec.revertRequested=true")
	}
}

func TestPluginList(t *testing.T) {
	root, f, buf := newTestCLI(t)
	nop := acttest.NewNop("nop")
	f.Registry = acttest.NewFakeRegistry(nop)
	if err := execCLI(root, "plugin", "list"); err != nil {
		t.Fatalf("plugin list: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "nop") || !strings.Contains(out, "REVERSIBLE") {
		t.Errorf("plugin list output unexpected:\n%s", out)
	}
}

func TestPluginListUsesDefaultRegistryByDefault(t *testing.T) {
	// With no override the factory falls back to the global registry; this build
	// links no plugins into the test binary, so the table is just the header.
	root, _, buf := newTestCLI(t)
	if err := execCLI(root, "plugin", "list"); err != nil {
		t.Fatalf("plugin list: %v", err)
	}
	if !strings.Contains(buf.String(), "NAME") {
		t.Errorf("plugin list should always print a header:\n%s", buf.String())
	}
}

// pendingAudit builds an audit record holding an open gate, as the operator
// would have written it.
func pendingAudit(name, policy string, expiresIn time.Duration) *v1alpha1.ActionAudit {
	a := &v1alpha1.ActionAudit{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{v1alpha1.LabelPolicy: policy},
		},
		Spec: v1alpha1.ActionAuditSpec{
			PolicyRef:   policy,
			TriggeredAt: metav1.Now(),
			MetricValue: "0.42",
			Actions: []v1alpha1.ActionRecord{
				{Index: 0, Plugin: "notify.slack", Status: "Succeeded"},
			},
			Gate: &v1alpha1.ApprovalGate{
				ActionIndex:    1,
				PendingPlugins: []string{"network.isolate"},
				Targets:        []v1alpha1.AuditTarget{{Kind: "Deployment", Name: "api"}},
				ExpiresAt:      metav1.Time{Time: time.Now().Add(expiresIn)},
			},
		},
	}
	a.Status.ApprovalPhase = v1alpha1.PhasePending
	return a
}

func TestActionPendingListsOpenGates(t *testing.T) {
	root, _, buf := newTestCLI(t,
		pendingAudit("held-1", "api-guard", time.Hour),
		sampleAudit("done-1", "api-guard", time.Now(), "Succeeded"),
	)
	if err := execCLI(root, "action", "pending"); err != nil {
		t.Fatalf("action pending: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "held-1") {
		t.Errorf("open gate missing from output:\n%s", out)
	}
	if strings.Contains(out, "done-1") {
		t.Errorf("a completed record should not be listed as pending:\n%s", out)
	}
	if !strings.Contains(out, "network.isolate") {
		t.Errorf("the held plugin is the evidence the approver needs:\n%s", out)
	}
}

func TestActionApproveRecordsTheDecision(t *testing.T) {
	audit := pendingAudit("held-2", "api-guard", time.Hour)
	root, f, buf := newTestCLI(t, audit)

	if err := execCLI(root, "action", "approve", "held-2", "--reason", "confirmed bad deploy"); err != nil {
		t.Fatalf("action approve: %v", err)
	}
	if !strings.Contains(buf.String(), "approved") {
		t.Errorf("expected confirmation output, got:\n%s", buf.String())
	}

	c, err := f.client()
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	var got v1alpha1.ActionAudit
	if err := c.Get(context.Background(), types.NamespacedName{Name: "held-2", Namespace: "default"}, &got); err != nil {
		t.Fatalf("re-reading audit: %v", err)
	}
	if got.Spec.Gate.Decision != v1alpha1.DecisionApproved {
		t.Errorf("decision = %q, want Approved", got.Spec.Gate.Decision)
	}
	if got.Spec.Gate.Reason != "confirmed bad deploy" {
		t.Errorf("reason = %q, want it recorded", got.Spec.Gate.Reason)
	}
	// The CLI must not name the approver; only the webhook may.
	if got.Spec.Gate.DecidedBy != "" {
		t.Errorf("decidedBy = %q, want empty — identity is stamped at admission", got.Spec.Gate.DecidedBy)
	}
}

func TestActionDenyRecordsTheDecision(t *testing.T) {
	root, f, _ := newTestCLI(t, pendingAudit("held-3", "api-guard", time.Hour))
	if err := execCLI(root, "action", "deny", "held-3"); err != nil {
		t.Fatalf("action deny: %v", err)
	}
	c, _ := f.client()
	var got v1alpha1.ActionAudit
	if err := c.Get(context.Background(), types.NamespacedName{Name: "held-3", Namespace: "default"}, &got); err != nil {
		t.Fatalf("re-reading audit: %v", err)
	}
	if got.Spec.Gate.Decision != v1alpha1.DecisionDenied {
		t.Errorf("decision = %q, want Denied", got.Spec.Gate.Decision)
	}
}

func TestActionApproveRefusesClosedAndLapsedGates(t *testing.T) {
	t.Run("expired", func(t *testing.T) {
		root, _, _ := newTestCLI(t, pendingAudit("held-4", "api-guard", -time.Minute))
		if err := execCLI(root, "action", "approve", "held-4"); err == nil {
			t.Error("approving a lapsed gate should fail")
		}
	})

	t.Run("already decided", func(t *testing.T) {
		audit := pendingAudit("held-5", "api-guard", time.Hour)
		audit.Status.ApprovalPhase = v1alpha1.PhaseApproved
		root, _, _ := newTestCLI(t, audit)
		if err := execCLI(root, "action", "approve", "held-5"); err == nil {
			t.Error("approving a gate that already reached a terminal phase should fail")
		}
	})

	t.Run("no gate", func(t *testing.T) {
		root, _, _ := newTestCLI(t, sampleAudit("plain-1", "api-guard", time.Now(), "Succeeded"))
		if err := execCLI(root, "action", "approve", "plain-1"); err == nil {
			t.Error("approving a record with no gate should fail")
		}
	})
}
