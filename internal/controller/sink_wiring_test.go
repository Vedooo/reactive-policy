/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
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
	"github.com/Vedooo/reactive-policy/internal/audit/sink/sinktest"
)

var _ = Describe("Audit sink wiring", func() {
	ctx := context.Background()

	cleanupAuditsFor := func(policyName string) {
		DeferCleanup(func() {
			var list v1alpha1.ActionAuditList
			_ = k8sClient.List(ctx, &list,
				client.InNamespace("default"),
				client.MatchingLabels{v1alpha1.LabelPolicy: policyName})
			for i := range list.Items {
				_ = k8sClient.Delete(ctx, &list.Items[i])
			}
		})
	}

	applyPolicy := func(p *v1alpha1.ReactivePolicy) types.NamespacedName {
		Expect(k8sClient.Create(ctx, p)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, p) })
		return types.NamespacedName{Name: p.Name, Namespace: p.Namespace}
	}

	It("forwards a trigger to the sink with one event per action", func() {
		nop := acttest.NewNop("nop")
		rec := sinktest.New()
		cleanupAuditsFor("sink-trigger")

		nn := applyPolicy(makePolicy("sink-trigger", "0.05", v1alpha1.OpGreaterThan))
		r := newReconcilerWithSink(&fakePromClient{value: 0.9}, rec, nop)
		r.Window.Observe(nn.String(), true, time.Now().Add(-time.Hour))

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(rec.Triggers()).To(HaveLen(1))
		batch := rec.Triggers()[0]
		Expect(batch).To(HaveLen(1))
		Expect(batch[0].PolicyRef).To(Equal("sink-trigger"))
		Expect(batch[0].Plugin).To(Equal("nop"))
		Expect(batch[0].Status).To(Equal(string(action.StatusSucceeded)))
		Expect(batch[0].AuditUID).NotTo(BeEmpty())
		Expect(batch[0].TriggeredAt).NotTo(BeZero())
	})

	It("logs and continues when the sink errors", func() {
		nop := acttest.NewNop("nop")
		rec := sinktest.New()
		rec.FailNext = sinktest.ErrInjected
		cleanupAuditsFor("sink-fail")

		nn := applyPolicy(makePolicy("sink-fail", "0.05", v1alpha1.OpGreaterThan))
		r := newReconcilerWithSink(&fakePromClient{value: 0.9}, rec, nop)
		r.Window.Observe(nn.String(), true, time.Now().Add(-time.Hour))

		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(nop.ExecuteCount()).To(Equal(1))
	})

	It("forwards a revert to the sink", func() {
		nop := acttest.NewNop("nop")
		nop.SetReversible(true)
		rec := sinktest.New()

		audit := &v1alpha1.ActionAudit{
			ObjectMeta: metav1.ObjectMeta{Name: "sink-revert", Namespace: "default"},
			Spec: v1alpha1.ActionAuditSpec{
				PolicyRef:       "p",
				TriggeredAt:     metav1.Now(),
				RevertRequested: true,
				Actions: []v1alpha1.ActionRecord{{
					Index:      0,
					Plugin:     "nop",
					Status:     string(action.StatusSucceeded),
					Reversible: true,
				}},
			},
		}
		Expect(k8sClient.Create(ctx, audit)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, audit) })
		nn := types.NamespacedName{Name: audit.Name, Namespace: audit.Namespace}

		r := newAuditReconcilerWithSink(rec, nop)
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(rec.Reverts()).To(HaveLen(1))
		batch := rec.Reverts()[0]
		Expect(batch).To(HaveLen(1))
		Expect(batch[0].Plugin).To(Equal("nop"))
		Expect(batch[0].Status).To(Equal(string(action.StatusSucceeded)))
	})
})
