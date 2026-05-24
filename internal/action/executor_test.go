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

package action_test

import (
	"context"
	"errors"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Vedooo/reactive-policy/api/v1alpha1"
	"github.com/Vedooo/reactive-policy/internal/action"
	acttest "github.com/Vedooo/reactive-policy/internal/action/testing"
)

func policyWith(actions ...v1alpha1.Action) *v1alpha1.ReactivePolicy {
	return &v1alpha1.ReactivePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec:       v1alpha1.ReactivePolicySpec{Actions: actions},
	}
}

func TestExecutorRunAllSucceed(t *testing.T) {
	a := acttest.NewNop("a")
	b := acttest.NewNop("b")
	exec := action.NewExecutor(acttest.NewFakeRegistry(a, b))

	results, err := exec.Run(context.Background(), policyWith(
		v1alpha1.Action{Plugin: "a"},
		v1alpha1.Action{Plugin: "b"},
	), action.Target{Namespace: "default"}, "0.9")

	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	for _, r := range results {
		if r.Status != action.StatusSucceeded {
			t.Errorf("result %s: status = %q, want Succeeded", r.PluginName, r.Status)
		}
	}
	if a.ExecuteCount() != 1 || b.ExecuteCount() != 1 {
		t.Errorf("execute counts = (%d, %d), want (1, 1)", a.ExecuteCount(), b.ExecuteCount())
	}
}

func TestExecutorOnFailureContinue(t *testing.T) {
	a := acttest.NewNop("a")
	a.ExecuteErr = errors.New("boom")
	b := acttest.NewNop("b")
	exec := action.NewExecutor(acttest.NewFakeRegistry(a, b))

	results, err := exec.Run(context.Background(), policyWith(
		v1alpha1.Action{Plugin: "a", OnFailure: v1alpha1.FailureContinue},
		v1alpha1.Action{Plugin: "b"},
	), action.Target{Namespace: "default"}, "0.9")

	if err != nil {
		t.Fatalf("continue should not abort the pipeline, got error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Status != action.StatusFailed {
		t.Errorf("result[0] status = %q, want Failed", results[0].Status)
	}
	if b.ExecuteCount() != 1 {
		t.Errorf("second action should still run; execute count = %d", b.ExecuteCount())
	}
}

func TestExecutorOnFailureStop(t *testing.T) {
	a := acttest.NewNop("a")
	a.ExecuteErr = errors.New("boom")
	b := acttest.NewNop("b")
	exec := action.NewExecutor(acttest.NewFakeRegistry(a, b))

	results, err := exec.Run(context.Background(), policyWith(
		v1alpha1.Action{Plugin: "a"}, // default onFailure is stop
		v1alpha1.Action{Plugin: "b"},
	), action.Target{Namespace: "default"}, "0.9")

	if err == nil {
		t.Fatal("stop should abort the pipeline and return an error")
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1 (pipeline stopped)", len(results))
	}
	if b.ExecuteCount() != 0 {
		t.Errorf("second action should not run after stop; execute count = %d", b.ExecuteCount())
	}
}

func TestExecutorOnFailureRollback(t *testing.T) {
	a := acttest.NewNop("a") // succeeds, must be reversed
	b := acttest.NewNop("b")
	b.ExecuteErr = errors.New("boom")
	exec := action.NewExecutor(acttest.NewFakeRegistry(a, b))

	results, err := exec.Run(context.Background(), policyWith(
		v1alpha1.Action{Plugin: "a"},
		v1alpha1.Action{Plugin: "b", OnFailure: v1alpha1.FailureRollback},
	), action.Target{Namespace: "default"}, "0.9")

	if err == nil {
		t.Fatal("rollback should abort the pipeline and return an error")
	}
	if a.ReverseCount() != 1 {
		t.Errorf("first action should be reversed once; reverse count = %d", a.ReverseCount())
	}
	if results[0].Details["reversed"] != true {
		t.Errorf("first result should be marked reversed; details = %v", results[0].Details)
	}
}

func TestExecutorUnknownPluginFails(t *testing.T) {
	exec := action.NewExecutor(action.NewRegistry())

	results, err := exec.Run(context.Background(), policyWith(
		v1alpha1.Action{Plugin: "ghost"},
	), action.Target{Namespace: "default"}, "0.9")

	if err == nil {
		t.Fatal("an unknown plugin should fail the pipeline")
	}
	if len(results) != 1 || results[0].Status != action.StatusFailed {
		t.Fatalf("want one Failed result, got %+v", results)
	}
}

func TestExecutorSharesTimestampAcrossActions(t *testing.T) {
	a := acttest.NewNop("a")
	exec := action.NewExecutor(acttest.NewFakeRegistry(a))

	before := time.Now()
	results, err := exec.Run(context.Background(), policyWith(v1alpha1.Action{Plugin: "a"}),
		action.Target{Namespace: "default"}, "0.9")
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if results[0].Timestamp.Before(before) {
		t.Errorf("result timestamp %v predates the call at %v", results[0].Timestamp, before)
	}
}
