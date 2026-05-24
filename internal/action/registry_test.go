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
	"testing"

	"github.com/Vedooo/reactive-policy/internal/action"
	acttest "github.com/Vedooo/reactive-policy/internal/action/testing"
)

func TestRegistryLookup(t *testing.T) {
	reg := action.NewRegistry()
	nop := acttest.NewNop("nop")
	reg.Register(nop)

	if got := reg.Lookup("nop"); got != action.Action(nop) {
		t.Fatalf("Lookup(nop) = %v, want the registered plugin", got)
	}
	if got := reg.Lookup("missing"); got != nil {
		t.Fatalf("Lookup(missing) = %v, want nil", got)
	}
}

func TestRegistryAllIsSorted(t *testing.T) {
	reg := acttest.NewFakeRegistry(
		acttest.NewNop("k8s.annotate"),
		acttest.NewNop("argocd.suspend"),
		acttest.NewNop("notify.slack"),
	)

	all := reg.All()
	want := []string{"argocd.suspend", "k8s.annotate", "notify.slack"}
	if len(all) != len(want) {
		t.Fatalf("All() returned %d plugins, want %d", len(all), len(want))
	}
	for i, name := range want {
		if all[i].Name() != name {
			t.Errorf("All()[%d] = %q, want %q", i, all[i].Name(), name)
		}
	}
}

func TestRegistryRegisterDuplicatePanics(t *testing.T) {
	reg := action.NewRegistry()
	reg.Register(acttest.NewNop("dup"))

	defer func() {
		if recover() == nil {
			t.Fatal("registering a duplicate plugin should panic")
		}
	}()
	reg.Register(acttest.NewNop("dup"))
}
