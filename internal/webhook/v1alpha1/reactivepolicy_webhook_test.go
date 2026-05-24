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
	"errors"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1alpha1 "github.com/Vedooo/reactive-policy/api/v1alpha1"
	"github.com/Vedooo/reactive-policy/internal/action"
	acttest "github.com/Vedooo/reactive-policy/internal/action/testing"
)

func validPolicy(actions ...apiv1alpha1.Action) *apiv1alpha1.ReactivePolicy {
	return &apiv1alpha1.ReactivePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec: apiv1alpha1.ReactivePolicySpec{
			Target: apiv1alpha1.Target{
				Selector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}},
				Kinds:    []apiv1alpha1.TargetKind{{APIVersion: "apps/v1", Kind: "Deployment"}},
			},
			Observe: apiv1alpha1.Observe{
				Source:    apiv1alpha1.SourcePrometheus,
				Endpoint:  "http://prometheus:9090",
				Query:     "vector(1)",
				Threshold: "0.5",
				Operator:  apiv1alpha1.OpGreaterThan,
				Duration:  metav1.Duration{Duration: time.Minute},
			},
			Actions: actions,
		},
	}
}

func newValidator(plugins ...action.Action) *ReactivePolicyCustomValidator {
	return &ReactivePolicyCustomValidator{registry: acttest.NewFakeRegistry(plugins...)}
}

func TestValidateCreateAcceptsKnownReversiblePlugin(t *testing.T) {
	v := newValidator(acttest.NewNop("nop"))

	if _, err := v.ValidateCreate(context.Background(), validPolicy(apiv1alpha1.Action{Plugin: "nop"})); err != nil {
		t.Fatalf("a known reversible plugin should be accepted, got: %v", err)
	}
}

func TestValidateCreateRejectsUnknownPlugin(t *testing.T) {
	v := newValidator() // empty registry

	if _, err := v.ValidateCreate(context.Background(), validPolicy(apiv1alpha1.Action{Plugin: "ghost"})); err == nil {
		t.Fatal("an unknown plugin should be rejected")
	}
}

func TestValidateCreateRejectsPluginValidateError(t *testing.T) {
	bad := acttest.NewNop("bad")
	bad.ValidateErr = errors.New("missing required field")
	v := newValidator(bad)

	if _, err := v.ValidateCreate(context.Background(), validPolicy(apiv1alpha1.Action{Plugin: "bad"})); err == nil {
		t.Fatal("a plugin Validate error should be rejected")
	}
}

func TestValidateCreateIrreversibleRequiresOptIn(t *testing.T) {
	v := newValidator(acttest.NewNop("kill").SetReversible(false))

	t.Run("rejected without opt-in", func(t *testing.T) {
		if _, err := v.ValidateCreate(context.Background(), validPolicy(apiv1alpha1.Action{Plugin: "kill"})); err == nil {
			t.Fatal("an irreversible plugin without allowIrreversible should be rejected")
		}
	})

	t.Run("accepted with opt-in", func(t *testing.T) {
		policy := validPolicy(apiv1alpha1.Action{Plugin: "kill"})
		policy.Spec.AllowIrreversible = true
		if _, err := v.ValidateCreate(context.Background(), policy); err != nil {
			t.Fatalf("allowIrreversible=true should permit an irreversible plugin, got: %v", err)
		}
	})
}

func TestValidateRejectsSanityFloorViolations(t *testing.T) {
	v := newValidator(acttest.NewNop("nop"))

	cases := map[string]func(*apiv1alpha1.ReactivePolicy){
		"short cooldown": func(p *apiv1alpha1.ReactivePolicy) { p.Spec.Cooldown = metav1.Duration{Duration: time.Second} },
		"short poll interval": func(p *apiv1alpha1.ReactivePolicy) {
			p.Spec.Observe.PollInterval = metav1.Duration{Duration: time.Second}
		},
		"short duration": func(p *apiv1alpha1.ReactivePolicy) { p.Spec.Observe.Duration = metav1.Duration{Duration: time.Second} },
		"bad source":     func(p *apiv1alpha1.ReactivePolicy) { p.Spec.Observe.Source = "datadog" },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			policy := validPolicy(apiv1alpha1.Action{Plugin: "nop"})
			mutate(policy)
			if _, err := v.ValidateCreate(context.Background(), policy); err == nil {
				t.Fatalf("%s should be rejected", name)
			}
		})
	}
}
