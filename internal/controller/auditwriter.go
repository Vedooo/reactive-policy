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
	"encoding/json"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/Vedooo/reactive-policy/api/v1alpha1"
	"github.com/Vedooo/reactive-policy/internal/action"
	"github.com/Vedooo/reactive-policy/internal/audit/sink"
)

// writeAudit persists one ActionAudit record describing a triggered pipeline.
// The record is intentionally not owned by the policy: audit history must
// outlive the policy and is reclaimed only by the retention sweep (ADR-003).
func (r *ReactivePolicyReconciler) writeAudit(ctx context.Context, policy *v1alpha1.ReactivePolicy, triggeredAt metav1.Time, results []action.Result) error {
	records := make([]v1alpha1.ActionRecord, 0, len(results))
	for i := range results {
		// #nosec G115 -- an action pipeline has a handful of entries; the index
		// cannot approach the int32 limit.
		idx := int32(i)
		records = append(records, r.toRecord(idx, results[i]))
	}

	audit := &v1alpha1.ActionAudit{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: policy.Name + "-",
			Namespace:    policy.Namespace,
			Labels:       map[string]string{v1alpha1.LabelPolicy: policy.Name},
		},
		Spec: v1alpha1.ActionAuditSpec{
			PolicyRef:   policy.Name,
			PolicyUID:   string(policy.UID),
			TriggeredAt: triggeredAt,
			MetricValue: policy.Status.CurrentMetricValue,
			Actions:     records,
		},
	}
	if err := r.Create(ctx, audit); err != nil {
		return fmt.Errorf("creating audit record: %w", err)
	}
	if s := r.AuditSink; s != nil {
		if err := s.RecordTrigger(ctx, buildSinkEvents(audit)); err != nil {
			log.FromContext(ctx).Error(err, "audit sink: recording trigger", "audit", audit.Name)
		}
	}
	return nil
}

func buildSinkEvents(audit *v1alpha1.ActionAudit) []sink.Event {
	events := make([]sink.Event, 0, len(audit.Spec.Actions))
	for i := range audit.Spec.Actions {
		rec := audit.Spec.Actions[i]
		ev := sink.Event{
			AuditUID:         string(audit.UID),
			AuditName:        audit.Name,
			AuditNamespace:   audit.Namespace,
			PolicyRef:        audit.Spec.PolicyRef,
			PolicyUID:        audit.Spec.PolicyUID,
			TriggeredAt:      audit.Spec.TriggeredAt.Time,
			MetricValue:      audit.Spec.MetricValue,
			ActionIndex:      rec.Index,
			ActionID:         rec.ActionID,
			Plugin:           rec.Plugin,
			TargetAPIVersion: rec.Target.APIVersion,
			TargetKind:       rec.Target.Kind,
			TargetNamespace:  rec.Target.Namespace,
			TargetName:       rec.Target.Name,
			Status:           rec.Status,
			Message:          rec.Message,
			Reversible:       rec.Reversible,
		}
		if rec.Details != nil && len(rec.Details.Raw) > 0 {
			ev.DetailsJSON = append([]byte(nil), rec.Details.Raw...)
		}
		events = append(events, ev)
	}
	return events
}

// toRecord maps an executor Result onto its serialized ActionRecord, resolving
// reversibility from the registry and marshaling plugin Details to raw JSON.
func (r *ReactivePolicyReconciler) toRecord(index int32, res action.Result) v1alpha1.ActionRecord {
	rec := v1alpha1.ActionRecord{
		Index:    index,
		ActionID: res.ActionID,
		Plugin:   res.PluginName,
		Target: v1alpha1.AuditTarget{
			APIVersion: res.Target.APIVersion,
			Kind:       res.Target.Kind,
			Name:       res.Target.Name,
			Namespace:  res.Target.Namespace,
		},
		Status:  string(res.Status),
		Message: res.Message,
	}
	if plugin := r.Executor.Lookup(res.PluginName); plugin != nil {
		rec.Reversible = plugin.IsReversible()
	}
	if len(res.Details) > 0 {
		if raw, err := json.Marshal(res.Details); err == nil {
			rec.Details = &apiextensionsv1.JSON{Raw: raw}
		}
	}
	return rec
}

// recordToResult reconstructs an executor Result from a persisted record so the
// plugin's Reverse can be replayed.
func recordToResult(rec v1alpha1.ActionRecord) action.Result {
	res := action.Result{
		ActionID:   rec.ActionID,
		PluginName: rec.Plugin,
		Target: action.Target{
			APIVersion: rec.Target.APIVersion,
			Kind:       rec.Target.Kind,
			Name:       rec.Target.Name,
			Namespace:  rec.Target.Namespace,
		},
		Status:  action.ResultStatus(rec.Status),
		Message: rec.Message,
	}
	if rec.Details != nil && len(rec.Details.Raw) > 0 {
		details := map[string]any{}
		if err := json.Unmarshal(rec.Details.Raw, &details); err == nil {
			res.Details = details
		}
	}
	return res
}
