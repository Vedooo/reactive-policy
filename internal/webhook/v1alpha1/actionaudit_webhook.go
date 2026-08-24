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

package v1alpha1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	reactivepolicyiov1alpha1 "github.com/Vedooo/reactive-policy/api/v1alpha1"
)

var actionauditlog = logf.Log.WithName("actionaudit-resource")

// ApprovalWebhookPath is where the approval admission handler is served.
const ApprovalWebhookPath = "/mutate-reactive-policy-io-v1alpha1-actionaudit"

// +kubebuilder:webhook:path=/mutate-reactive-policy-io-v1alpha1-actionaudit,mutating=true,failurePolicy=fail,sideEffects=None,groups=reactive-policy.io,resources=actionaudits,verbs=update,versions=v1alpha1,name=mactionaudit-v1alpha1.kb.io,admissionReviewVersions=v1

// ActionAuditApprovalHandler admits decisions on open approval gates.
//
// It exists for one reason the API server cannot solve on its own: Kubernetes
// does not persist who set a field. If the approver's identity were merely
// another field on the object, anyone able to record a decision could also
// record whose decision it was, and the audit trail would prove nothing. The
// handler takes the identity from the authenticated admission request instead
// and stamps it onto the record, overwriting whatever the client sent. Users
// choose the verdict; the API server decides whose name goes next to it.
//
// It also enforces the gate's shape: decisions are write-once, only the
// operator may open or remove a gate, and a lapsed gate admits no verdict at
// all (ADR-011).
type ActionAuditApprovalHandler struct {
	Decoder admission.Decoder
}

// SetupActionAuditWebhookWithManager registers the approval admission handler.
func SetupActionAuditWebhookWithManager(mgr ctrl.Manager) error {
	mgr.GetWebhookServer().Register(ApprovalWebhookPath, &admission.Webhook{
		Handler: &ActionAuditApprovalHandler{Decoder: admission.NewDecoder(mgr.GetScheme())},
	})
	return nil
}

// Handle validates and stamps an update to an ActionAudit's approval gate.
func (h *ActionAuditApprovalHandler) Handle(_ context.Context, req admission.Request) admission.Response {
	if req.Operation != admissionv1.Update {
		return admission.Allowed("")
	}

	var newAudit reactivepolicyiov1alpha1.ActionAudit
	if err := h.Decoder.Decode(req, &newAudit); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	var oldAudit reactivepolicyiov1alpha1.ActionAudit
	if len(req.OldObject.Raw) > 0 {
		if err := h.Decoder.DecodeRaw(req.OldObject, &oldAudit); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
	}

	newGate, oldGate := newAudit.Spec.Gate, oldAudit.Spec.Gate

	switch {
	case newGate == nil && oldGate == nil:
		return admission.Allowed("")
	case newGate == nil:
		return admission.Denied("spec.gate cannot be removed; the approval trail is part of the record")
	case oldGate == nil:
		return admission.Denied("spec.gate is opened by the operator when a pipeline stops at a gated action; it cannot be added by a client")
	}

	// No verdict change. The stamped fields still have to be left alone, or a
	// client could rewrite the approver's name after the fact.
	if newGate.Decision == oldGate.Decision {
		if newGate.DecidedBy != oldGate.DecidedBy || !sameTime(newGate.DecidedAt, oldGate.DecidedAt) {
			return admission.Denied("spec.gate.decidedBy and spec.gate.decidedAt are stamped from the authenticated request and cannot be written directly")
		}
		return admission.Allowed("")
	}

	if oldGate.Decision != "" {
		return admission.Denied(fmt.Sprintf("spec.gate.decision is write-once; this gate was already %s", oldGate.Decision))
	}
	if newGate.Decision != reactivepolicyiov1alpha1.DecisionApproved &&
		newGate.Decision != reactivepolicyiov1alpha1.DecisionDenied {
		return admission.Denied(fmt.Sprintf("spec.gate.decision must be %q or %q",
			reactivepolicyiov1alpha1.DecisionApproved, reactivepolicyiov1alpha1.DecisionDenied))
	}
	if phase := oldAudit.Status.ApprovalPhase; phase != reactivepolicyiov1alpha1.PhasePending {
		return admission.Denied(fmt.Sprintf("this gate is not open for a decision; its approval phase is %q", phase))
	}
	// Fail-closed, enforced at the door as well as in the controller. Admitting
	// a verdict on a lapsed gate would race the reconciler that is about to
	// expire it, and half the time the actions would run anyway.
	if !time.Now().Before(oldGate.ExpiresAt.Time) {
		return admission.Denied("the approval gate has expired; the actions it held will not run")
	}

	patched := newAudit.DeepCopy()
	now := metav1.Now()
	patched.Spec.Gate.DecidedBy = requesterName(req)
	patched.Spec.Gate.DecidedAt = &now

	marshaled, err := json.Marshal(patched)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	actionauditlog.Info("admitting approval decision",
		"audit", newAudit.Name, "namespace", newAudit.Namespace,
		"decision", newGate.Decision, "decidedBy", patched.Spec.Gate.DecidedBy)
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaled)
}

// requesterName is the authenticated identity behind the admission request. It
// comes from the API server, not from the object, which is the whole point.
func requesterName(req admission.Request) string {
	if req.UserInfo.Username != "" {
		return req.UserInfo.Username
	}
	return "unknown"
}

func sameTime(a, b *metav1.Time) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return a.Equal(b)
	}
}
