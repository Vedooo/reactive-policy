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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/Vedooo/reactive-policy/api/v1alpha1"
)

func auditTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("adding scheme: %v", err)
	}
	return s
}

func auditAt(name, policy string, triggered time.Time) *v1alpha1.ActionAudit {
	return &v1alpha1.ActionAudit{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{v1alpha1.LabelPolicy: policy},
		},
		Spec: v1alpha1.ActionAuditSpec{
			PolicyRef:   policy,
			TriggeredAt: metav1.Time{Time: triggered},
			Actions: []v1alpha1.ActionRecord{
				{Index: 0, Plugin: "nop", Status: "Succeeded"},
			},
		},
	}
}

func TestAuditLimiterCountsOnlyTheLastHour(t *testing.T) {
	now := time.Now()
	c := fake.NewClientBuilder().WithScheme(auditTestScheme(t)).WithObjects(
		auditAt("old", "p", now.Add(-90*time.Minute)),
		auditAt("recent1", "p", now.Add(-30*time.Minute)),
		auditAt("recent2", "p", now.Add(-5*time.Minute)),
		auditAt("other", "q", now.Add(-1*time.Minute)),
	).Build()

	l := newAuditLimiter(c)
	count, err := l.count(context.Background(), "default", "p", now, time.Second)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 triggers in the last hour for policy p, got %d", count)
	}
}

func TestAuditLimiterEnforcesCap(t *testing.T) {
	now := time.Now()
	c := fake.NewClientBuilder().WithScheme(auditTestScheme(t)).WithObjects(
		auditAt("a1", "p", now.Add(-30*time.Minute)),
		auditAt("a2", "p", now.Add(-5*time.Minute)),
	).Build()
	l := newAuditLimiter(c)

	if ok, _ := l.Allowed(context.Background(), "default", "p", 2, now, time.Second); ok {
		t.Fatal("cap 2 with 2 recent triggers should deny")
	}
	if ok, _ := l.Allowed(context.Background(), "default", "p", 3, now, time.Second); !ok {
		t.Fatal("cap 3 with 2 recent triggers should allow")
	}
}

func TestAuditLimiterCachesWithinTTL(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	c := fake.NewClientBuilder().WithScheme(auditTestScheme(t)).WithObjects(
		auditAt("a1", "p", now.Add(-5*time.Minute)),
	).Build()
	l := newAuditLimiter(c)

	if n, _ := l.count(ctx, "default", "p", now, time.Minute); n != 1 {
		t.Fatalf("initial count want 1, got %d", n)
	}
	// A record created after the cache was warmed must not be seen until the TTL
	// lapses.
	if err := c.Create(ctx, auditAt("a2", "p", now.Add(-2*time.Minute))); err != nil {
		t.Fatalf("create: %v", err)
	}
	if n, _ := l.count(ctx, "default", "p", now.Add(30*time.Second), time.Minute); n != 1 {
		t.Fatalf("within TTL want cached 1, got %d", n)
	}
	if n, _ := l.count(ctx, "default", "p", now.Add(2*time.Minute), time.Minute); n != 2 {
		t.Fatalf("after TTL want refreshed 2, got %d", n)
	}
}

func TestAuditLimiterObserveBumpsCachedCount(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	c := fake.NewClientBuilder().WithScheme(auditTestScheme(t)).Build()
	l := newAuditLimiter(c)

	if n, _ := l.count(ctx, "default", "p", now, time.Minute); n != 0 {
		t.Fatalf("empty count want 0, got %d", n)
	}
	l.Observe("default", "p", now)
	if n, _ := l.count(ctx, "default", "p", now, time.Minute); n != 1 {
		t.Fatalf("after Observe want cached 1, got %d", n)
	}
}

func TestAuditLimiterForgetClearsCache(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	c := fake.NewClientBuilder().WithScheme(auditTestScheme(t)).Build()
	l := newAuditLimiter(c)

	l.Observe("default", "p", now) // no-op without a cached entry
	_, _ = l.count(ctx, "default", "p", now, time.Minute)
	l.Observe("default", "p", now)
	l.Forget("default", "p")
	if n, _ := l.count(ctx, "default", "p", now, time.Minute); n != 0 {
		t.Fatalf("after Forget want re-queried 0, got %d", n)
	}
}
