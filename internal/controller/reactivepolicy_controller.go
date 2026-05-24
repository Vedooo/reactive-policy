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

package controller

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/Vedooo/reactive-policy/api/v1alpha1"
	"github.com/Vedooo/reactive-policy/internal/action"
	"github.com/Vedooo/reactive-policy/internal/metrics"
	"github.com/Vedooo/reactive-policy/internal/prometheus"
)

const (
	ConditionReady                 = "Ready"
	ConditionMetricSourceReachable = "MetricSourceReachable"
	ConditionThresholdCrossed      = "ThresholdCrossed"
	ConditionRateLimited           = "RateLimited"

	defaultPollInterval       = 30 * time.Second
	defaultCooldown           = 5 * time.Minute
	defaultMaxTriggersPerHour = 5
	defaultMaxResources       = 10
)

type ReactivePolicyReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Prometheus prometheus.Factory
	Window     *prometheus.SlidingWindow
	Executor   *action.Executor
	Limiter    *auditLimiter
}

// +kubebuilder:rbac:groups=reactive-policy.io,resources=reactivepolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=reactive-policy.io,resources=reactivepolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=reactive-policy.io,resources=reactivepolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups=reactive-policy.io,resources=actionaudits,verbs=get;list;watch;create

func (r *ReactivePolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	key := req.NamespacedName.String()

	var policy v1alpha1.ReactivePolicy
	if err := r.Get(ctx, req.NamespacedName, &policy); err != nil {
		if apierrors.IsNotFound(err) {
			r.Window.Forget(key)
			r.Limiter.Forget(req.Namespace, req.Name)
			metrics.UntrackPolicy(req.Namespace, req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("fetching policy: %w", err)
	}
	metrics.TrackPolicy(policy.Namespace, policy.Name)

	pollInterval := policy.Spec.Observe.PollInterval.Duration
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}
	now := time.Now()

	// Gate 1: cooldown. After a trigger the policy stays quiet for at least its
	// cooldown (ADR-007). We exit before querying the metric source, per
	// docs/ARCHITECTURE.md §2.
	if policy.Status.LastTriggeredAt != nil {
		cooldown := cooldownOrDefault(&policy)
		if elapsed := now.Sub(policy.Status.LastTriggeredAt.Time); elapsed < cooldown {
			policy.Status.State = v1alpha1.StateCooldown
			r.setCondition(&policy, ConditionReady, metav1.ConditionTrue, "CoolingDown", "policy is cooling down after a recent trigger")
			return r.requeue(ctx, &policy, cooldown-elapsed)
		}
	}

	// Gate 2: rate limit. A rolling one-hour cap prevents a flapping metric from
	// triggering a runaway pipeline (ADR-007). The count comes from persisted
	// ActionAudit records, so it survives operator restarts (ADR-009).
	allowed, limitErr := r.Limiter.Allowed(ctx, policy.Namespace, policy.Name, maxTriggersOrDefault(&policy), now, pollInterval)
	if limitErr != nil {
		logger.Error(limitErr, "evaluating rate limit", "policy", key)
		return r.requeue(ctx, &policy, pollInterval)
	}
	if !allowed {
		policy.Status.State = v1alpha1.StateRateLimited
		metrics.RecordRateLimited(policy.Namespace)
		r.setCondition(&policy, ConditionRateLimited, metav1.ConditionTrue, "RateLimited", "policy hit its maxTriggersPerHour cap")
		r.setCondition(&policy, ConditionReady, metav1.ConditionTrue, "RateLimited", "policy is valid but rate limited")
		return r.requeue(ctx, &policy, pollInterval)
	}

	policy.Status.State = v1alpha1.StateWatching
	policy.Status.LastEvaluatedAt = &metav1.Time{Time: now}
	r.setCondition(&policy, ConditionRateLimited, metav1.ConditionFalse, "WithinLimit", "policy is within its trigger rate limit")

	promClient, err := r.Prometheus(policy.Spec.Observe.Endpoint)
	if err != nil {
		logger.Error(err, "building metric client")
		metrics.RecordEvaluation(policy.Name, policy.Namespace, metrics.ResultError)
		r.setCondition(&policy, ConditionMetricSourceReachable, metav1.ConditionFalse, "ClientError", err.Error())
		r.setCondition(&policy, ConditionReady, metav1.ConditionTrue, "Evaluating", "policy is being evaluated")
		return r.requeue(ctx, &policy, pollInterval)
	}

	value, queryErr := promClient.Query(ctx, policy.Spec.Observe.Query)
	if queryErr != nil {
		logger.Error(queryErr, "metric query failed", "endpoint", policy.Spec.Observe.Endpoint)
		metrics.RecordQueryError()
		metrics.RecordEvaluation(policy.Name, policy.Namespace, metrics.ResultError)
		r.Window.Forget(key)
		r.setCondition(&policy, ConditionMetricSourceReachable, metav1.ConditionFalse, "QueryFailed", queryErr.Error())
		r.setCondition(&policy, ConditionReady, metav1.ConditionTrue, "Evaluating", "policy is being evaluated")
		return r.requeue(ctx, &policy, pollInterval)
	}
	r.setCondition(&policy, ConditionMetricSourceReachable, metav1.ConditionTrue, "QuerySucceeded", "metric query succeeded")
	policy.Status.CurrentMetricValue = strconv.FormatFloat(value, 'g', -1, 64)

	threshold, err := prometheus.ParseThreshold(policy.Spec.Observe.Threshold)
	if err != nil {
		logger.Error(err, "invalid threshold")
		metrics.RecordEvaluation(policy.Name, policy.Namespace, metrics.ResultError)
		policy.Status.State = v1alpha1.StateInvalid
		r.setCondition(&policy, ConditionReady, metav1.ConditionFalse, "InvalidThreshold", err.Error())
		return r.requeue(ctx, &policy, pollInterval)
	}
	r.setCondition(&policy, ConditionReady, metav1.ConditionTrue, "Evaluating", "policy is being evaluated")

	crossed := prometheus.Compare(value, threshold, policy.Spec.Observe.Operator)
	if crossed {
		metrics.RecordEvaluation(policy.Name, policy.Namespace, metrics.ResultCrossed)
	} else {
		metrics.RecordEvaluation(policy.Name, policy.Namespace, metrics.ResultWithin)
	}
	crossedFor := r.Window.Observe(key, crossed, now)
	sustained := crossed && crossedFor >= policy.Spec.Observe.Duration.Duration

	if crossed {
		r.setCondition(&policy, ConditionThresholdCrossed, metav1.ConditionTrue, "ThresholdCrossed",
			fmt.Sprintf("value %s %s threshold %s", policy.Status.CurrentMetricValue,
				policy.Spec.Observe.Operator, policy.Spec.Observe.Threshold))
	} else {
		r.setCondition(&policy, ConditionThresholdCrossed, metav1.ConditionFalse, "WithinThreshold",
			"metric value is within the configured threshold")
	}

	if sustained {
		return r.trigger(ctx, &policy, key, now, pollInterval, logger)
	}
	return r.requeue(ctx, &policy, pollInterval)
}

// trigger runs the action pipeline, writes an ActionAudit record, and records
// the trigger against the cooldown and rate-limit state. The trigger counts
// even if some actions fail: retrying a partially-applied, possibly-destructive
// pipeline is more dangerous than completing it once (see docs/ARCHITECTURE.md
// §7).
func (r *ReactivePolicyReconciler) trigger(ctx context.Context, policy *v1alpha1.ReactivePolicy, key string, now time.Time, pollInterval time.Duration, logger logr.Logger) (ctrl.Result, error) {
	// Plugins read and mutate cluster resources through the client carried in
	// the context (see internal/action/clientctx.go).
	ctx = action.WithClient(ctx, r.Client)

	// Resolve spec.target into the concrete cluster resources the pipeline acts
	// on. A resolution failure (bad selector, missing kind, or more matches than
	// maxResources) is a refusal to act, not a trigger: surface it and wait for
	// the next poll rather than acting on an unsafe target set.
	targets, resolveErr := r.resolveTargets(ctx, policy)
	if resolveErr != nil {
		logger.Error(resolveErr, "resolving targets", "policy", key)
		r.setCondition(policy, ConditionReady, metav1.ConditionFalse, "TargetResolutionFailed", resolveErr.Error())
		return r.requeue(ctx, policy, pollInterval)
	}
	// A policy may legitimately match no resources (e.g. a notify-only pipeline);
	// run the pipeline once against a namespace-scoped target so such actions
	// still fire.
	if len(targets) == 0 {
		targets = []action.Target{{Namespace: policy.Namespace}}
	}

	// Run the full pipeline against each matched resource, collecting every
	// result into a single per-trigger audit. One audit per trigger keeps the
	// rate-limit count (ADR-009) and the audit history aligned with triggers
	// rather than with individual actions or targets.
	results := make([]action.Result, 0, len(targets)*len(policy.Spec.Actions))
	var runErr error
	for i := range targets {
		res, err := r.Executor.Run(ctx, policy, targets[i], policy.Status.CurrentMetricValue)
		results = append(results, res...)
		if err != nil {
			runErr = err
		}
	}

	triggeredAt := metav1.Time{Time: now}
	policy.Status.LastTriggeredAt = &triggeredAt
	policy.Status.TriggerCount++
	policy.Status.State = v1alpha1.StateTriggering
	metrics.RecordTrigger(policy.Namespace)

	// Persist the audit trail before returning (docs/ARCHITECTURE.md §6). A write
	// failure is logged but does not fail the reconcile: re-running a
	// possibly-destructive pipeline to retry the audit write is worse than a
	// missing record.
	if err := r.writeAudit(ctx, policy, triggeredAt, results); err != nil {
		logger.Error(err, "writing audit record", "policy", key)
	} else {
		r.Limiter.Observe(policy.Namespace, policy.Name, now)
	}
	r.Window.Forget(key)

	if runErr != nil {
		logger.Error(runErr, "action pipeline reported a failure", "policy", key, "targets", len(targets), "actions", len(results))
	} else {
		logger.Info("policy triggered", "policy", key, "targets", len(targets), "actions", len(results), "value", policy.Status.CurrentMetricValue)
	}
	return r.requeue(ctx, policy, pollInterval)
}

// resolveTargets lists the cluster resources selected by policy.Spec.Target
// across every configured kind and returns them as action targets. It enforces
// the MaxResources safety cap: matching more resources than allowed is an error
// so a broad selector can never fan the pipeline out beyond the policy's intent.
// Unstructured reads bypass the manager cache (controller-runtime does not cache
// unstructured objects by default), so this needs only list permission — already
// covered by the broad RBAC the resource-operating plugins require.
func (r *ReactivePolicyReconciler) resolveTargets(ctx context.Context, policy *v1alpha1.ReactivePolicy) ([]action.Target, error) {
	selector, err := metav1.LabelSelectorAsSelector(&policy.Spec.Target.Selector)
	if err != nil {
		return nil, fmt.Errorf("invalid target selector: %w", err)
	}

	maxResources := defaultMaxResources
	if policy.Spec.Target.MaxResources != nil {
		maxResources = int(*policy.Spec.Target.MaxResources)
	}

	var targets []action.Target
	for _, kind := range policy.Spec.Target.Kinds {
		list := &unstructured.UnstructuredList{}
		list.SetAPIVersion(kind.APIVersion)
		list.SetKind(kind.Kind + "List")
		if err := r.List(ctx, list,
			client.InNamespace(policy.Namespace),
			client.MatchingLabelsSelector{Selector: selector},
		); err != nil {
			return nil, fmt.Errorf("listing %s %s: %w", kind.APIVersion, kind.Kind, err)
		}
		for i := range list.Items {
			item := &list.Items[i]
			targets = append(targets, action.Target{
				APIVersion: kind.APIVersion,
				Kind:       kind.Kind,
				Namespace:  item.GetNamespace(),
				Name:       item.GetName(),
			})
		}
	}

	if len(targets) > maxResources {
		return nil, fmt.Errorf("target selector matched %d resources, exceeding maxResources %d", len(targets), maxResources)
	}
	return targets, nil
}

func (r *ReactivePolicyReconciler) requeue(ctx context.Context, policy *v1alpha1.ReactivePolicy, after time.Duration) (ctrl.Result, error) {
	if err := r.Status().Update(ctx, policy); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("updating status: %w", err)
	}
	return ctrl.Result{RequeueAfter: after}, nil
}

func (r *ReactivePolicyReconciler) setCondition(policy *v1alpha1.ReactivePolicy, condType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&policy.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: policy.Generation,
	})
}

func cooldownOrDefault(policy *v1alpha1.ReactivePolicy) time.Duration {
	if policy.Spec.Cooldown.Duration > 0 {
		return policy.Spec.Cooldown.Duration
	}
	return defaultCooldown
}

func maxTriggersOrDefault(policy *v1alpha1.ReactivePolicy) int {
	if policy.Spec.MaxTriggersPerHour != nil && *policy.Spec.MaxTriggersPerHour > 0 {
		return int(*policy.Spec.MaxTriggersPerHour)
	}
	return defaultMaxTriggersPerHour
}

func (r *ReactivePolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Prometheus == nil {
		r.Prometheus = prometheus.NewClient
	}
	if r.Window == nil {
		r.Window = prometheus.NewSlidingWindow()
	}
	if r.Executor == nil {
		r.Executor = action.NewExecutor(action.Default())
	}
	if r.Limiter == nil {
		r.Limiter = newAuditLimiter(r.Client)
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ReactivePolicy{}).
		Named("reactivepolicy").
		Complete(r)
}
