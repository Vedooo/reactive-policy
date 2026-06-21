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

// Package sink defines a pluggable audit sink for forwarding ActionAudit
// outcomes to long-term analytical storage. The CRD remains source of truth;
// sink errors are logged, never fatal.
package sink

import (
	"context"
	"time"
)

// Event is one action's outcome flattened for analytics. Events from the same
// trigger share AuditUID and TriggeredAt.
type Event struct {
	AuditUID         string
	AuditName        string
	AuditNamespace   string
	PolicyRef        string
	PolicyUID        string
	TriggeredAt      time.Time
	MetricValue      string
	ActionIndex      int32
	ActionID         string
	Plugin           string
	TargetAPIVersion string
	TargetKind       string
	TargetNamespace  string
	TargetName       string
	Status           string
	Message          string
	Reversible       bool
	DetailsJSON      []byte
}

// RevertEvent is one action's revert outcome.
type RevertEvent struct {
	AuditUID       string
	AuditName      string
	AuditNamespace string
	PolicyRef      string
	ActionIndex    int32
	Plugin         string
	RevertedAt     time.Time
	Status         string
	Message        string
}

// Sink consumes audit events. Implementations must be safe to call from a hot
// reconcile path.
type Sink interface {
	RecordTrigger(ctx context.Context, events []Event) error
	RecordRevert(ctx context.Context, events []RevertEvent) error
	Close(ctx context.Context) error
}

// Noop accepts everything and stores nothing.
type Noop struct{}

func (Noop) RecordTrigger(context.Context, []Event) error      { return nil }
func (Noop) RecordRevert(context.Context, []RevertEvent) error { return nil }
func (Noop) Close(context.Context) error                       { return nil }
