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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LabelPolicy is set on every ActionAudit record to the name of the policy that
// produced it. The controller selects on it to count recent triggers (ADR-009)
// and the CLI uses it to filter history.
const LabelPolicy = "reactive-policy.io/policy"

// ApprovalDecision is the verdict a human records on an open approval gate.
// +kubebuilder:validation:Enum=Approved;Denied
type ApprovalDecision string

const (
	// DecisionApproved releases the actions held behind the gate.
	DecisionApproved ApprovalDecision = "Approved"
	// DecisionDenied abandons them; they are recorded as Skipped.
	DecisionDenied ApprovalDecision = "Denied"
)

// ApprovalPhase is where a gated record sits in its approval lifecycle. It is
// empty on records that ran straight through without gating.
// +kubebuilder:validation:Enum=Pending;Approved;Denied;Expired
type ApprovalPhase string

const (
	// PhasePending means the gate is open and waiting for a decision.
	PhasePending ApprovalPhase = "Pending"
	// PhaseApproved means the held actions were released and have run.
	PhaseApproved ApprovalPhase = "Approved"
	// PhaseDenied means a human refused the gate.
	PhaseDenied ApprovalPhase = "Denied"
	// PhaseExpired means no decision arrived before approvalTimeout elapsed.
	// Expiry denies: the held actions never run (ADR-011).
	PhaseExpired ApprovalPhase = "Expired"
)

// ApprovalGate is the pre-execution hold written when a pipeline stops at an
// action with requiresApproval. The record is created before the gated action
// runs rather than after, so the approval token and the audit trail are the
// same object (ADR-011).
type ApprovalGate struct {
	// ActionIndex is the zero-based position of the gated action in the policy
	// pipeline. Actions before it already ran and are recorded in spec.actions;
	// this one and every action after it are held.
	ActionIndex int32 `json:"actionIndex"`

	// PendingPlugins names the plugins waiting behind the gate, in pipeline
	// order, so an approver can see what they are releasing without fetching
	// the policy.
	// +optional
	PendingPlugins []string `json:"pendingPlugins,omitempty"`

	// Targets are the resources the held actions will operate on, resolved at
	// trigger time. Recording them is what makes the gate reviewable: the
	// approver sees the blast radius, and the resumed pipeline is confined to
	// the same set even if the selector would match more by then.
	// +optional
	Targets []AuditTarget `json:"targets,omitempty"`

	// ExpiresAt is when an undecided gate is denied.
	ExpiresAt metav1.Time `json:"expiresAt"`

	// Decision is the human verdict, set by `rp action approve|deny`. Empty
	// while the gate is still waiting. It is write-once: the validating webhook
	// rejects any attempt to change or clear a recorded decision.
	// +optional
	Decision ApprovalDecision `json:"decision,omitempty"`

	// DecidedBy is the identity that recorded the decision. The mutating
	// webhook stamps it from the admission request's user info, overwriting
	// whatever the client sent, so it cannot be forged by writing the field.
	// +optional
	DecidedBy string `json:"decidedBy,omitempty"`

	// DecidedAt is when the decision was admitted, stamped by the same webhook
	// that sets DecidedBy.
	// +optional
	DecidedAt *metav1.Time `json:"decidedAt,omitempty"`

	// Reason is the optional free-text justification supplied with the
	// decision.
	// +optional
	Reason string `json:"reason,omitempty"`
}

// AuditTarget identifies the Kubernetes resource an audited action operated on.
type AuditTarget struct {
	// APIVersion is the group/version of the target, e.g. "argoproj.io/v1alpha1".
	// +optional
	APIVersion string `json:"apiVersion,omitempty"`

	// Kind is the target resource kind.
	// +optional
	Kind string `json:"kind,omitempty"`

	// Name is the target resource name.
	// +optional
	Name string `json:"name,omitempty"`

	// Namespace is the target resource namespace.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// ActionRecord is the immutable outcome of a single action in a triggered
// pipeline. Details carries plugin-specific data needed to reverse the action.
type ActionRecord struct {
	// Index is the action's zero-based position in the policy pipeline.
	Index int32 `json:"index"`

	// ActionID is the executor-assigned identifier for this execution.
	// +optional
	ActionID string `json:"actionID,omitempty"`

	// Plugin is the registered plugin name that ran, e.g. "argocd.suspend".
	Plugin string `json:"plugin"`

	// Target is the resource the action operated on.
	// +optional
	Target AuditTarget `json:"target,omitempty"`

	// Status is the action's terminal state: Succeeded, Failed, or Skipped.
	Status string `json:"status"`

	// Message is a human-readable summary of the outcome.
	// +optional
	Message string `json:"message,omitempty"`

	// Reversible reports whether the plugin that ran supports reversal.
	// +optional
	Reversible bool `json:"reversible,omitempty"`

	// Details is the plugin-specific payload recorded at execution time. The
	// operator passes it back to the plugin's Reverse on revert.
	// +optional
	Details *apiextensionsv1.JSON `json:"details,omitempty"`
}

// ActionAuditSpec is the immutable record of one policy trigger and the actions
// it ran. The operator writes it once; the only mutable field is
// RevertRequested, which the CLI sets to ask the operator to undo the actions.
type ActionAuditSpec struct {
	// PolicyRef is the name of the ReactivePolicy that triggered. The record
	// lives in the same namespace as the policy.
	PolicyRef string `json:"policyRef"`

	// PolicyUID disambiguates records when a policy is deleted and recreated.
	// +optional
	PolicyUID string `json:"policyUID,omitempty"`

	// TriggeredAt is the shared timestamp of the trigger that produced the
	// pipeline. Rate limiting counts records by this time (ADR-009).
	TriggeredAt metav1.Time `json:"triggeredAt"`

	// MetricValue is the observed value that crossed the threshold.
	// +optional
	MetricValue string `json:"metricValue,omitempty"`

	// Actions is the ordered set of action outcomes for this trigger.
	Actions []ActionRecord `json:"actions"`

	// RevertRequested asks the operator to reverse this record's reversible
	// actions. Set by `rp action revert`; cleared semantics are one-shot — the
	// operator records the outcome in status and does not act twice.
	// +optional
	RevertRequested bool `json:"revertRequested,omitempty"`

	// Gate is the approval hold this record is carrying, present only when the
	// pipeline stopped at an action with requiresApproval. Nil on records that
	// ran straight through.
	// +optional
	Gate *ApprovalGate `json:"gate,omitempty"`
}

// RevertResult is the per-action outcome of a reversal attempt.
type RevertResult struct {
	// Index is the action's position in the original pipeline.
	Index int32 `json:"index"`

	// Plugin is the plugin name that was reversed.
	Plugin string `json:"plugin"`

	// Status is the reversal outcome: Succeeded, Skipped, or Failed.
	Status string `json:"status"`

	// Message is a human-readable summary of the reversal.
	// +optional
	Message string `json:"message,omitempty"`
}

// ActionAuditStatus records the result of a revert request. It is empty until a
// revert is requested and processed by the operator.
type ActionAuditStatus struct {
	// Reverted is true once the operator has processed a revert request for this
	// record, regardless of per-action success.
	// +optional
	Reverted bool `json:"reverted,omitempty"`

	// RevertedAt is when the operator processed the revert request.
	// +optional
	RevertedAt *metav1.Time `json:"revertedAt,omitempty"`

	// RevertResults is the per-action outcome of the reversal.
	// +optional
	RevertResults []RevertResult `json:"revertResults,omitempty"`

	// ApprovalPhase tracks a gated record from Pending to its terminal phase.
	// Empty on records that never gated.
	// +optional
	ApprovalPhase ApprovalPhase `json:"approvalPhase,omitempty"`

	// ResumedAt is when the operator ran the actions held behind an approved
	// gate. It is the point the policy's cooldown starts from, so time spent
	// waiting for a decision does not consume the quiet period that is meant to
	// follow the action.
	// +optional
	ResumedAt *metav1.Time `json:"resumedAt,omitempty"`

	// Conditions is the standard set of condition objects for the record.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=aa;audit
// +kubebuilder:printcolumn:name="Policy",type=string,JSONPath=`.spec.policyRef`
// +kubebuilder:printcolumn:name="Triggered",type=date,JSONPath=`.spec.triggeredAt`
// +kubebuilder:printcolumn:name="Approval",type=string,JSONPath=`.status.approvalPhase`
// +kubebuilder:printcolumn:name="Reverted",type=boolean,JSONPath=`.status.reverted`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ActionAudit is the auditable record of one ReactivePolicy trigger and every
// action it ran (see ADR-003). Records are queryable history and the source of
// truth for restart-safe rate limiting (ADR-009).
type ActionAudit struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ActionAuditSpec   `json:"spec,omitempty"`
	Status ActionAuditStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ActionAuditList contains a list of ActionAudit.
type ActionAuditList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ActionAudit `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ActionAudit{}, &ActionAuditList{})
}
