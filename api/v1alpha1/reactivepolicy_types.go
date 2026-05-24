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
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MetricSource identifies the kind of metric backend a policy observes. Only
// Prometheus is supported in v0.1 (see ADR-005).
// +kubebuilder:validation:Enum=prometheus
type MetricSource string

const (
	// SourcePrometheus queries a Prometheus HTTP API endpoint.
	SourcePrometheus MetricSource = "prometheus"
)

// ComparisonOperator is the relation used to compare the observed metric value
// against the configured threshold.
// +kubebuilder:validation:Enum=GreaterThan;GreaterThanOrEqual;LessThan;LessThanOrEqual;Equal;NotEqual
type ComparisonOperator string

const (
	// OpGreaterThan triggers when value > threshold.
	OpGreaterThan ComparisonOperator = "GreaterThan"
	// OpGreaterThanOrEqual triggers when value >= threshold.
	OpGreaterThanOrEqual ComparisonOperator = "GreaterThanOrEqual"
	// OpLessThan triggers when value < threshold.
	OpLessThan ComparisonOperator = "LessThan"
	// OpLessThanOrEqual triggers when value <= threshold.
	OpLessThanOrEqual ComparisonOperator = "LessThanOrEqual"
	// OpEqual triggers when value == threshold.
	OpEqual ComparisonOperator = "Equal"
	// OpNotEqual triggers when value != threshold.
	OpNotEqual ComparisonOperator = "NotEqual"
)

// FailurePolicy controls what happens to the action pipeline when one action
// fails (see ADR-006).
// +kubebuilder:validation:Enum=continue;stop;rollback
type FailurePolicy string

const (
	// FailureContinue runs the remaining actions despite the failure.
	FailureContinue FailurePolicy = "continue"
	// FailureStop halts the pipeline at the failed action (the default).
	FailureStop FailurePolicy = "stop"
	// FailureRollback reverses preceding succeeded actions in reverse order.
	FailureRollback FailurePolicy = "rollback"
)

// PolicyState is a coarse, human-readable summary of where a policy is in its
// lifecycle.
// +kubebuilder:validation:Enum=Watching;Triggering;Cooldown;RateLimited;Invalid
type PolicyState string

const (
	// StateWatching means the policy is evaluating its metric normally.
	StateWatching PolicyState = "Watching"
	// StateTriggering means the action pipeline is currently executing.
	StateTriggering PolicyState = "Triggering"
	// StateCooldown means the policy recently triggered and is cooling down.
	StateCooldown PolicyState = "Cooldown"
	// StateRateLimited means the policy hit its maxTriggersPerHour cap.
	StateRateLimited PolicyState = "RateLimited"
	// StateInvalid means the policy is structurally or semantically invalid.
	StateInvalid PolicyState = "Invalid"
)

// TargetKind identifies a Kubernetes resource kind that a policy's actions may
// operate on.
type TargetKind struct {
	// APIVersion is the group/version of the kind, e.g. "apps/v1" or
	// "argoproj.io/v1alpha1".
	APIVersion string `json:"apiVersion"`

	// Kind is the resource kind, e.g. "Deployment" or "Application".
	Kind string `json:"kind"`
}

// Target defines which resources a policy's actions operate on. Plugins receive
// the matched resources as their Target parameter.
type Target struct {
	// Selector is a standard label selector matched against candidate resources.
	Selector metav1.LabelSelector `json:"selector"`

	// Kinds is the set of resource kinds to match. At least one, at most ten.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=10
	Kinds []TargetKind `json:"kinds"`

	// MaxResources is a safety cap: if the selector matches more resources than
	// this, the policy refuses to act.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=10
	// +optional
	MaxResources *int32 `json:"maxResources,omitempty"`
}

// Observe defines the metric to watch and the threshold condition that triggers
// the policy.
type Observe struct {
	// Source is the metric backend type. Only "prometheus" in v0.1.
	Source MetricSource `json:"source"`

	// Endpoint is the full URL of the metric source HTTP API.
	Endpoint string `json:"endpoint"`

	// Query is the PromQL query. It must return an instant vector with a single
	// sample.
	Query string `json:"query"`

	// Threshold is the quantity-formatted number the metric value is compared
	// against, e.g. "0.05".
	Threshold string `json:"threshold"`

	// Operator is the comparison relation between the value and the threshold.
	Operator ComparisonOperator `json:"operator"`

	// Duration is how long the threshold must stay crossed before triggering.
	// Bounds (min 30s, max 24h) are enforced by the validating webhook.
	Duration metav1.Duration `json:"duration"`

	// PollInterval is how often the metric source is queried. Bounds (min 10s,
	// max 5m) are enforced by the validating webhook.
	// +kubebuilder:default="30s"
	// +optional
	PollInterval metav1.Duration `json:"pollInterval,omitempty"`

	// AuthSecretRef optionally references a bearer token for authenticating to
	// the metric source.
	// +optional
	AuthSecretRef *corev1.SecretKeySelector `json:"authSecretRef,omitempty"`
}

// Action is a single plugin invocation in a policy's pipeline.
type Action struct {
	// Plugin is the registered plugin name, e.g. "notify.slack".
	Plugin string `json:"plugin"`

	// Params are plugin-specific parameters, each value being arbitrary JSON.
	// +optional
	Params map[string]apiextensionsv1.JSON `json:"params,omitempty"`

	// OnFailure controls pipeline behavior if this action fails.
	// +kubebuilder:default=stop
	// +optional
	OnFailure FailurePolicy `json:"onFailure,omitempty"`
}

// AuditSpec configures retention of ActionAudit records produced by a policy.
type AuditSpec struct {
	// Retention is how long ActionAudit records are kept. Supports day ("30d")
	// and year ("1y") units beyond Go durations. Max 1y, enforced by the
	// validating webhook.
	// +kubebuilder:default="30d"
	// +optional
	Retention string `json:"retention,omitempty"`
}

// ReactivePolicySpec defines the desired state of ReactivePolicy.
type ReactivePolicySpec struct {
	// Target selects the resources the policy's actions operate on.
	Target Target `json:"target"`

	// Observe defines the metric and threshold condition to watch.
	Observe Observe `json:"observe"`

	// Actions is the ordered pipeline of plugin invocations run on trigger. At
	// least one action is required.
	// +kubebuilder:validation:MinItems=1
	Actions []Action `json:"actions"`

	// Cooldown is the minimum time between successive triggers. Bounds (min 30s,
	// max 24h) are enforced by the validating webhook.
	// +kubebuilder:default="5m"
	// +optional
	Cooldown metav1.Duration `json:"cooldown,omitempty"`

	// MaxTriggersPerHour caps how many times the policy may trigger in any
	// rolling one-hour window.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=60
	// +kubebuilder:default=5
	// +optional
	MaxTriggersPerHour *int32 `json:"maxTriggersPerHour,omitempty"`

	// AllowIrreversible permits actions whose plugins are not reversible. When
	// false, the webhook rejects such policies.
	// +kubebuilder:default=false
	// +optional
	AllowIrreversible bool `json:"allowIrreversible,omitempty"`

	// Audit configures ActionAudit record retention.
	// +optional
	Audit *AuditSpec `json:"audit,omitempty"`
}

// ReactivePolicyStatus defines the observed state of ReactivePolicy. It is
// written by the controller; users should not edit it.
type ReactivePolicyStatus struct {
	// State is a coarse summary of the policy's lifecycle position.
	// +optional
	State PolicyState `json:"state,omitempty"`

	// LastEvaluatedAt is the time of the most recent metric evaluation.
	// +optional
	LastEvaluatedAt *metav1.Time `json:"lastEvaluatedAt,omitempty"`

	// LastTriggeredAt is the time of the most recent successful trigger.
	// +optional
	LastTriggeredAt *metav1.Time `json:"lastTriggeredAt,omitempty"`

	// TriggerCount is the total number of triggers since policy creation.
	// +optional
	TriggerCount int32 `json:"triggerCount,omitempty"`

	// CurrentMetricValue is the most recently observed metric value.
	// +optional
	CurrentMetricValue string `json:"currentMetricValue,omitempty"`

	// Conditions is the standard set of condition objects for the policy.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=rp;rpolicy
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="Last Triggered",type=date,JSONPath=`.status.lastTriggeredAt`
// +kubebuilder:printcolumn:name="Count",type=integer,JSONPath=`.status.triggerCount`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ReactivePolicy is the Schema for the reactivepolicies API.
type ReactivePolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ReactivePolicySpec   `json:"spec,omitempty"`
	Status ReactivePolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ReactivePolicyList contains a list of ReactivePolicy.
type ReactivePolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ReactivePolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ReactivePolicy{}, &ReactivePolicyList{})
}
