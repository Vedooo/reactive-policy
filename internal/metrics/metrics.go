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

// Package metrics defines and registers the operator's own Prometheus metrics
// (docs/ARCHITECTURE.md §8). Other packages record through the helper functions
// here rather than touching the collectors directly, keeping call sites terse
// and the label conventions in one place.
package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Evaluation result label values for EvaluationsTotal.
const (
	ResultCrossed = "crossed"
	ResultWithin  = "within"
	ResultError   = "error"
)

// targetKindNone labels actions whose target has no specific kind (e.g. the
// policy-scoped target the executor uses before resource resolution lands).
const targetKindNone = "none"

var (
	// EvaluationsTotal counts metric evaluations partitioned by outcome.
	EvaluationsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "reactive_policy_evaluations_total",
		Help: "Total metric evaluations by policy, namespace, and result (crossed, within, error).",
	}, []string{"policy", "namespace", "result"})

	// ActionsTotal counts action executions partitioned by plugin, target kind,
	// and terminal status.
	ActionsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "reactive_policy_actions_total",
		Help: "Total action executions by plugin, target_kind, and result (Succeeded, Failed, Skipped).",
	}, []string{"plugin", "target_kind", "result"})

	// ActionDurationSeconds observes per-action execution latency.
	ActionDurationSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "reactive_policy_action_duration_seconds",
		Help:    "Action execution latency in seconds by plugin.",
		Buckets: prometheus.DefBuckets,
	}, []string{"plugin"})

	// ActivePolicies reports how many policies the operator currently watches.
	ActivePolicies = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "reactive_policy_active_policies",
		Help: "Number of ReactivePolicy resources currently being watched, by namespace.",
	}, []string{"namespace"})

	// TriggeredPoliciesTotal counts how often policies fired their pipeline.
	TriggeredPoliciesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "reactive_policy_triggered_policies_total",
		Help: "Total policy triggers by namespace.",
	}, []string{"namespace"})

	// RateLimitedTotal counts how often a reconcile was suppressed by the
	// per-policy hourly rate limit (ADR-007/ADR-009).
	RateLimitedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "reactive_policy_rate_limited_total",
		Help: "Total reconciles suppressed by the maxTriggersPerHour rate limit, by namespace.",
	}, []string{"namespace"})

	// PrometheusQueryErrorsTotal counts failed metric-source queries.
	PrometheusQueryErrorsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "reactive_policy_prometheus_query_errors_total",
		Help: "Total failed queries against configured metric sources.",
	})

	// PluginValidationErrorsTotal counts admission-time plugin validation
	// failures by plugin.
	PluginValidationErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "reactive_policy_plugin_validation_errors_total",
		Help: "Total plugin validation failures at admission by plugin.",
	}, []string{"plugin"})

	// ApprovalGatesOpenedTotal counts pipelines that stopped for a human
	// decision, by namespace.
	ApprovalGatesOpenedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "reactive_policy_approval_gates_opened_total",
		Help: "Total action pipelines that stopped at an approval gate by namespace.",
	}, []string{"namespace"})

	// ApprovalDecisionsTotal counts how gates were resolved. The "expired"
	// outcome is the one worth alerting on: it means nobody answered in time and
	// the actions were dropped.
	ApprovalDecisionsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "reactive_policy_approval_decisions_total",
		Help: "Total approval gate resolutions by namespace and outcome (approved, denied, expired).",
	}, []string{"namespace", "outcome"})

	// ApprovalWaitSeconds measures how long gates waited before resolving. The
	// buckets span seconds to hours because the interesting tail is the gate
	// nobody noticed.
	ApprovalWaitSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "reactive_policy_approval_wait_seconds",
		Help:    "Time a pipeline spent holding for an approval decision, by outcome.",
		Buckets: []float64{10, 30, 60, 300, 900, 1800, 3600, 7200, 21600},
	}, []string{"outcome"})

	// ApprovalGatesPending is the number of gates currently waiting. A gate that
	// stays here is a pipeline nobody has looked at.
	ApprovalGatesPending = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "reactive_policy_approval_gates_pending",
		Help: "Approval gates currently waiting for a decision by namespace.",
	}, []string{"namespace"})
)

func init() {
	crmetrics.Registry.MustRegister(
		EvaluationsTotal,
		ActionsTotal,
		ActionDurationSeconds,
		ActivePolicies,
		TriggeredPoliciesTotal,
		RateLimitedTotal,
		PrometheusQueryErrorsTotal,
		PluginValidationErrorsTotal,
		ApprovalGatesOpenedTotal,
		ApprovalDecisionsTotal,
		ApprovalWaitSeconds,
		ApprovalGatesPending,
	)
}

// RecordEvaluation increments the evaluation counter for a policy outcome.
func RecordEvaluation(policy, namespace, result string) {
	EvaluationsTotal.WithLabelValues(policy, namespace, result).Inc()
}

// RecordAction increments the action counter. An empty target kind is recorded
// as "none".
func RecordAction(plugin, targetKind, result string) {
	if targetKind == "" {
		targetKind = targetKindNone
	}
	ActionsTotal.WithLabelValues(plugin, targetKind, result).Inc()
}

// ObserveActionDuration records how long an action took.
func ObserveActionDuration(plugin string, seconds float64) {
	ActionDurationSeconds.WithLabelValues(plugin).Observe(seconds)
}

// RecordTrigger increments the trigger counter for a namespace.
func RecordTrigger(namespace string) {
	TriggeredPoliciesTotal.WithLabelValues(namespace).Inc()
}

// RecordRateLimited increments the rate-limit suppression counter for a
// namespace.
func RecordRateLimited(namespace string) {
	RateLimitedTotal.WithLabelValues(namespace).Inc()
}

// RecordQueryError increments the metric-source query error counter.
func RecordQueryError() {
	PrometheusQueryErrorsTotal.Inc()
}

// RecordGateOpened counts a pipeline that stopped for approval and marks it
// pending.
func RecordGateOpened(namespace string) {
	ApprovalGatesOpenedTotal.WithLabelValues(namespace).Inc()
	ApprovalGatesPending.WithLabelValues(namespace).Inc()
}

// RecordGateResolved counts a gate resolution, records how long it waited, and
// clears it from the pending gauge. Outcome is one of "approved", "denied", or
// "expired".
func RecordGateResolved(namespace, outcome string, waited float64) {
	ApprovalDecisionsTotal.WithLabelValues(namespace, outcome).Inc()
	ApprovalWaitSeconds.WithLabelValues(outcome).Observe(waited)
	ApprovalGatesPending.WithLabelValues(namespace).Dec()
}

// RecordValidationError increments the plugin validation error counter.
func RecordValidationError(plugin string) {
	PluginValidationErrorsTotal.WithLabelValues(plugin).Inc()
}

// policyTracker maintains the set of watched policies per namespace so
// ActivePolicies reflects a live count. The set warms up as the controller
// reconciles existing objects after start.
var (
	trackerMu sync.Mutex
	tracked   = map[string]map[string]struct{}{}
)

// TrackPolicy records that a policy is being watched and updates the gauge.
func TrackPolicy(namespace, name string) {
	trackerMu.Lock()
	defer trackerMu.Unlock()
	if tracked[namespace] == nil {
		tracked[namespace] = map[string]struct{}{}
	}
	tracked[namespace][name] = struct{}{}
	ActivePolicies.WithLabelValues(namespace).Set(float64(len(tracked[namespace])))
}

// UntrackPolicy drops a deleted policy from the watched set and updates the
// gauge.
func UntrackPolicy(namespace, name string) {
	trackerMu.Lock()
	defer trackerMu.Unlock()
	if tracked[namespace] == nil {
		return
	}
	delete(tracked[namespace], name)
	ActivePolicies.WithLabelValues(namespace).Set(float64(len(tracked[namespace])))
}
