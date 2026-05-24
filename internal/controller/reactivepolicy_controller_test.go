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
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/Vedooo/reactive-policy/api/v1alpha1"
	"github.com/Vedooo/reactive-policy/internal/action"
	acttest "github.com/Vedooo/reactive-policy/internal/action/testing"
	"github.com/Vedooo/reactive-policy/internal/prometheus"
)

var errUnreachable = errors.New("connection refused")

type fakePromClient struct {
	value float64
	err   error
}

func (f *fakePromClient) Query(context.Context, string) (float64, error) {
	return f.value, f.err
}

func newReconciler(fake *fakePromClient, plugins ...action.Action) *ReactivePolicyReconciler {
	return &ReactivePolicyReconciler{
		Client: k8sClient,
		Scheme: k8sClient.Scheme(),
		Prometheus: func(string, ...prometheus.Option) (prometheus.Client, error) {
			return fake, nil
		},
		Window:   prometheus.NewSlidingWindow(),
		Executor: action.NewExecutor(acttest.NewFakeRegistry(plugins...)),
		Limiter:  newAuditLimiter(k8sClient),
	}
}

func makePolicy(name string, threshold string, op v1alpha1.ComparisonOperator) *v1alpha1.ReactivePolicy {
	return &v1alpha1.ReactivePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: v1alpha1.ReactivePolicySpec{
			Target: v1alpha1.Target{
				Selector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}},
				Kinds:    []v1alpha1.TargetKind{{APIVersion: "apps/v1", Kind: "Deployment"}},
			},
			Observe: v1alpha1.Observe{
				Source:    v1alpha1.SourcePrometheus,
				Endpoint:  "http://prometheus.monitoring:9090",
				Query:     "vector(1)",
				Threshold: threshold,
				Operator:  op,
				Duration:  metav1.Duration{Duration: time.Minute},
			},
			Actions: []v1alpha1.Action{{Plugin: "nop"}},
		},
	}
}

// makeDeployment builds a minimal valid Deployment in the default namespace
// carrying the given metadata labels, used to exercise target resolution.
func makeDeployment(name string, labels map[string]string) *appsv1.Deployment {
	replicas := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"pod": name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"pod": name}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx"}}},
			},
		},
	}
}

var _ = Describe("ReactivePolicy Controller", func() {
	ctx := context.Background()

	apply := func(policy *v1alpha1.ReactivePolicy) types.NamespacedName {
		Expect(k8sClient.Create(ctx, policy)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, policy)
		})
		return types.NamespacedName{Name: policy.Name, Namespace: policy.Namespace}
	}

	// auditsFor lists the audit records a policy has produced, and registers a
	// cleanup so records (which intentionally outlive their policy) do not leak
	// between specs.
	auditsFor := func(policyName string) []v1alpha1.ActionAudit {
		var list v1alpha1.ActionAuditList
		Expect(k8sClient.List(ctx, &list,
			client.InNamespace("default"),
			client.MatchingLabels{v1alpha1.LabelPolicy: policyName})).To(Succeed())
		return list.Items
	}

	cleanupAudits := func(policyName string) {
		DeferCleanup(func() {
			for i := range auditsFor(policyName) {
				rec := auditsFor(policyName)[i]
				_ = k8sClient.Delete(ctx, &rec)
			}
		})
	}

	condition := func(nn types.NamespacedName, condType string) *metav1.Condition {
		var got v1alpha1.ReactivePolicy
		Expect(k8sClient.Get(ctx, nn, &got)).To(Succeed())
		return meta.FindStatusCondition(got.Status.Conditions, condType)
	}

	It("populates currentMetricValue and stays within threshold (happy path)", func() {
		nn := apply(makePolicy("happy", "0.05", v1alpha1.OpGreaterThan))
		r := newReconciler(&fakePromClient{value: 0.012})

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		var got v1alpha1.ReactivePolicy
		Expect(k8sClient.Get(ctx, nn, &got)).To(Succeed())
		Expect(got.Status.CurrentMetricValue).To(Equal("0.012"))
		Expect(got.Status.State).To(Equal(v1alpha1.StateWatching))
		Expect(got.Status.LastEvaluatedAt).NotTo(BeNil())

		Expect(condition(nn, ConditionMetricSourceReachable).Status).To(Equal(metav1.ConditionTrue))
		Expect(condition(nn, ConditionThresholdCrossed).Status).To(Equal(metav1.ConditionFalse))
		Expect(condition(nn, ConditionReady).Status).To(Equal(metav1.ConditionTrue))
	})

	It("reports MetricSourceReachable=False when the query fails", func() {
		nn := apply(makePolicy("unreachable", "0.05", v1alpha1.OpGreaterThan))
		r := newReconciler(&fakePromClient{err: errUnreachable})

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		cond := condition(nn, ConditionMetricSourceReachable)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("QueryFailed"))
	})

	It("flags ThresholdCrossed=True when the metric crosses (briefly, not yet sustained)", func() {
		nn := apply(makePolicy("crossed", "0.05", v1alpha1.OpGreaterThan))
		r := newReconciler(&fakePromClient{value: 0.9})

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(condition(nn, ConditionThresholdCrossed).Status).To(Equal(metav1.ConditionTrue))
		var got v1alpha1.ReactivePolicy
		Expect(k8sClient.Get(ctx, nn, &got)).To(Succeed())
		Expect(got.Status.State).To(Equal(v1alpha1.StateWatching))
		Expect(got.Status.CurrentMetricValue).To(Equal("0.9"))
	})

	It("marks the policy Invalid when the threshold is unparseable", func() {
		nn := apply(makePolicy("badthreshold", "not-a-number", v1alpha1.OpGreaterThan))
		r := newReconciler(&fakePromClient{value: 1})

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		var got v1alpha1.ReactivePolicy
		Expect(k8sClient.Get(ctx, nn, &got)).To(Succeed())
		Expect(got.Status.State).To(Equal(v1alpha1.StateInvalid))
		Expect(condition(nn, ConditionReady).Status).To(Equal(metav1.ConditionFalse))
	})

	It("triggers the pipeline and writes an audit record once the threshold is sustained", func() {
		nop := acttest.NewNop("nop")
		cleanupAudits("trigger")
		nn := apply(makePolicy("trigger", "0.05", v1alpha1.OpGreaterThan))
		r := newReconciler(&fakePromClient{value: 0.9}, nop)
		// Pre-seed the window so the crossing is already older than the duration.
		r.Window.Observe(nn.String(), true, time.Now().Add(-time.Hour))

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(nop.ExecuteCount()).To(Equal(1))

		var got v1alpha1.ReactivePolicy
		Expect(k8sClient.Get(ctx, nn, &got)).To(Succeed())
		Expect(got.Status.State).To(Equal(v1alpha1.StateTriggering))
		Expect(got.Status.TriggerCount).To(Equal(int32(1)))
		Expect(got.Status.LastTriggeredAt).NotTo(BeNil())

		audits := auditsFor("trigger")
		Expect(audits).To(HaveLen(1))
		Expect(audits[0].Spec.PolicyRef).To(Equal("trigger"))
		Expect(audits[0].Spec.Actions).To(HaveLen(1))
		Expect(audits[0].Spec.Actions[0].Plugin).To(Equal("nop"))
		Expect(audits[0].Spec.Actions[0].Status).To(Equal(string(action.StatusSucceeded)))
	})

	It("does not re-trigger while cooling down", func() {
		nop := acttest.NewNop("nop")
		cleanupAudits("cooldown")
		nn := apply(makePolicy("cooldown", "0.05", v1alpha1.OpGreaterThan))
		r := newReconciler(&fakePromClient{value: 0.9}, nop)
		r.Window.Observe(nn.String(), true, time.Now().Add(-time.Hour))

		// First reconcile triggers.
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(nop.ExecuteCount()).To(Equal(1))

		// Second reconcile is still within the default 5m cooldown: no execution.
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(nop.ExecuteCount()).To(Equal(1))

		var got v1alpha1.ReactivePolicy
		Expect(k8sClient.Get(ctx, nn, &got)).To(Succeed())
		Expect(got.Status.State).To(Equal(v1alpha1.StateCooldown))
	})

	It("stops triggering once maxTriggersPerHour is reached", func() {
		nop := acttest.NewNop("nop")
		cleanupAudits("ratelimited")
		policy := makePolicy("ratelimited", "0.05", v1alpha1.OpGreaterThan)
		one := int32(1)
		policy.Spec.MaxTriggersPerHour = &one
		nn := apply(policy)

		// Persist a prior trigger this hour as an ActionAudit and elapse cooldown.
		prior := auditAt("ratelimited-prior", "ratelimited", time.Now())
		Expect(k8sClient.Create(ctx, prior)).To(Succeed())
		var cur v1alpha1.ReactivePolicy
		Expect(k8sClient.Get(ctx, nn, &cur)).To(Succeed())
		cur.Status.LastTriggeredAt = &metav1.Time{Time: time.Now().Add(-time.Hour)}
		Expect(k8sClient.Status().Update(ctx, &cur)).To(Succeed())

		r := newReconciler(&fakePromClient{value: 0.9}, nop)
		r.Window.Observe(nn.String(), true, time.Now().Add(-time.Hour))
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(nop.ExecuteCount()).To(Equal(0))

		var got v1alpha1.ReactivePolicy
		Expect(k8sClient.Get(ctx, nn, &got)).To(Succeed())
		Expect(got.Status.State).To(Equal(v1alpha1.StateRateLimited))
		Expect(condition(nn, ConditionRateLimited).Status).To(Equal(metav1.ConditionTrue))
	})

	It("keeps the rate limit across an operator restart by counting persisted audits", func() {
		cleanupAudits("restart")
		policy := makePolicy("restart", "0.05", v1alpha1.OpGreaterThan)
		one := int32(1)
		policy.Spec.MaxTriggersPerHour = &one
		nn := apply(policy)

		// First operator instance triggers once, writing the audit record.
		first := newReconciler(&fakePromClient{value: 0.9}, acttest.NewNop("nop"))
		first.Window.Observe(nn.String(), true, time.Now().Add(-time.Hour))
		_, err := first.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(auditsFor("restart")).To(HaveLen(1))

		// Elapse the cooldown so only the rate limit can stop the next trigger.
		var cur v1alpha1.ReactivePolicy
		Expect(k8sClient.Get(ctx, nn, &cur)).To(Succeed())
		cur.Status.LastTriggeredAt = &metav1.Time{Time: time.Now().Add(-time.Hour)}
		Expect(k8sClient.Status().Update(ctx, &cur)).To(Succeed())

		// A brand-new operator instance has an empty limiter cache, yet must still
		// see the prior trigger from the persisted audit and refuse to fire.
		restarted := newReconciler(&fakePromClient{value: 0.9}, acttest.NewNop("nop"))
		restartedNop := acttest.NewNop("nop")
		restarted.Executor = action.NewExecutor(acttest.NewFakeRegistry(restartedNop))
		restarted.Window.Observe(nn.String(), true, time.Now().Add(-time.Hour))
		_, err = restarted.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(restartedNop.ExecuteCount()).To(Equal(0))

		var got v1alpha1.ReactivePolicy
		Expect(k8sClient.Get(ctx, nn, &got)).To(Succeed())
		Expect(got.Status.State).To(Equal(v1alpha1.StateRateLimited))
	})

	It("resolves the target selector and runs the pipeline against each matched resource", func() {
		nop := acttest.NewNop("nop")
		cleanupAudits("resolve")
		d1 := makeDeployment("api-1", map[string]string{"app": "demo"})
		d2 := makeDeployment("api-2", map[string]string{"app": "demo"})
		other := makeDeployment("other-1", map[string]string{"app": "other"})
		for _, d := range []*appsv1.Deployment{d1, d2, other} {
			Expect(k8sClient.Create(ctx, d)).To(Succeed())
		}
		DeferCleanup(func() {
			for _, d := range []*appsv1.Deployment{d1, d2, other} {
				_ = k8sClient.Delete(ctx, d)
			}
		})

		nn := apply(makePolicy("resolve", "0.05", v1alpha1.OpGreaterThan))
		r := newReconciler(&fakePromClient{value: 0.9}, nop)
		r.Window.Observe(nn.String(), true, time.Now().Add(-time.Hour))

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		// The pipeline runs once per matched Deployment, skipping the non-match.
		Expect(nop.ExecuteCount()).To(Equal(2))

		// One audit per trigger, holding a record per resolved target.
		audits := auditsFor("resolve")
		Expect(audits).To(HaveLen(1))
		Expect(audits[0].Spec.Actions).To(HaveLen(2))
		var names []string
		for _, rec := range audits[0].Spec.Actions {
			Expect(rec.Target.Kind).To(Equal("Deployment"))
			Expect(rec.Target.APIVersion).To(Equal("apps/v1"))
			names = append(names, rec.Target.Name)
		}
		Expect(names).To(ConsistOf("api-1", "api-2"))
	})

	It("refuses to trigger when the selector matches more than maxResources", func() {
		nop := acttest.NewNop("nop")
		cleanupAudits("capped")
		d1 := makeDeployment("cap-1", map[string]string{"app": "capped"})
		d2 := makeDeployment("cap-2", map[string]string{"app": "capped"})
		for _, d := range []*appsv1.Deployment{d1, d2} {
			Expect(k8sClient.Create(ctx, d)).To(Succeed())
		}
		DeferCleanup(func() {
			for _, d := range []*appsv1.Deployment{d1, d2} {
				_ = k8sClient.Delete(ctx, d)
			}
		})

		policy := makePolicy("capped", "0.05", v1alpha1.OpGreaterThan)
		policy.Spec.Target.Selector = metav1.LabelSelector{MatchLabels: map[string]string{"app": "capped"}}
		one := int32(1)
		policy.Spec.Target.MaxResources = &one
		nn := apply(policy)
		r := newReconciler(&fakePromClient{value: 0.9}, nop)
		r.Window.Observe(nn.String(), true, time.Now().Add(-time.Hour))

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		// Two resources matched a cap of one: the policy refuses to act.
		Expect(nop.ExecuteCount()).To(Equal(0))
		Expect(auditsFor("capped")).To(BeEmpty())
		ready := condition(nn, ConditionReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal("TargetResolutionFailed"))
	})
})
