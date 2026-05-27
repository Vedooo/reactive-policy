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
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	reactivepolicyiov1alpha1 "github.com/Vedooo/reactive-policy/api/v1alpha1"
	"github.com/Vedooo/reactive-policy/internal/action"
	"github.com/Vedooo/reactive-policy/internal/metrics"
)

var reactivepolicylog = logf.Log.WithName("reactivepolicy-resource")

// Sanity floors and ceilings the webhook enforces beyond the CRD schema
// (see docs/CRD_SPEC.md §5 and ADR-007).
const (
	minCooldown     = 30 * time.Second
	maxCooldown     = 24 * time.Hour
	minPollInterval = 10 * time.Second
	maxPollInterval = 5 * time.Minute
	minDuration     = 30 * time.Second
	maxDuration     = 24 * time.Hour
)

var reactivePolicyGK = schema.GroupKind{Group: "reactive-policy.io", Kind: "ReactivePolicy"}

// SetupReactivePolicyWebhookWithManager registers the validating webhook for
// ReactivePolicy, validating action params against the given plugin registry.
func SetupReactivePolicyWebhookWithManager(mgr ctrl.Manager, registry *action.Registry) error {
	return ctrl.NewWebhookManagedBy(mgr, &reactivepolicyiov1alpha1.ReactivePolicy{}).
		WithValidator(&ReactivePolicyCustomValidator{registry: registry}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-reactive-policy-io-v1alpha1-reactivepolicy,mutating=false,failurePolicy=fail,sideEffects=None,groups=reactive-policy.io,resources=reactivepolicies,verbs=create;update,versions=v1alpha1,name=vreactivepolicy-v1alpha1.kb.io,admissionReviewVersions=v1

// ReactivePolicyCustomValidator validates ReactivePolicy resources on admission.
type ReactivePolicyCustomValidator struct {
	registry *action.Registry
}

var _ admission.Validator[*reactivepolicyiov1alpha1.ReactivePolicy] = &ReactivePolicyCustomValidator{}

// ValidateCreate validates a ReactivePolicy on creation.
func (v *ReactivePolicyCustomValidator) ValidateCreate(_ context.Context, policy *reactivepolicyiov1alpha1.ReactivePolicy) (admission.Warnings, error) {
	reactivepolicylog.Info("validating create", "name", policy.GetName())
	return nil, v.validate(policy)
}

// ValidateUpdate validates a ReactivePolicy on update.
func (v *ReactivePolicyCustomValidator) ValidateUpdate(_ context.Context, _, newObj *reactivepolicyiov1alpha1.ReactivePolicy) (admission.Warnings, error) {
	reactivepolicylog.Info("validating update", "name", newObj.GetName())
	return nil, v.validate(newObj)
}

// ValidateDelete allows all deletions; there is nothing to validate.
func (v *ReactivePolicyCustomValidator) ValidateDelete(_ context.Context, _ *reactivepolicyiov1alpha1.ReactivePolicy) (admission.Warnings, error) {
	return nil, nil
}

// validate runs the semantic checks the CRD's OpenAPI schema cannot express:
// the sanity floors on observe and timing fields, plugin existence and
// parameter validity, and the reversibility opt-in (see docs/ARCHITECTURE.md
// §5.1 and docs/CRD_SPEC.md §5).
func (v *ReactivePolicyCustomValidator) validate(policy *reactivepolicyiov1alpha1.ReactivePolicy) error {
	spec := field.NewPath("spec")

	var errs field.ErrorList //nolint:prealloc // small, variable number of appended lists
	errs = append(errs, validateObserve(spec.Child("observe"), policy.Spec.Observe)...)
	errs = append(errs, validateTimings(spec, policy.Spec)...)
	errs = append(errs, v.validateActions(spec.Child("actions"), policy.Spec)...)

	if len(errs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(reactivePolicyGK, policy.Name, errs)
}

func validateObserve(path *field.Path, o reactivepolicyiov1alpha1.Observe) field.ErrorList {
	var errs field.ErrorList
	if o.Source != reactivepolicyiov1alpha1.SourcePrometheus {
		errs = append(errs, field.NotSupported(path.Child("source"), o.Source,
			[]string{string(reactivepolicyiov1alpha1.SourcePrometheus)}))
	}
	if d := o.Duration.Duration; d < minDuration || d > maxDuration {
		errs = append(errs, field.Invalid(path.Child("duration"), d.String(),
			fmt.Sprintf("must be between %s and %s", minDuration, maxDuration)))
	}
	if p := o.PollInterval.Duration; p != 0 && (p < minPollInterval || p > maxPollInterval) {
		errs = append(errs, field.Invalid(path.Child("pollInterval"), p.String(),
			fmt.Sprintf("must be between %s and %s", minPollInterval, maxPollInterval)))
	}
	return errs
}

func validateTimings(path *field.Path, s reactivepolicyiov1alpha1.ReactivePolicySpec) field.ErrorList {
	var errs field.ErrorList
	if c := s.Cooldown.Duration; c != 0 && (c < minCooldown || c > maxCooldown) {
		errs = append(errs, field.Invalid(path.Child("cooldown"), c.String(),
			fmt.Sprintf("must be between %s and %s", minCooldown, maxCooldown)))
	}
	return errs
}

func (v *ReactivePolicyCustomValidator) validateActions(path *field.Path, s reactivepolicyiov1alpha1.ReactivePolicySpec) field.ErrorList {
	errs := make(field.ErrorList, 0, len(s.Actions))
	for i := range s.Actions {
		act := s.Actions[i]
		p := path.Index(i)

		plugin := v.registry.Lookup(act.Plugin)
		if plugin == nil {
			metrics.RecordValidationError(act.Plugin)
			errs = append(errs, field.NotFound(p.Child("plugin"), act.Plugin))
			continue
		}
		if err := plugin.Validate(action.ParamsFromCRD(act.Params)); err != nil {
			metrics.RecordValidationError(act.Plugin)
			errs = append(errs, field.Invalid(p.Child("params"), act.Plugin, err.Error()))
		}
		if !plugin.IsReversible() && !s.AllowIrreversible {
			errs = append(errs, field.Forbidden(p.Child("plugin"),
				fmt.Sprintf("plugin %q is irreversible; set spec.allowIrreversible=true to permit it", act.Plugin)))
		}
	}
	return errs
}
