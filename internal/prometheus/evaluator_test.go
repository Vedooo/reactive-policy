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

package prometheus

import (
	"testing"
	"time"

	"github.com/Vedooo/reactive-policy/api/v1alpha1"
)

func TestParseThreshold(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    float64
		wantErr bool
	}{
		{name: "decimal", in: "0.05", want: 0.05},
		{name: "integer", in: "100", want: 100},
		{name: "whitespace", in: "  1.5 ", want: 1.5},
		{name: "negative", in: "-3", want: -3},
		{name: "invalid", in: "abc", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseThreshold(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got nil", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("ParseThreshold(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		name      string
		value     float64
		threshold float64
		op        v1alpha1.ComparisonOperator
		want      bool
	}{
		{name: "gt true", value: 2, threshold: 1, op: v1alpha1.OpGreaterThan, want: true},
		{name: "gt false", value: 1, threshold: 1, op: v1alpha1.OpGreaterThan, want: false},
		{name: "gte equal", value: 1, threshold: 1, op: v1alpha1.OpGreaterThanOrEqual, want: true},
		{name: "lt true", value: 0.5, threshold: 1, op: v1alpha1.OpLessThan, want: true},
		{name: "lte equal", value: 1, threshold: 1, op: v1alpha1.OpLessThanOrEqual, want: true},
		{name: "eq true", value: 1, threshold: 1, op: v1alpha1.OpEqual, want: true},
		{name: "neq true", value: 2, threshold: 1, op: v1alpha1.OpNotEqual, want: true},
		{name: "neq false", value: 1, threshold: 1, op: v1alpha1.OpNotEqual, want: false},
		{name: "unknown operator", value: 5, threshold: 1, op: v1alpha1.ComparisonOperator("Bogus"), want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Compare(tc.value, tc.threshold, tc.op); got != tc.want {
				t.Errorf("Compare(%v, %v, %s) = %v, want %v", tc.value, tc.threshold, tc.op, got, tc.want)
			}
		})
	}
}

func TestSlidingWindow(t *testing.T) {
	const key = "ns/policy"
	w := NewSlidingWindow()
	base := time.Now()

	if got := w.Observe(key, true, base); got != 0 {
		t.Fatalf("first crossing: got %v, want 0", got)
	}
	if got := w.Observe(key, true, base.Add(90*time.Second)); got != 90*time.Second {
		t.Fatalf("sustained: got %v, want 90s", got)
	}
	if got := w.Observe(key, false, base.Add(2*time.Minute)); got != 0 {
		t.Fatalf("reset: got %v, want 0", got)
	}
	if got := w.Observe(key, true, base.Add(3*time.Minute)); got != 0 {
		t.Fatalf("re-crossing: got %v, want 0", got)
	}
	if got := w.Observe(key, true, base.Add(4*time.Minute)); got != time.Minute {
		t.Fatalf("re-sustained: got %v, want 1m", got)
	}
	w.Forget(key)
	if got := w.Observe(key, true, base.Add(5*time.Minute)); got != 0 {
		t.Fatalf("after forget: got %v, want 0", got)
	}
}

func TestSlidingWindowIndependentKeys(t *testing.T) {
	w := NewSlidingWindow()
	base := time.Now()
	w.Observe("a", true, base)
	w.Observe("b", true, base.Add(time.Minute))
	if got := w.Observe("a", true, base.Add(time.Minute)); got != time.Minute {
		t.Errorf("key a: got %v, want 1m", got)
	}
	if got := w.Observe("b", true, base.Add(time.Minute)); got != 0 {
		t.Errorf("key b: got %v, want 0", got)
	}
}
