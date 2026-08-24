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
	"strings"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/Vedooo/reactive-policy/api/v1alpha1"
	"github.com/Vedooo/reactive-policy/internal/action"
	"github.com/Vedooo/reactive-policy/internal/audit/sink"
	"github.com/Vedooo/reactive-policy/internal/metrics"
)

const (
	// ConditionReverted is set on an ActionAudit once a revert request has been
	// processed.
	ConditionReverted = "Reverted"
	// ConditionApproved reports how an approval gate was resolved: True once the
	// held actions were released, False when the gate was refused or expired.
	ConditionApproved = "Approved"

	defaultRetention = 30 * 24 * time.Hour
	day              = 24 * time.Hour
	year             = 365 * day
)

// ActionAuditReconciler processes revert requests against ActionAudit records
// and reclaims records older than their policy's audit retention (ADR-003). The
// CLI never reverses actions itself; it sets spec.revertRequested and the
// operator replays each plugin's Reverse here.
type ActionAuditReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Executor  *action.Executor
	AuditSink sink.Sink
}

// +kubebuilder:rbac:groups=reactive-policy.io,resources=actionaudits,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=reactive-policy.io,resources=actionaudits/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=reactive-policy.io,resources=reactivepolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups=reactive-policy.io,resources=reactivepolicies/status,verbs=get;update;patch

func (r *ActionAuditReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var audit v1alpha1.ActionAudit
	if err := r.Get(ctx, req.NamespacedName, &audit); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("fetching audit record: %w", err)
	}

	// Retention sweep: reclaim records past their policy's retention window.
	retention := r.retentionFor(ctx, &audit)
	expiry := audit.CreationTimestamp.Add(retention)
	if remaining := time.Until(expiry); remaining <= 0 {
		if err := r.Delete(ctx, &audit); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("deleting expired audit record: %w", err)
		}
		return ctrl.Result{}, nil
	}

	// An open gate is resolved before anything else. A revert request against a
	// record whose actions have not run yet has nothing to reverse, and the
	// pipeline it is holding is the more urgent business (ADR-011).
	if audit.Status.ApprovalPhase == v1alpha1.PhasePending {
		return r.resolveGate(ctx, &audit, expiry, logger)
	}

	if audit.Spec.RevertRequested && !audit.Status.Reverted {
		return r.revert(ctx, &audit, expiry, logger)
	}

	return ctrl.Result{RequeueAfter: time.Until(expiry)}, nil
}

// resolveGate advances a record holding an open approval gate. It either
// releases the held actions, records the refusal, or — when nobody answered
// before approvalTimeout — denies by expiry. Waiting is the only branch that
// does nothing: it requeues for the moment the gate lapses so an unanswered
// gate closes itself even if no other event arrives.
func (r *ActionAuditReconciler) resolveGate(ctx context.Context, audit *v1alpha1.ActionAudit, expiry time.Time, logger logr.Logger) (ctrl.Result, error) {
	gate := audit.Spec.Gate
	if gate == nil {
		// A pending phase with no gate is not a state the operator writes. Clear
		// it rather than holding a pipeline that no longer describes itself.
		logger.Info("audit record is pending with no gate; clearing phase", "audit", audit.Name)
		return r.closeGate(ctx, audit, v1alpha1.PhaseDenied, "GateMissing", "record was pending with no gate description", expiry, logger)
	}

	switch {
	case gate.Decision == v1alpha1.DecisionApproved:
		return r.releaseGate(ctx, audit, expiry, logger)
	case gate.Decision == v1alpha1.DecisionDenied:
		return r.closeGate(ctx, audit, v1alpha1.PhaseDenied, "Denied",
			fmt.Sprintf("denied by %s", decidedByOrUnknown(gate)), expiry, logger)
	case !time.Now().Before(gate.ExpiresAt.Time):
		return r.closeGate(ctx, audit, v1alpha1.PhaseExpired, "Expired",
			fmt.Sprintf("no decision within the approval timeout; %d action(s) were not run", len(gate.PendingPlugins)),
			expiry, logger)
	default:
		return ctrl.Result{RequeueAfter: time.Until(gate.ExpiresAt.Time)}, nil
	}
}

// releaseGate runs the actions the gate held and folds their outcomes into the
// same record, so one ActionAudit describes the whole trigger — what ran before
// the gate, who approved, and what ran after.
func (r *ActionAuditReconciler) releaseGate(ctx context.Context, audit *v1alpha1.ActionAudit, expiry time.Time, logger logr.Logger) (ctrl.Result, error) {
	gate := audit.Spec.Gate

	var policy v1alpha1.ReactivePolicy
	nn := types.NamespacedName{Namespace: audit.Namespace, Name: audit.Spec.PolicyRef}
	if err := r.Get(ctx, nn, &policy); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("fetching policy for approved gate: %w", err)
		}
		return r.closeGate(ctx, audit, v1alpha1.PhaseDenied, "PolicyGone",
			"the policy was deleted while the gate was open; held actions were not run", expiry, logger)
	}
	// A policy deleted and recreated under the same name is a different policy.
	// Its pipeline may say something else entirely, and the approver signed off
	// on the old one.
	if audit.Spec.PolicyUID != "" && string(policy.UID) != audit.Spec.PolicyUID {
		return r.closeGate(ctx, audit, v1alpha1.PhaseDenied, "PolicyReplaced",
			"the policy was replaced while the gate was open; held actions were not run", expiry, logger)
	}

	ctx = action.WithClient(ctx, r.Client)

	// The gate recorded the targets resolved at trigger time. Re-running the
	// selector here could widen the blast radius past what the approver saw, so
	// the resumed pipeline is confined to the recorded set.
	targets := make([]action.Target, 0, len(gate.Targets))
	for i := range gate.Targets {
		targets = append(targets, action.Target{
			APIVersion: gate.Targets[i].APIVersion,
			Kind:       gate.Targets[i].Kind,
			Name:       gate.Targets[i].Name,
			Namespace:  gate.Targets[i].Namespace,
		})
	}
	if len(targets) == 0 {
		targets = []action.Target{{Namespace: audit.Namespace}}
	}

	from := int(gate.ActionIndex)
	to := len(policy.Spec.Actions)
	results := make([]action.Result, 0, len(targets)*(to-from))
	var runErr error
	for i := range targets {
		res, err := r.Executor.RunRange(ctx, &policy, targets[i], audit.Spec.MetricValue, from, to)
		results = append(results, res...)
		if err != nil {
			runErr = err
		}
	}

	offset := len(audit.Spec.Actions)
	for i := range results {
		// #nosec G115 -- bounded by pipeline length times target count.
		audit.Spec.Actions = append(audit.Spec.Actions, toRecord(r.Executor, int32(offset+i), results[i]))
	}
	if err := r.Update(ctx, audit); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("recording released actions: %w", err)
	}

	now := metav1.Now()
	audit.Status.ApprovalPhase = v1alpha1.PhaseApproved
	audit.Status.ResumedAt = &now
	meta.SetStatusCondition(&audit.Status.Conditions, metav1.Condition{
		Type:    ConditionApproved,
		Status:  metav1.ConditionTrue,
		Reason:  "Released",
		Message: fmt.Sprintf("approved by %s; ran %d held action(s)", decidedByOrUnknown(gate), len(results)),
	})
	if err := r.Status().Update(ctx, audit); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("updating gate status: %w", err)
	}

	metrics.RecordGateResolved(audit.Namespace, "approved", gateWaited(audit, now.Time))
	if s := r.AuditSink; s != nil && len(results) > 0 {
		if err := s.RecordTrigger(ctx, buildSinkEvents(audit)); err != nil {
			logger.Error(err, "audit sink: recording released actions", "audit", audit.Name)
		}
	}
	r.startCooldown(ctx, &policy, now, logger)

	if runErr != nil {
		logger.Error(runErr, "released pipeline reported a failure", "audit", audit.Name, "actions", len(results))
	} else {
		logger.Info("approval released held actions", "audit", audit.Name,
			"approvedBy", decidedByOrUnknown(gate), "actions", len(results))
	}
	return ctrl.Result{RequeueAfter: time.Until(expiry)}, nil
}

// closeGate ends a gate without running what it held, recording the held
// actions as Skipped so the trail says explicitly that they did not run.
func (r *ActionAuditReconciler) closeGate(ctx context.Context, audit *v1alpha1.ActionAudit, phase v1alpha1.ApprovalPhase, reason, message string, expiry time.Time, logger logr.Logger) (ctrl.Result, error) {
	if gate := audit.Spec.Gate; gate != nil && len(gate.PendingPlugins) > 0 {
		offset := len(audit.Spec.Actions)
		for i, plugin := range gate.PendingPlugins {
			audit.Spec.Actions = append(audit.Spec.Actions, v1alpha1.ActionRecord{
				// #nosec G115 -- bounded by pipeline length.
				Index:   int32(offset + i),
				Plugin:  plugin,
				Status:  string(action.StatusSkipped),
				Message: message,
			})
		}
		if err := r.Update(ctx, audit); err != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, fmt.Errorf("recording skipped actions: %w", err)
		}
	}

	now := metav1.Now()
	audit.Status.ApprovalPhase = phase
	meta.SetStatusCondition(&audit.Status.Conditions, metav1.Condition{
		Type:    ConditionApproved,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
	if err := r.Status().Update(ctx, audit); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("updating gate status: %w", err)
	}

	outcome := "denied"
	if phase == v1alpha1.PhaseExpired {
		outcome = "expired"
	}
	metrics.RecordGateResolved(audit.Namespace, outcome, gateWaited(audit, now.Time))
	logger.Info("approval gate closed without running held actions",
		"audit", audit.Name, "phase", phase, "reason", reason)

	// A refused gate still starts the cooldown. The pipeline did not run, but
	// re-asking the same person the same question on the next poll is exactly
	// the approval fatigue the gate exists to avoid.
	var policy v1alpha1.ReactivePolicy
	nn := types.NamespacedName{Namespace: audit.Namespace, Name: audit.Spec.PolicyRef}
	if err := r.Get(ctx, nn, &policy); err == nil {
		r.startCooldown(ctx, &policy, now, logger)
	}
	return ctrl.Result{RequeueAfter: time.Until(expiry)}, nil
}

// startCooldown drops the policy's gate reference and starts its cooldown from
// the decision. Time spent waiting for a human does not count against the quiet
// period that is meant to follow the action.
func (r *ActionAuditReconciler) startCooldown(ctx context.Context, policy *v1alpha1.ReactivePolicy, at metav1.Time, logger logr.Logger) {
	policy.Status.LastTriggeredAt = &at
	policy.Status.PendingGateRef = ""
	policy.Status.State = v1alpha1.StateCooldown
	if err := r.Status().Update(ctx, policy); err != nil {
		// The policy reconciler re-derives both on its next pass, so a lost race
		// here costs a poll interval rather than correctness.
		logger.Error(err, "starting cooldown after approval decision", "policy", policy.Name)
	}
}

// gateWaited returns how long a gate was open, in seconds.
func gateWaited(audit *v1alpha1.ActionAudit, until time.Time) float64 {
	return until.Sub(audit.Spec.TriggeredAt.Time).Seconds()
}

// decidedByOrUnknown names the approver, falling back when a gate closed
// without one — an expiry has no decider.
func decidedByOrUnknown(gate *v1alpha1.ApprovalGate) string {
	if gate != nil && gate.DecidedBy != "" {
		return gate.DecidedBy
	}
	return "unknown"
}

// revert replays each reversible action's Reverse in reverse order and records
// the outcome. It is one-shot: status.reverted guards against acting twice.
func (r *ActionAuditReconciler) revert(ctx context.Context, audit *v1alpha1.ActionAudit, expiry time.Time, logger logr.Logger) (ctrl.Result, error) {
	ctx = action.WithClient(ctx, r.Client)

	results := make([]v1alpha1.RevertResult, 0, len(audit.Spec.Actions))
	for i := len(audit.Spec.Actions) - 1; i >= 0; i-- {
		rec := audit.Spec.Actions[i]
		results = append(results, r.reverseOne(ctx, rec))
	}

	now := metav1.Now()
	audit.Status.Reverted = true
	audit.Status.RevertedAt = &now
	audit.Status.RevertResults = results
	meta.SetStatusCondition(&audit.Status.Conditions, metav1.Condition{
		Type:    ConditionReverted,
		Status:  metav1.ConditionTrue,
		Reason:  "RevertProcessed",
		Message: fmt.Sprintf("processed revert of %d actions", len(results)),
	})

	if err := r.Status().Update(ctx, audit); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("updating audit status: %w", err)
	}
	if s := r.AuditSink; s != nil {
		if err := s.RecordRevert(ctx, buildRevertEvents(audit, results)); err != nil {
			logger.Error(err, "audit sink: recording revert", "audit", audit.Name)
		}
	}
	logger.Info("processed revert request", "audit", audit.Name, "actions", len(results))
	return ctrl.Result{RequeueAfter: time.Until(expiry)}, nil
}

// reverseOne reverses a single recorded action, skipping ones that did not
// succeed or whose plugin is not reversible.
func (r *ActionAuditReconciler) reverseOne(ctx context.Context, rec v1alpha1.ActionRecord) v1alpha1.RevertResult {
	out := v1alpha1.RevertResult{Index: rec.Index, Plugin: rec.Plugin}

	if rec.Status != string(action.StatusSucceeded) {
		out.Status = string(action.StatusSkipped)
		out.Message = "action did not succeed; nothing to reverse"
		return out
	}
	if !rec.Reversible {
		out.Status = string(action.StatusSkipped)
		out.Message = "plugin is not reversible"
		return out
	}
	plugin := r.Executor.Lookup(rec.Plugin)
	if plugin == nil {
		out.Status = string(action.StatusFailed)
		out.Message = "plugin not registered"
		return out
	}
	if err := plugin.Reverse(ctx, recordToResult(rec)); err != nil {
		out.Status = string(action.StatusFailed)
		out.Message = err.Error()
		return out
	}
	out.Status = string(action.StatusSucceeded)
	out.Message = "reversed"
	return out
}

// retentionFor resolves the retention window for an audit record from its
// owning policy, defaulting to 30d when the policy or its setting is absent.
func (r *ActionAuditReconciler) retentionFor(ctx context.Context, audit *v1alpha1.ActionAudit) time.Duration {
	var policy v1alpha1.ReactivePolicy
	err := r.Get(ctx, types.NamespacedName{Namespace: audit.Namespace, Name: audit.Spec.PolicyRef}, &policy)
	if err != nil || policy.Spec.Audit == nil || policy.Spec.Audit.Retention == "" {
		return defaultRetention
	}
	d, perr := parseRetention(policy.Spec.Audit.Retention)
	if perr != nil {
		return defaultRetention
	}
	return d
}

// parseRetention accepts Go durations plus day ("30d") and year ("1y") units,
// which time.ParseDuration does not understand.
func parseRetention(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasSuffix(s, "d"):
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid day retention %q: %w", s, err)
		}
		return time.Duration(n) * day, nil
	case strings.HasSuffix(s, "y"):
		n, err := strconv.Atoi(strings.TrimSuffix(s, "y"))
		if err != nil {
			return 0, fmt.Errorf("invalid year retention %q: %w", s, err)
		}
		return time.Duration(n) * year, nil
	default:
		return time.ParseDuration(s)
	}
}

func buildRevertEvents(audit *v1alpha1.ActionAudit, results []v1alpha1.RevertResult) []sink.RevertEvent {
	revertedAt := time.Now()
	if audit.Status.RevertedAt != nil {
		revertedAt = audit.Status.RevertedAt.Time
	}
	events := make([]sink.RevertEvent, 0, len(results))
	for i := range results {
		rr := results[i]
		events = append(events, sink.RevertEvent{
			AuditUID:       string(audit.UID),
			AuditName:      audit.Name,
			AuditNamespace: audit.Namespace,
			PolicyRef:      audit.Spec.PolicyRef,
			ActionIndex:    rr.Index,
			Plugin:         rr.Plugin,
			RevertedAt:     revertedAt,
			Status:         rr.Status,
			Message:        rr.Message,
		})
	}
	return events
}

func (r *ActionAuditReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Executor == nil {
		r.Executor = action.NewExecutor(action.Default())
	}
	if r.AuditSink == nil {
		r.AuditSink = sink.Noop{}
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ActionAudit{}).
		Named("actionaudit").
		Complete(r)
}
