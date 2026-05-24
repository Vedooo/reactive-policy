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
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/Vedooo/reactive-policy/api/v1alpha1"
	"github.com/Vedooo/reactive-policy/internal/action"
	acttest "github.com/Vedooo/reactive-policy/internal/action/testing"
)

func newAuditReconciler(plugins ...action.Action) *ActionAuditReconciler {
	return &ActionAuditReconciler{
		Client:   k8sClient,
		Scheme:   k8sClient.Scheme(),
		Executor: action.NewExecutor(acttest.NewFakeRegistry(plugins...)),
	}
}

var _ = Describe("ActionAudit Controller", func() {
	ctx := context.Background()

	applyAudit := func(audit *v1alpha1.ActionAudit) types.NamespacedName {
		Expect(k8sClient.Create(ctx, audit)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, audit)
		})
		return types.NamespacedName{Name: audit.Name, Namespace: audit.Namespace}
	}

	It("reverses reversible, succeeded actions when a revert is requested", func() {
		nop := acttest.NewNop("nop")
		nop.SetReversible(true)

		nn := applyAudit(&v1alpha1.ActionAudit{
			ObjectMeta: metav1.ObjectMeta{Name: "revert-ok", Namespace: "default"},
			Spec: v1alpha1.ActionAuditSpec{
				PolicyRef:       "revpol",
				TriggeredAt:     metav1.Now(),
				RevertRequested: true,
				Actions: []v1alpha1.ActionRecord{{
					Index:      0,
					Plugin:     "nop",
					Status:     string(action.StatusSucceeded),
					Reversible: true,
					Details:    &apiextensionsv1.JSON{Raw: []byte(`{"key":"value"}`)},
				}},
			},
		})

		r := newAuditReconciler(nop)
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(nop.ReverseCount()).To(Equal(1))

		var got v1alpha1.ActionAudit
		Expect(k8sClient.Get(ctx, nn, &got)).To(Succeed())
		Expect(got.Status.Reverted).To(BeTrue())
		Expect(got.Status.RevertedAt).NotTo(BeNil())
		Expect(got.Status.RevertResults).To(HaveLen(1))
		Expect(got.Status.RevertResults[0].Status).To(Equal(string(action.StatusSucceeded)))
	})

	It("skips actions that are not reversible or did not succeed", func() {
		nn := applyAudit(&v1alpha1.ActionAudit{
			ObjectMeta: metav1.ObjectMeta{Name: "revert-skip", Namespace: "default"},
			Spec: v1alpha1.ActionAuditSpec{
				PolicyRef:       "skippol",
				TriggeredAt:     metav1.Now(),
				RevertRequested: true,
				Actions: []v1alpha1.ActionRecord{
					{Index: 0, Plugin: "irreversible", Status: string(action.StatusSucceeded), Reversible: false},
					{Index: 1, Plugin: "failed", Status: string(action.StatusFailed), Reversible: true},
				},
			},
		})

		r := newAuditReconciler()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		var got v1alpha1.ActionAudit
		Expect(k8sClient.Get(ctx, nn, &got)).To(Succeed())
		Expect(got.Status.Reverted).To(BeTrue())
		Expect(got.Status.RevertResults).To(HaveLen(2))
		for _, rr := range got.Status.RevertResults {
			Expect(rr.Status).To(Equal(string(action.StatusSkipped)))
		}
	})

	It("does not revert a record without a revert request", func() {
		nn := applyAudit(&v1alpha1.ActionAudit{
			ObjectMeta: metav1.ObjectMeta{Name: "no-revert", Namespace: "default"},
			Spec: v1alpha1.ActionAuditSpec{
				PolicyRef:   "quietpol",
				TriggeredAt: metav1.Now(),
				Actions:     []v1alpha1.ActionRecord{{Index: 0, Plugin: "nop", Status: string(action.StatusSucceeded)}},
			},
		})

		r := newAuditReconciler()
		res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		// Not expired and no revert: requeue near the retention horizon.
		Expect(res.RequeueAfter).To(BeNumerically(">", time.Hour))

		var got v1alpha1.ActionAudit
		Expect(k8sClient.Get(ctx, nn, &got)).To(Succeed())
		Expect(got.Status.Reverted).To(BeFalse())
	})
})

func TestParseRetention(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"30d", 30 * 24 * time.Hour, true},
		{"1y", 365 * 24 * time.Hour, true},
		{"12h", 12 * time.Hour, true},
		{"90m", 90 * time.Minute, true},
		{"bogus", 0, false},
		{"5x", 0, false},
	}
	for _, tc := range cases {
		got, err := parseRetention(tc.in)
		if tc.ok {
			if err != nil {
				t.Errorf("parseRetention(%q) unexpected error: %v", tc.in, err)
				continue
			}
			if got != tc.want {
				t.Errorf("parseRetention(%q) = %v, want %v", tc.in, got, tc.want)
			}
		} else if err == nil {
			t.Errorf("parseRetention(%q) expected error, got %v", tc.in, got)
		}
	}
}
