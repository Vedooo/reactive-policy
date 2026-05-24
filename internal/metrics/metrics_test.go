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

package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

func TestRecordEvaluation(t *testing.T) {
	EvaluationsTotal.Reset()
	RecordEvaluation("p", "ns", ResultCrossed)
	RecordEvaluation("p", "ns", ResultCrossed)
	RecordEvaluation("p", "ns", ResultWithin)

	if got := testutil.ToFloat64(EvaluationsTotal.WithLabelValues("p", "ns", ResultCrossed)); got != 2 {
		t.Errorf("crossed count = %v, want 2", got)
	}
	if got := testutil.ToFloat64(EvaluationsTotal.WithLabelValues("p", "ns", ResultWithin)); got != 1 {
		t.Errorf("within count = %v, want 1", got)
	}
}

func TestRecordActionDefaultsTargetKind(t *testing.T) {
	ActionsTotal.Reset()
	RecordAction("notify.slack", "", "Succeeded")
	RecordAction("k8s.annotate", "Deployment", "Failed")

	if got := testutil.ToFloat64(ActionsTotal.WithLabelValues("notify.slack", "none", "Succeeded")); got != 1 {
		t.Errorf("empty target kind should record as none, got %v", got)
	}
	if got := testutil.ToFloat64(ActionsTotal.WithLabelValues("k8s.annotate", "Deployment", "Failed")); got != 1 {
		t.Errorf("kinded action count = %v, want 1", got)
	}
}

func TestActivePoliciesTracker(t *testing.T) {
	ActivePolicies.Reset()
	// Use a unique namespace so the package-level tracked set does not collide
	// with other tests sharing this process.
	const ns = "tracker-ns"

	TrackPolicy(ns, "p1")
	TrackPolicy(ns, "p2")
	TrackPolicy(ns, "p1") // duplicate must not double-count
	if got := testutil.ToFloat64(ActivePolicies.WithLabelValues(ns)); got != 2 {
		t.Errorf("active policies = %v, want 2", got)
	}

	UntrackPolicy(ns, "p1")
	if got := testutil.ToFloat64(ActivePolicies.WithLabelValues(ns)); got != 1 {
		t.Errorf("after untrack = %v, want 1", got)
	}
	UntrackPolicy(ns, "missing") // no-op, must not panic or go negative
	if got := testutil.ToFloat64(ActivePolicies.WithLabelValues(ns)); got != 1 {
		t.Errorf("untracking a missing policy changed the gauge: %v", got)
	}
}

func TestAllDocumentedMetricsAreRegistered(t *testing.T) {
	// Touch every metric so its family is present in the gathered output.
	RecordEvaluation("p", "ns", ResultWithin)
	RecordAction("notify.slack", "Deployment", "Succeeded")
	ObserveActionDuration("notify.slack", 0.01)
	TrackPolicy("reg-ns", "p")
	RecordTrigger("ns")
	RecordRateLimited("ns")
	RecordQueryError()
	RecordValidationError("notify.slack")

	mfs, err := crmetrics.Registry.Gather()
	if err != nil {
		t.Fatalf("gathering metrics: %v", err)
	}
	present := map[string]bool{}
	for _, mf := range mfs {
		present[mf.GetName()] = true
	}

	for _, want := range []string{
		"reactive_policy_evaluations_total",
		"reactive_policy_actions_total",
		"reactive_policy_action_duration_seconds",
		"reactive_policy_active_policies",
		"reactive_policy_triggered_policies_total",
		"reactive_policy_rate_limited_total",
		"reactive_policy_prometheus_query_errors_total",
		"reactive_policy_plugin_validation_errors_total",
	} {
		if !present[want] {
			t.Errorf("metric %q is not registered/exposed", want)
		}
	}
}
