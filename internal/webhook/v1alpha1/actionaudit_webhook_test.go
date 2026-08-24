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
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	apiv1alpha1 "github.com/Vedooo/reactive-policy/api/v1alpha1"
)

const approver = "alice@example.com"

func newApprovalHandler(t *testing.T) *ActionAuditApprovalHandler {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := apiv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding scheme: %v", err)
	}
	return &ActionAuditApprovalHandler{Decoder: admission.NewDecoder(scheme)}
}

// gatedAudit builds a record holding an open gate that expires in an hour.
func gatedAudit() *apiv1alpha1.ActionAudit {
	return &apiv1alpha1.ActionAudit{
		ObjectMeta: metav1.ObjectMeta{Name: "audit-1", Namespace: "default"},
		Spec: apiv1alpha1.ActionAuditSpec{
			PolicyRef:   "p",
			TriggeredAt: metav1.Now(),
			Gate: &apiv1alpha1.ApprovalGate{
				ActionIndex:    1,
				PendingPlugins: []string{"network.isolate"},
				ExpiresAt:      metav1.Time{Time: time.Now().Add(time.Hour)},
			},
		},
		Status: apiv1alpha1.ActionAuditStatus{ApprovalPhase: apiv1alpha1.PhasePending},
	}
}

func handleUpdate(t *testing.T, h *ActionAuditApprovalHandler, oldObj, newObj *apiv1alpha1.ActionAudit) admission.Response {
	t.Helper()
	oldRaw, err := json.Marshal(oldObj)
	if err != nil {
		t.Fatalf("marshaling old object: %v", err)
	}
	newRaw, err := json.Marshal(newObj)
	if err != nil {
		t.Fatalf("marshaling new object: %v", err)
	}
	return h.Handle(context.Background(), admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
			Object:    runtime.RawExtension{Raw: newRaw},
			OldObject: runtime.RawExtension{Raw: oldRaw},
			UserInfo:  authenticationv1.UserInfo{Username: approver},
		},
	})
}

// patchedGate applies the response's JSON patch and returns the resulting gate,
// which is how the stamped identity is observed.
func patchedGate(t *testing.T, resp admission.Response, obj *apiv1alpha1.ActionAudit) *apiv1alpha1.ApprovalGate {
	t.Helper()
	raw, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("marshaling object: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshaling object: %v", err)
	}
	for _, p := range resp.Patches {
		// The handler only ever adds or replaces gate fields, so a shallow
		// application over spec.gate is enough for the assertion.
		if p.Path == "/spec/gate/decidedBy" {
			gate := doc["spec"].(map[string]any)["gate"].(map[string]any)
			gate["decidedBy"] = p.Value
		}
	}
	patched, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("re-marshaling: %v", err)
	}
	var out apiv1alpha1.ActionAudit
	if err := json.Unmarshal(patched, &out); err != nil {
		t.Fatalf("decoding patched object: %v", err)
	}
	return out.Spec.Gate
}

func TestApprovalStampsIdentityFromTheRequest(t *testing.T) {
	h := newApprovalHandler(t)
	oldObj := gatedAudit()
	newObj := gatedAudit()
	newObj.Spec.Gate.Decision = apiv1alpha1.DecisionApproved

	resp := handleUpdate(t, h, oldObj, newObj)
	if !resp.Allowed {
		t.Fatalf("a decision on an open gate should be admitted: %v", resp.Result)
	}
	if gate := patchedGate(t, resp, newObj); gate.DecidedBy != approver {
		t.Errorf("decidedBy = %q, want %q", gate.DecidedBy, approver)
	}
}

func TestApprovalOverwritesAForgedApprover(t *testing.T) {
	h := newApprovalHandler(t)
	oldObj := gatedAudit()
	newObj := gatedAudit()
	newObj.Spec.Gate.Decision = apiv1alpha1.DecisionApproved
	// A client claiming someone else signed off. The API server knows better.
	newObj.Spec.Gate.DecidedBy = "the-cto@example.com"

	resp := handleUpdate(t, h, oldObj, newObj)
	if !resp.Allowed {
		t.Fatalf("the decision should still be admitted: %v", resp.Result)
	}
	if gate := patchedGate(t, resp, newObj); gate.DecidedBy != approver {
		t.Errorf("decidedBy = %q, want the authenticated %q — a forged approver must be overwritten",
			gate.DecidedBy, approver)
	}
}

func TestApprovalRejectsRewritingAStampedIdentity(t *testing.T) {
	h := newApprovalHandler(t)
	decidedAt := metav1.Now()
	oldObj := gatedAudit()
	oldObj.Spec.Gate.Decision = apiv1alpha1.DecisionApproved
	oldObj.Spec.Gate.DecidedBy = approver
	oldObj.Spec.Gate.DecidedAt = &decidedAt
	oldObj.Status.ApprovalPhase = apiv1alpha1.PhaseApproved

	newObj := oldObj.DeepCopy()
	newObj.Spec.Gate.DecidedBy = "someone-else@example.com"

	if resp := handleUpdate(t, h, oldObj, newObj); resp.Allowed {
		t.Error("rewriting decidedBy after the fact should be denied")
	}
}

func TestApprovalIsWriteOnce(t *testing.T) {
	h := newApprovalHandler(t)
	oldObj := gatedAudit()
	oldObj.Spec.Gate.Decision = apiv1alpha1.DecisionDenied
	oldObj.Spec.Gate.DecidedBy = approver
	oldObj.Status.ApprovalPhase = apiv1alpha1.PhaseDenied

	newObj := oldObj.DeepCopy()
	newObj.Spec.Gate.Decision = apiv1alpha1.DecisionApproved

	if resp := handleUpdate(t, h, oldObj, newObj); resp.Allowed {
		t.Error("a recorded decision must not be changed into another one")
	}
}

func TestApprovalRejectedOnAnExpiredGate(t *testing.T) {
	h := newApprovalHandler(t)
	oldObj := gatedAudit()
	oldObj.Spec.Gate.ExpiresAt = metav1.Time{Time: time.Now().Add(-time.Minute)}
	newObj := oldObj.DeepCopy()
	newObj.Spec.Gate.Decision = apiv1alpha1.DecisionApproved

	if resp := handleUpdate(t, h, oldObj, newObj); resp.Allowed {
		t.Error("a lapsed gate must not admit a verdict; expiry is fail-closed")
	}
}

func TestApprovalRejectedWhenGateIsNotOpen(t *testing.T) {
	h := newApprovalHandler(t)
	oldObj := gatedAudit()
	oldObj.Status.ApprovalPhase = apiv1alpha1.PhaseExpired
	newObj := oldObj.DeepCopy()
	newObj.Spec.Gate.Decision = apiv1alpha1.DecisionApproved

	if resp := handleUpdate(t, h, oldObj, newObj); resp.Allowed {
		t.Error("a gate that already reached a terminal phase must not admit a verdict")
	}
}

func TestApprovalRejectsClientCreatedAndRemovedGates(t *testing.T) {
	h := newApprovalHandler(t)

	t.Run("adding a gate", func(t *testing.T) {
		oldObj := gatedAudit()
		oldObj.Spec.Gate = nil
		oldObj.Status.ApprovalPhase = ""
		if resp := handleUpdate(t, h, oldObj, gatedAudit()); resp.Allowed {
			t.Error("a client must not be able to open a gate")
		}
	})

	t.Run("removing a gate", func(t *testing.T) {
		newObj := gatedAudit()
		newObj.Spec.Gate = nil
		if resp := handleUpdate(t, h, gatedAudit(), newObj); resp.Allowed {
			t.Error("a client must not be able to erase the approval trail")
		}
	})
}

func TestApprovalIgnoresUnrelatedUpdates(t *testing.T) {
	h := newApprovalHandler(t)
	oldObj := gatedAudit()
	newObj := oldObj.DeepCopy()
	newObj.Spec.RevertRequested = true

	if resp := handleUpdate(t, h, oldObj, newObj); !resp.Allowed {
		t.Errorf("an update that does not touch the gate should pass: %v", resp.Result)
	}
}
