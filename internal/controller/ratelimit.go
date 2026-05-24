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
	"fmt"
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Vedooo/reactive-policy/api/v1alpha1"
)

// auditLimiter enforces a rolling one-hour cap on triggers per policy from the
// persisted ActionAudit records (ADR-009). Counting persisted records means the
// limit survives operator restarts, unlike the in-memory scheme it replaces.
//
// To avoid a LIST against etcd on every reconcile, the count is cached per
// policy for a caller-supplied TTL (the policy's pollInterval). A freshly
// recorded trigger bumps the cached count via Observe so the cap holds even
// before the cache refreshes.
type auditLimiter struct {
	reader client.Reader

	mu    sync.Mutex
	cache map[string]limiterEntry
}

type limiterEntry struct {
	// queriedAt is when count was last computed from a LIST.
	queriedAt time.Time
	// count is the number of triggers observed within the trailing hour as of
	// queriedAt, plus any triggers recorded via Observe since.
	count int
}

func newAuditLimiter(reader client.Reader) *auditLimiter {
	return &auditLimiter{reader: reader, cache: make(map[string]limiterEntry)}
}

// Allowed reports whether the policy identified by namespace/name may trigger at
// now without exceeding maxPerHour. ttl caps how stale the cached count may be.
func (l *auditLimiter) Allowed(ctx context.Context, namespace, name string, maxPerHour int, now time.Time, ttl time.Duration) (bool, error) {
	count, err := l.count(ctx, namespace, name, now, ttl)
	if err != nil {
		return false, err
	}
	return count < maxPerHour, nil
}

// count returns the number of triggers in the trailing hour ending at now,
// serving a cached value when it is younger than ttl.
func (l *auditLimiter) count(ctx context.Context, namespace, name string, now time.Time, ttl time.Duration) (int, error) {
	key := namespace + "/" + name

	l.mu.Lock()
	if entry, ok := l.cache[key]; ok && now.Sub(entry.queriedAt) < ttl {
		c := entry.count
		l.mu.Unlock()
		return c, nil
	}
	l.mu.Unlock()

	var list v1alpha1.ActionAuditList
	if err := l.reader.List(ctx, &list,
		client.InNamespace(namespace),
		client.MatchingLabels{v1alpha1.LabelPolicy: name},
	); err != nil {
		return 0, fmt.Errorf("listing audit records for rate limit: %w", err)
	}

	cutoff := now.Add(-time.Hour)
	count := 0
	for i := range list.Items {
		if list.Items[i].Spec.TriggeredAt.Time.After(cutoff) {
			count++
		}
	}

	l.mu.Lock()
	l.cache[key] = limiterEntry{queriedAt: now, count: count}
	l.mu.Unlock()
	return count, nil
}

// Observe bumps the cached trigger count after the operator records a new
// trigger, so the cap reflects it before the cache TTL expires.
func (l *auditLimiter) Observe(namespace, name string, now time.Time) {
	key := namespace + "/" + name
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.cache[key]
	if !ok {
		// No cached count to bump; the next Allowed will LIST and include the
		// record we just wrote.
		return
	}
	entry.count++
	entry.queriedAt = now
	l.cache[key] = entry
}

// Forget drops cached state for a policy, e.g. when it is deleted.
func (l *auditLimiter) Forget(namespace, name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.cache, namespace+"/"+name)
}
