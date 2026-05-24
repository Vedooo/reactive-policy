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
)

const (
	// ConditionReverted is set on an ActionAudit once a revert request has been
	// processed.
	ConditionReverted = "Reverted"

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
	Scheme   *runtime.Scheme
	Executor *action.Executor
}

// +kubebuilder:rbac:groups=reactive-policy.io,resources=actionaudits,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=reactive-policy.io,resources=actionaudits/status,verbs=get;update;patch

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

	if audit.Spec.RevertRequested && !audit.Status.Reverted {
		return r.revert(ctx, &audit, expiry, logger)
	}

	return ctrl.Result{RequeueAfter: time.Until(expiry)}, nil
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

func (r *ActionAuditReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Executor == nil {
		r.Executor = action.NewExecutor(action.Default())
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ActionAudit{}).
		Named("actionaudit").
		Complete(r)
}
