/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package sink_test

import (
	"context"
	"testing"
	"time"

	"github.com/Vedooo/reactive-policy/internal/audit/sink"
)

func TestNoop(t *testing.T) {
	var s sink.Sink = sink.Noop{}
	ctx := context.Background()

	if err := s.RecordTrigger(ctx, []sink.Event{{AuditUID: "u", Plugin: "p", TriggeredAt: time.Now()}}); err != nil {
		t.Fatalf("RecordTrigger: %v", err)
	}
	if err := s.RecordRevert(ctx, []sink.RevertEvent{{AuditUID: "u", Plugin: "p", RevertedAt: time.Now()}}); err != nil {
		t.Fatalf("RecordRevert: %v", err)
	}
	if err := s.RecordTrigger(ctx, nil); err != nil {
		t.Fatalf("RecordTrigger(nil): %v", err)
	}
	if err := s.RecordRevert(ctx, nil); err != nil {
		t.Fatalf("RecordRevert(nil): %v", err)
	}
	if err := s.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
