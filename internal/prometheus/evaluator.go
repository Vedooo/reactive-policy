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
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Vedooo/reactive-policy/api/v1alpha1"
)

func ParseThreshold(s string) (float64, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("parsing threshold %q: %w", s, err)
	}
	return v, nil
}

func Compare(value, threshold float64, op v1alpha1.ComparisonOperator) bool {
	switch op {
	case v1alpha1.OpGreaterThan:
		return value > threshold
	case v1alpha1.OpGreaterThanOrEqual:
		return value >= threshold
	case v1alpha1.OpLessThan:
		return value < threshold
	case v1alpha1.OpLessThanOrEqual:
		return value <= threshold
	case v1alpha1.OpEqual:
		return value == threshold
	case v1alpha1.OpNotEqual:
		return value != threshold
	default:
		return false
	}
}

// SlidingWindow tracks how long each policy's threshold has stayed crossed.
// State is in-memory only; an operator restart resets it.
type SlidingWindow struct {
	mu        sync.Mutex
	crossedAt map[string]time.Time
}

func NewSlidingWindow() *SlidingWindow {
	return &SlidingWindow{crossedAt: make(map[string]time.Time)}
}

// Observe returns how long key's threshold has been continuously crossed: zero
// on the first crossing or when not crossed, otherwise the elapsed time.
func (w *SlidingWindow) Observe(key string, crossed bool, now time.Time) time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !crossed {
		delete(w.crossedAt, key)
		return 0
	}

	since, ok := w.crossedAt[key]
	if !ok {
		w.crossedAt[key] = now
		return 0
	}
	return now.Sub(since)
}

func (w *SlidingWindow) Forget(key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.crossedAt, key)
}
