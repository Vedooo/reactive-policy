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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/Vedooo/reactive-policy/api/v1alpha1"
	"github.com/Vedooo/reactive-policy/internal/action"
	acttest "github.com/Vedooo/reactive-policy/internal/action/testing"
)

// makeGatedPolicy builds a policy whose pipeline notifies first and then holds
// for approval, the shape the gate exists for: the harmless step runs during
// the incident, the destructive one waits for a person.
func makeGatedPolicy(name string, timeout time.Duration) *v1alpha1.ReactivePolicy {
	policy := makePolicy(name, "0.05", v1alpha1.OpGreaterThan)
	policy.Spec.Actions = []v1alpha1.Action{
		{Plugin: "notify"},
		{Plugin: "isolate", RequiresApproval: true},
	}
	policy.Spec.ApprovalTimeout = metav1.Duration{Duration: timeout}
	return policy
}

var _ = Describe("Approval gate", func() {
	ctx := context.Background()

	auditsFor := func(policyName string) []v1alpha1.ActionAudit {
		var list v1alpha1.ActionAuditList
		Expect(k8sClient.List(ctx, &list,
			client.InNamespace("default"),
			client.MatchingLabels{v1alpha1.LabelPolicy: policyName})).To(Succeed())
		return list.Items
	}

	apply := func(policy *v1alpha1.ReactivePolicy) types.NamespacedName {
		Expect(k8sClient.Create(ctx, policy)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, policy)
			for i := range auditsFor(policy.Name) {
				rec := auditsFor(policy.Name)[i]
				_ = k8sClient.Delete(ctx, &rec)
			}
		})
		return types.NamespacedName{Name: policy.Name, Namespace: policy.Namespace}
	}

	// trigger drives one sustained evaluation, pre-seeding the window so the
	// crossing already counts as sustained.
	trigger := func(r *ReactivePolicyReconciler, nn types.NamespacedName) {
		r.Window.Observe(nn.String(), true, time.Now().Add(-time.Hour))
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
	}

	reconcileAudit := func(ar *ActionAuditReconciler, audit *v1alpha1.ActionAudit) {
		_, err := ar.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{
			Name: audit.Name, Namespace: audit.Namespace,
		}})
		Expect(err).NotTo(HaveOccurred())
	}

	decide := func(audit *v1alpha1.ActionAudit, decision v1alpha1.ApprovalDecision, who string) {
		var fresh v1alpha1.ActionAudit
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: audit.Name, Namespace: audit.Namespace}, &fresh)).To(Succeed())
		fresh.Spec.Gate.Decision = decision
		// envtest runs without the admission webhook, so the test stands in for
		// the stamp the webhook would apply.
		fresh.Spec.Gate.DecidedBy = who
		now := metav1.Now()
		fresh.Spec.Gate.DecidedAt = &now
		Expect(k8sClient.Update(ctx, &fresh)).To(Succeed())
	}

	It("runs the actions before the gate and holds the rest", func() {
		notify, isolate := acttest.NewNop("notify"), acttest.NewNop("isolate")
		nn := apply(makeGatedPolicy("gate-holds", time.Hour))
		r := newReconciler(&fakePromClient{value: 0.9}, notify, isolate)

		trigger(r, nn)

		Expect(notify.ExecuteCount()).To(Equal(1), "the pre-gate action should run during the incident")
		Expect(isolate.ExecuteCount()).To(Equal(0), "the gated action must not run before approval")

		var policy v1alpha1.ReactivePolicy
		Expect(k8sClient.Get(ctx, nn, &policy)).To(Succeed())
		Expect(policy.Status.State).To(Equal(v1alpha1.StateAwaitingApproval))
		Expect(policy.Status.PendingGateRef).NotTo(BeEmpty())
		Expect(policy.Status.TriggerCount).To(Equal(int32(1)))
		// The cooldown must not start while a human is still thinking.
		Expect(policy.Status.LastTriggeredAt).To(BeNil())

		audits := auditsFor("gate-holds")
		Expect(audits).To(HaveLen(1))
		gate := audits[0].Spec.Gate
		Expect(gate).NotTo(BeNil())
		Expect(gate.ActionIndex).To(Equal(int32(1)))
		Expect(gate.PendingPlugins).To(Equal([]string{"isolate"}))
		Expect(audits[0].Status.ApprovalPhase).To(Equal(v1alpha1.PhasePending))
		Expect(audits[0].Spec.Actions).To(HaveLen(1), "only the pre-gate action is recorded so far")
	})

	It("does not open a second gate while one is still waiting", func() {
		notify, isolate := acttest.NewNop("notify"), acttest.NewNop("isolate")
		nn := apply(makeGatedPolicy("gate-dedupe", time.Hour))
		r := newReconciler(&fakePromClient{value: 0.9}, notify, isolate)

		trigger(r, nn)
		Expect(auditsFor("gate-dedupe")).To(HaveLen(1))

		// The metric is still bad — that is exactly why someone is being asked.
		trigger(r, nn)
		trigger(r, nn)

		Expect(auditsFor("gate-dedupe")).To(HaveLen(1), "a pending gate must absorb re-triggers")
		Expect(notify.ExecuteCount()).To(Equal(1), "the pre-gate action must not re-run behind an open gate")
	})

	It("runs the held actions once approved and starts the cooldown from the decision", func() {
		notify, isolate := acttest.NewNop("notify"), acttest.NewNop("isolate")
		nn := apply(makeGatedPolicy("gate-approve", time.Hour))
		r := newReconciler(&fakePromClient{value: 0.9}, notify, isolate)
		trigger(r, nn)

		audit := auditsFor("gate-approve")[0]
		decide(&audit, v1alpha1.DecisionApproved, "alice@example.com")

		ar := newAuditReconciler(notify, isolate)
		reconcileAudit(ar, &audit)

		Expect(isolate.ExecuteCount()).To(Equal(1), "approval should release the held action")
		Expect(notify.ExecuteCount()).To(Equal(1), "the pre-gate action must not run twice")

		var got v1alpha1.ActionAudit
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: audit.Name, Namespace: audit.Namespace}, &got)).To(Succeed())
		Expect(got.Status.ApprovalPhase).To(Equal(v1alpha1.PhaseApproved))
		Expect(got.Status.ResumedAt).NotTo(BeNil())
		Expect(got.Spec.Actions).To(HaveLen(2), "the released action joins the same record")
		Expect(got.Spec.Actions[1].Plugin).To(Equal("isolate"))
		Expect(got.Spec.Actions[1].Status).To(Equal(string(action.StatusSucceeded)))

		var policy v1alpha1.ReactivePolicy
		Expect(k8sClient.Get(ctx, nn, &policy)).To(Succeed())
		Expect(policy.Status.PendingGateRef).To(BeEmpty())
		Expect(policy.Status.LastTriggeredAt).NotTo(BeNil(), "cooldown starts at the decision, not the trigger")
		Expect(policy.Status.LastTriggeredAt.Time).To(BeTemporally(">=", audit.Spec.TriggeredAt.Time))
	})

	It("records the held actions as skipped when denied", func() {
		notify, isolate := acttest.NewNop("notify"), acttest.NewNop("isolate")
		nn := apply(makeGatedPolicy("gate-deny", time.Hour))
		r := newReconciler(&fakePromClient{value: 0.9}, notify, isolate)
		trigger(r, nn)

		audit := auditsFor("gate-deny")[0]
		decide(&audit, v1alpha1.DecisionDenied, "bob@example.com")

		ar := newAuditReconciler(notify, isolate)
		reconcileAudit(ar, &audit)

		Expect(isolate.ExecuteCount()).To(Equal(0), "a denied gate must never run what it held")

		var got v1alpha1.ActionAudit
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: audit.Name, Namespace: audit.Namespace}, &got)).To(Succeed())
		Expect(got.Status.ApprovalPhase).To(Equal(v1alpha1.PhaseDenied))
		Expect(got.Spec.Actions).To(HaveLen(2))
		Expect(got.Spec.Actions[1].Status).To(Equal(string(action.StatusSkipped)))
		Expect(got.Spec.Actions[1].Message).To(ContainSubstring("bob@example.com"))
	})

	It("denies by expiry when nobody answers in time", func() {
		notify, isolate := acttest.NewNop("notify"), acttest.NewNop("isolate")
		// A gate that lapsed before anyone looked at it.
		nn := apply(makeGatedPolicy("gate-expire", time.Minute))
		r := newReconciler(&fakePromClient{value: 0.9}, notify, isolate)
		trigger(r, nn)

		audit := auditsFor("gate-expire")[0]
		var fresh v1alpha1.ActionAudit
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: audit.Name, Namespace: audit.Namespace}, &fresh)).To(Succeed())
		fresh.Spec.Gate.ExpiresAt = metav1.Time{Time: time.Now().Add(-time.Second)}
		Expect(k8sClient.Update(ctx, &fresh)).To(Succeed())

		ar := newAuditReconciler(notify, isolate)
		reconcileAudit(ar, &fresh)

		Expect(isolate.ExecuteCount()).To(Equal(0), "expiry is fail-closed")

		var got v1alpha1.ActionAudit
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: audit.Name, Namespace: audit.Namespace}, &got)).To(Succeed())
		Expect(got.Status.ApprovalPhase).To(Equal(v1alpha1.PhaseExpired))
		Expect(got.Spec.Actions).To(HaveLen(2))
		Expect(got.Spec.Actions[1].Status).To(Equal(string(action.StatusSkipped)))
	})

	It("refuses to release a gate whose policy was replaced", func() {
		notify, isolate := acttest.NewNop("notify"), acttest.NewNop("isolate")
		nn := apply(makeGatedPolicy("gate-replaced", time.Hour))
		r := newReconciler(&fakePromClient{value: 0.9}, notify, isolate)
		trigger(r, nn)

		audit := auditsFor("gate-replaced")[0]
		// The approver signed off on a pipeline that no longer exists under this
		// name.
		var fresh v1alpha1.ActionAudit
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: audit.Name, Namespace: audit.Namespace}, &fresh)).To(Succeed())
		fresh.Spec.PolicyUID = "a-different-policy"
		fresh.Spec.Gate.Decision = v1alpha1.DecisionApproved
		Expect(k8sClient.Update(ctx, &fresh)).To(Succeed())

		ar := newAuditReconciler(notify, isolate)
		reconcileAudit(ar, &fresh)

		Expect(isolate.ExecuteCount()).To(Equal(0))

		var got v1alpha1.ActionAudit
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: audit.Name, Namespace: audit.Namespace}, &got)).To(Succeed())
		Expect(got.Status.ApprovalPhase).To(Equal(v1alpha1.PhaseDenied))
	})

	It("resumes against the targets recorded at trigger time, not the selector", func() {
		notify, isolate := acttest.NewNop("notify"), acttest.NewNop("isolate")
		Expect(k8sClient.Create(ctx, makeDeployment("gated-one", map[string]string{"app": "demo"}))).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, makeDeployment("gated-one", map[string]string{"app": "demo"}))
			_ = k8sClient.Delete(ctx, makeDeployment("gated-two", map[string]string{"app": "demo"}))
		})

		nn := apply(makeGatedPolicy("gate-targets", time.Hour))
		r := newReconciler(&fakePromClient{value: 0.9}, notify, isolate)
		trigger(r, nn)

		audit := auditsFor("gate-targets")[0]
		Expect(audit.Spec.Gate.Targets).To(HaveLen(1))

		// A second matching resource appears while the gate is open. The
		// approver never saw it, so the released pipeline must not touch it.
		Expect(k8sClient.Create(ctx, makeDeployment("gated-two", map[string]string{"app": "demo"}))).To(Succeed())

		decide(&audit, v1alpha1.DecisionApproved, "carol@example.com")
		ar := newAuditReconciler(notify, isolate)
		reconcileAudit(ar, &audit)

		Expect(isolate.ExecuteCount()).To(Equal(1), "the blast radius must stay what the approver signed off")

		var got v1alpha1.ActionAudit
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: audit.Name, Namespace: audit.Namespace}, &got)).To(Succeed())
		Expect(got.Spec.Actions[1].Target.Name).To(Equal("gated-one"))
	})
})
