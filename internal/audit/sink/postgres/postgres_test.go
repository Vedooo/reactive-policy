/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Vedooo/reactive-policy/internal/audit/sink"
)

type fakeWriter struct {
	mu       sync.Mutex
	triggers [][]sink.Event
	reverts  [][]sink.RevertEvent
	closed   bool
	failNext error
}

func (f *fakeWriter) writeTriggers(_ context.Context, events []sink.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failNext; err != nil {
		f.failNext = nil
		return err
	}
	f.triggers = append(f.triggers, append([]sink.Event(nil), events...))
	return nil
}

func (f *fakeWriter) writeReverts(_ context.Context, events []sink.RevertEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reverts = append(f.reverts, append([]sink.RevertEvent(nil), events...))
	return nil
}

func (f *fakeWriter) close(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeWriter) snapshot() (trigs [][]sink.Event, revs [][]sink.RevertEvent, closed bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]sink.Event(nil), f.triggers...), append([][]sink.RevertEvent(nil), f.reverts...), f.closed
}

func TestRecordTriggerForwardedAndDrainedOnClose(t *testing.T) {
	w := &fakeWriter{}
	s := newSink(w, Config{QueueSize: 4})

	ev := sink.Event{AuditUID: "u1", PolicyRef: "p", Plugin: "nop", Status: "Succeeded", TriggeredAt: time.Now()}
	if err := s.RecordTrigger(context.Background(), []sink.Event{ev}); err != nil {
		t.Fatalf("RecordTrigger: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	trigs, _, closed := w.snapshot()
	if !closed {
		t.Fatal("writer.close was not called")
	}
	if len(trigs) != 1 || len(trigs[0]) != 1 || trigs[0][0].AuditUID != "u1" {
		t.Fatalf("unexpected triggers: %+v", trigs)
	}
}

func TestRecordRevertForwarded(t *testing.T) {
	w := &fakeWriter{}
	s := newSink(w, Config{QueueSize: 4})

	rv := sink.RevertEvent{AuditUID: "u1", Plugin: "nop", Status: "Succeeded", RevertedAt: time.Now()}
	if err := s.RecordRevert(context.Background(), []sink.RevertEvent{rv}); err != nil {
		t.Fatalf("RecordRevert: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, revs, _ := w.snapshot()
	if len(revs) != 1 || len(revs[0]) != 1 || revs[0][0].AuditUID != "u1" {
		t.Fatalf("unexpected reverts: %+v", revs)
	}
}

func TestEmptyBatchesAreNoOp(t *testing.T) {
	w := &fakeWriter{}
	s := newSink(w, Config{QueueSize: 4})
	if err := s.RecordTrigger(context.Background(), nil); err != nil {
		t.Fatalf("RecordTrigger(nil): %v", err)
	}
	if err := s.RecordRevert(context.Background(), nil); err != nil {
		t.Fatalf("RecordRevert(nil): %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = s.Close(ctx)

	trigs, revs, _ := w.snapshot()
	if len(trigs) != 0 || len(revs) != 0 {
		t.Fatalf("expected no writes, got triggers=%v reverts=%v", trigs, revs)
	}
}

func TestWriterErrorIsLoggedNotReturned(t *testing.T) {
	w := &fakeWriter{failNext: errors.New("boom")}
	s := newSink(w, Config{QueueSize: 4})

	if err := s.RecordTrigger(context.Background(), []sink.Event{{AuditUID: "u", Plugin: "p"}}); err != nil {
		t.Fatalf("RecordTrigger: %v", err)
	}
	if err := s.RecordTrigger(context.Background(), []sink.Event{{AuditUID: "u2", Plugin: "p"}}); err != nil {
		t.Fatalf("RecordTrigger after failure: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = s.Close(ctx)

	trigs, _, _ := w.snapshot()
	if len(trigs) != 1 {
		t.Fatalf("expected the second batch to land after a failed first; got %d batches", len(trigs))
	}
}

func TestBackpressureWhenQueueFull(t *testing.T) {
	started := make(chan struct{}, 1)
	block := make(chan struct{})
	w := &blockingWriter{started: started, block: block}
	s := newSink(w, Config{QueueSize: 1})

	ev := sink.Event{AuditUID: "u", Plugin: "p"}

	if err := s.RecordTrigger(context.Background(), []sink.Event{ev}); err != nil {
		t.Fatalf("first RecordTrigger: %v", err)
	}
	<-started

	if err := s.RecordTrigger(context.Background(), []sink.Event{ev}); err != nil {
		t.Fatalf("second RecordTrigger (fills queue): %v", err)
	}
	err := s.RecordTrigger(context.Background(), []sink.Event{ev})
	if !errors.Is(err, ErrBackpressure) {
		t.Fatalf("third RecordTrigger: want ErrBackpressure, got %v", err)
	}

	close(block)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.Close(ctx)
}

type blockingWriter struct {
	started chan struct{}
	block   chan struct{}
}

func (b *blockingWriter) writeTriggers(context.Context, []sink.Event) error {
	b.started <- struct{}{}
	<-b.block
	return nil
}
func (b *blockingWriter) writeReverts(context.Context, []sink.RevertEvent) error { return nil }
func (b *blockingWriter) close(context.Context) error                            { return nil }

func TestCloseIsIdempotent(t *testing.T) {
	w := &fakeWriter{}
	s := newSink(w, Config{QueueSize: 1})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Close(ctx); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := s.Close(ctx); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
