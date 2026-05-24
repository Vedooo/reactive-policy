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

package action_test

import (
	"reflect"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/Vedooo/reactive-policy/internal/action"
	acttest "github.com/Vedooo/reactive-policy/internal/action/testing"
)

func pluginWithPerms(name string, rules ...rbacv1.PolicyRule) action.Action {
	p := acttest.NewNop(name)
	p.Permissions = rules
	return p
}

func TestAggregatePermissionsMergesVerbs(t *testing.T) {
	a := pluginWithPerms("a",
		rbacv1.PolicyRule{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get"}},
	)
	b := pluginWithPerms("b",
		rbacv1.PolicyRule{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"list", "get"}},
	)

	rules := action.AggregatePermissions([]action.Action{a, b})
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1 merged rule", len(rules))
	}
	if want := []string{"get", "list"}; !reflect.DeepEqual(rules[0].Verbs, want) {
		t.Errorf("verbs = %v, want %v (deduped and sorted)", rules[0].Verbs, want)
	}
}

func TestAggregatePermissionsDistinctRulesSorted(t *testing.T) {
	plugins := []action.Action{
		pluginWithPerms("argocd",
			rbacv1.PolicyRule{APIGroups: []string{"argoproj.io"}, Resources: []string{"applications"}, Verbs: []string{"patch", "get"}},
		),
		pluginWithPerms("slack",
			rbacv1.PolicyRule{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get"}},
		),
	}

	rules := action.AggregatePermissions(plugins)
	if len(rules) != 2 {
		t.Fatalf("got %d rules, want 2 distinct rules", len(rules))
	}
	// Empty API group ("") sorts before "argoproj.io".
	if rules[0].APIGroups[0] != "" || rules[0].Resources[0] != "secrets" {
		t.Errorf("first rule = %+v, want core/secrets first", rules[0])
	}
	if rules[1].APIGroups[0] != "argoproj.io" {
		t.Errorf("second rule = %+v, want argoproj.io", rules[1])
	}
	if want := []string{"get", "patch"}; !reflect.DeepEqual(rules[1].Verbs, want) {
		t.Errorf("argocd verbs = %v, want %v", rules[1].Verbs, want)
	}
}

func TestAggregatePermissionsEmpty(t *testing.T) {
	if rules := action.AggregatePermissions(nil); len(rules) != 0 {
		t.Errorf("AggregatePermissions(nil) = %v, want empty", rules)
	}
}
