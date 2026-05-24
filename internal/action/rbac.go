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

package action

import (
	"sort"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
)

// AggregatePermissions merges the RequiredPermissions of all given plugins into
// a deduplicated, deterministically ordered set of RBAC rules. Rules sharing the
// same API groups and resources have their verbs unioned. The operator's
// ClusterRole and the Helm chart are built from these (docs/ARCHITECTURE.md §3.3).
func AggregatePermissions(plugins []Action) []rbacv1.PolicyRule {
	buckets := map[string]*ruleBucket{}
	order := make([]string, 0)
	for _, p := range plugins {
		for _, rule := range p.RequiredPermissions() {
			groups := dedupeSort(rule.APIGroups)
			resources := dedupeSort(rule.Resources)
			k := strings.Join(groups, "\x1f") + "|" + strings.Join(resources, "\x1f")
			b, ok := buckets[k]
			if !ok {
				b = &ruleBucket{groups: groups, resources: resources, verbs: map[string]struct{}{}}
				buckets[k] = b
				order = append(order, k)
			}
			for _, v := range rule.Verbs {
				b.verbs[v] = struct{}{}
			}
		}
	}

	rules := make([]rbacv1.PolicyRule, 0, len(order))
	for _, k := range order {
		b := buckets[k]
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: b.groups,
			Resources: b.resources,
			Verbs:     setToSorted(b.verbs),
		})
	}
	sort.Slice(rules, func(i, j int) bool {
		gi, gj := strings.Join(rules[i].APIGroups, ","), strings.Join(rules[j].APIGroups, ",")
		if gi != gj {
			return gi < gj
		}
		return strings.Join(rules[i].Resources, ",") < strings.Join(rules[j].Resources, ",")
	})
	return rules
}

type ruleBucket struct {
	groups    []string
	resources []string
	verbs     map[string]struct{}
}

func dedupeSort(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func setToSorted(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
